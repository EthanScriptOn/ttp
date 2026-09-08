# Kubernetes 适配要求

TTP 的运行时使用 Kubernetes API 读取项目 Pod、Pod 日志和 Deployment 状态，并在发布时更新已适配的原生资源。TTP 不会自动安装集群插件，也不会把插件缺失伪装成发布成功。

## 必需能力

目标集群需要提供 Kubernetes 核心 API，且平台服务账号至少能访问目标环境的命名空间：

| 能力 | 用途 |
| --- | --- |
| `Deployment`、`Pod` | 发布工作负载、等待 rollout、查看运行状态 |
| `Service` | 发布服务入口 |
| `ConfigMap`、`Secret` | 发布配置 |
| `Ingress` | 发布外部入口配置 |
| `HorizontalPodAutoscaler` | 发布弹性伸缩配置 |
| `pods/log` | 查看 Pod 原始日志 |
| `pods/exec` | 进入 Pod 执行命令，只有启用终端权限时才需要 |

当前 Kubernetes provider 的真实发布策略只有 `rolling`。`canary`、`blue_green` 和 A/B 实验需要额外的流量 provider；不能仅靠 Kubernetes Deployment 把它们当成已完成。镜像构建和推送也需要接入真实 builder/registry，TTP 不会用 commit SHA 生成一个假的镜像地址。

## 监控组件

这些组件不是 Kubernetes 核心 API，需要按集群现状安装和维护：

- `metrics-server`：提供 Pod/Node 当前 CPU 和内存使用量。
- `Prometheus`：保存 CPU、内存、磁盘、网络、请求量、错误率和延迟等历史序列。
- `kube-state-metrics`：提供 Deployment、ReplicaSet、Pod 等对象状态指标。
- `APISIX`、Envoy 或其他 Gateway：执行灰度、蓝绿和 A/B 流量切换。

普通 HTTP JSON 请求的 A/B 分流可以使用 `deploy/apisix/ttp-ab-router.lua`。它从请求体读取例如 `$.wx_id`，不要求 JWT，也不信任客户端自带的实验结果 header。这个 Lua 插件需要由集群管理员按 APISIX 的 custom plugin 方式加载；TTP 当前不会自动创建 APISIX route/upstream。

没有 metrics-server 或 Prometheus 时，TTP 仍可展示 Kubernetes API 返回的 Pod 数量、Ready 状态和发布状态；指标曲线会显示不可用，不补零、不生成示例数据。

## RBAC

[`ttp-runtime-rbac.yaml`](ttp-runtime-rbac.yaml) 是最小化的参考模板：

- `ServiceAccount` 放在 `ttp-system` 命名空间。
- 每个业务命名空间单独绑定一个 `Role`，避免平台账号默认拥有所有命名空间的写权限。
- 节点和 metrics API 使用单独的只读 `ClusterRole`。
- `pods/exec` 在同一个模板中单独列出；不需要终端功能时应删除该规则。

把文件中的 `change-me` 替换成真实命名空间后再审阅并应用。应用前使用检查脚本确认当前身份的权限，不要直接把模板当作生产授权策略。

```bash
./deploy/kubernetes/check-prerequisites.sh --namespace release
```

检查脚本只读集群，不安装插件、不修改 RBAC、不修改业务资源。完整检查会因集群权限和可选组件缺失给出明确结果。

## 连接配置

后端启动前需要：

```bash
export CICD_RUNTIME_PROVIDER=kubernetes
export CICD_KUBE_CLUSTER_ID=cluster-prod
export CICD_KUBECONFIG=/etc/ttp/kubeconfig
```

也可以在集群内使用 `CICD_KUBE_IN_CLUSTER=true`，由 Pod 的 ServiceAccount 提供身份。kubeconfig 内容只在后端进程内使用，不返回前端。

