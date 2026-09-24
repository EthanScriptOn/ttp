# Kubernetes 适配要求

TTP 的运行时使用 Kubernetes API 读取项目 Pod、Pod 日志和 Deployment 状态，并在发布时更新已适配的原生资源。除下述监控组件外，TTP 不会自动安装集群插件，也不会把插件缺失伪装成发布成功。

## 发布权限链路

一次发布必须同时通过四段检查：当前登录用户具备 TTP 当前空间的发布权限；项目 Git 机器人能读取仓库并具备发布所需的写权限；`ttp-builder` 能认证目标镜像仓库并执行推送；TTP 使用的 Kubernetes 身份能在目标命名空间写入发布资源。前两段分别由 TTP 服务端和项目 Git 机器人配置页检查，第三段由 Builder 主机上的私有凭证映射检查，第四段在发布入队前通过 Kubernetes `SelfSubjectAccessReview` 检查。任一段失败都会阻止发布并返回对应错误码。

Kubernetes RBAC 只解决 TTP 是否能调用 API；镜像实际由节点或 Pod 的 `imagePullSecrets` 拉取。项目选择镜像仓库连接后，TTP 会在目标命名空间创建/更新 `kubernetes.io/dockerconfigjson` Secret，并把它注入每个 Deployment 的 `imagePullSecrets`；Manifest 中已有的 Secret 引用会保留。目标命名空间需要允许 TTP 身份创建和更新 Secret。凭证失效时，rollout 日志会明确显示 `ErrImagePull` / `ImagePullBackOff` 及 Pod 名称。

## 必需能力

目标集群需要提供 Kubernetes 核心 API，且平台服务账号至少能访问目标环境的命名空间：

| 能力 | 用途 |
| --- | --- |
| `Deployment`、`Pod` | 发布工作负载、等待 rollout、查看运行状态 |
| `StatefulSet`、`DaemonSet`、`Job`、`CronJob` | 发布有状态、节点级和批处理工作负载 |
| `Service` | 发布服务入口 |
| `ConfigMap`、`Secret`、`PersistentVolumeClaim` | 发布配置和持久化存储 |
| `ResourceQuota`、`LimitRange` | 为 TTP 环境 namespace 执行 CPU、内存、磁盘、Pod 和 PVC 预算 |
| `Ingress` | 发布外部入口配置 |
| `HorizontalPodAutoscaler` | 发布弹性伸缩配置 |
| `pods/log` | 查看 Pod 原始日志 |
| `pods/exec` | 进入 Pod 执行命令，只有启用终端权限时才需要 |

当前 Kubernetes provider 的真实发布策略只有 `rolling`。`canary`、`blue_green` 和 A/B 实验需要额外的流量 provider；不能仅靠 Kubernetes Deployment 把它们当成已完成。镜像构建和推送也需要接入真实 builder/registry，TTP 不会用 commit SHA 生成一个假的镜像地址。

## 监控组件

这些组件不是 Kubernetes 核心 API，TTP 的“安装监控”会在 `ttp-monitoring`
命名空间中统一创建和维护：

- `Prometheus`：TTP 唯一的监控数据源，保存并查询资源和应用历史序列。
- `node-exporter`：以 DaemonSet 运行，提供节点 CPU、内存、Swap、磁盘、磁盘 IO、网络和 Load 指标。
- `kube-state-metrics`：提供 Deployment、ReplicaSet、Pod、Job、PVC 等 Kubernetes 对象状态指标。
- Kubelet/cAdvisor：由 Prometheus 通过 Kubernetes API 代理抓取容器 CPU、内存和工作负载指标。
- `APISIX`、Envoy 或其他 Gateway：执行灰度、蓝绿和 A/B 流量切换。

TTP 自己维护的三个监控镜像只使用国内镜像服务，每个组件内置 DaoCloud、国内高校镜像站或国内云厂商的三个候选源。Kubernetes 会先在当前源重试；TTP 连续三次确认 `ErrImagePull` 或 `ImagePullBackOff` 后切换到下一个源，并把当前镜像源、重试次数和最终错误显示在安装进度弹框中。该策略只作用于 TTP 安装的监控组件，不会改写项目或用户 YAML 中的业务镜像地址。

普通 HTTP JSON 请求的 A/B 分流可以使用 `deploy/apisix/ttp-ab-router.lua`。它从请求体读取例如 `$.wx_id`，不要求 JWT，也不信任客户端自带的实验结果 header。这个 Lua 插件需要由集群管理员按 APISIX 的 custom plugin 方式加载；TTP 当前不会自动创建 APISIX route/upstream。

Prometheus 采集链路未就绪时，TTP 仍可展示 Kubernetes API 返回的 Pod 数量、Ready 状态和发布状态；资源指标会显示不可用，不补零、不生成示例数据。TTP 的资源指标查询只走 Prometheus。

## RBAC

[`ttp-runtime-rbac.yaml`](ttp-runtime-rbac.yaml) 是最小化的参考模板：

- `ServiceAccount` 放在 `ttp-system` 命名空间。
- `ttp-runtime-namespace-access` 是一份可被业务命名空间引用的 `ClusterRole`，包含发布、日志、PVC、ResourceQuota 和 LimitRange 所需的 namespace 内权限；它不会单独授予任何权限，只有被某个 namespace 的 `RoleBinding` 引用后才生效。
- `ttp-runtime-namespace-manager` 授予创建/更新 TTP namespace、在生成 namespace 中创建/更新 `RoleBinding`，以及绑定 `ttp-runtime-namespace-access` 的引导能力。
- TTP 创建或更新环境 namespace 时，会自动维护该 namespace 下的 `RoleBinding/ttp-runtime`，不再需要把模板里的 namespace 占位符手动替换 N 次。
- 节点只读状态使用单独的只读 `ClusterRole`；Prometheus 及两个 exporter 的权限由安装资源统一创建。
- `pods/exec` 在 `ttp-runtime-namespace-access` 中单独列出；不需要终端功能时应删除该规则。

应用前请审阅模板中的权限范围，特别是 `rolebindings` 写权限以及监控安装所需的 `clusterroles/bind`、`clusterroles/escalate` 能力：它们分别用于动态 namespace 授权和创建 Prometheus exporter 的只读角色。应用后使用检查脚本确认实际运行身份的权限，不要直接把模板当作生产授权策略。

```bash
./deploy/kubernetes/check-prerequisites.sh --namespace ttp-lab-dev --service-account ttp-runtime
```

检查脚本只读集群，不安装插件、不修改 RBAC、不修改业务资源。完整检查会因集群权限和可选组件缺失给出明确结果。

## 连接配置

后端启动前需要：

```bash
export CICD_RUNTIME_PROVIDER=kubernetes
export CICD_KUBE_CLUSTER_ID=cluster-prod
export CICD_KUBECONFIG=/etc/ttp/kubeconfig
export CICD_KUBE_SERVICE_ACCOUNT_NAMESPACE=ttp-system
export CICD_KUBE_SERVICE_ACCOUNT_NAME=ttp-runtime
```

也可以在集群内使用 `CICD_KUBE_IN_CLUSTER=true`，由 Pod 的 ServiceAccount 提供身份。`CICD_KUBE_SERVICE_ACCOUNT_NAMESPACE` 和 `CICD_KUBE_SERVICE_ACCOUNT_NAME` 必须与实际运行 TTP 的 ServiceAccount 一致，否则自动创建的 RoleBinding 会绑定到错误主体。kubeconfig 内容只在后端进程内使用，不返回前端。
