# TTP · The Turbocharged Platform

本平台提供按空间隔离的项目、集群和发布控制面基础设施。当前仓库范围包含 MySQL 8 schema、本地开发 Compose、真实 Git/Kubernetes 接入和回归脚本。

## 快速开始

```bash
cd "/Users/yuebuy/GolandProjects/android-reverse-lab-control/cicd-platform"
cp .env.example .env
# 编辑 .env，至少替换数据库密码、CICD_JWT_SECRET，并设置 CICD_KUBE_CLUSTER_ID。

# 统一启动 MySQL、TTP 后端和前端。
scripts/start-local.sh
```

首次启动会自动初始化 `.runtime/credential.key`，用于加密项目 Git Token；以后重启会复用这个文件，不需要再次录入 Token。生产环境也可以通过 `CICD_GIT_CREDENTIAL_KEY` 或密钥管理系统显式注入固定密钥。请同时把 `CICD_JWT_SECRET` 写入长期配置，不要每次启动都重新生成。

空库只在后端首次启动时创建固定的默认管理员：账号 `admin`，密码 `ttp`。Redis 不是必需依赖，需要时执行 `docker compose -p "${COMPOSE_PROJECT_NAME:-cicd-platform}" -f deploy/docker-compose.yml --profile cache up -d`。Compose 不包含任何真实凭据；密码只存在于当前终端或本地未提交的环境文件中。

Compose 默认从国内公开镜像代理 `docker.m.daocloud.io` 拉取 MySQL 和 Redis，不依赖 Docker Hub。

## 文件

- [`migrations/001_init.sql`](migrations/001_init.sql)：初始化核心表；`004_release_runtime.sql` 追加环境发布状态和执行日志，`005_project_git_credentials.sql` 追加项目级仓库机器人凭证表，`007_release_artifact.sql` 追加不可变镜像产物记录，`008_drop_project_build_configs.sql` 清理已移除的项目级构建配置表，`011_project_deployment_resource_files.sql` 保存逐文件 Kubernetes 资源配置，`012_deployment_namespace_quotas.sql` 保存环境资源配额，`014_deployment_resource_overrides.sql` 保存环境级资源覆盖，`015_project_auto_merge.sql` 保存环境发布成功后的项目自动合并策略，`016_release_flows.sql` 保存发布流程和参与分支集合。
- [`deploy/docker-compose.yml`](deploy/docker-compose.yml)：MySQL 8.0.36 和可选 Redis 7.2。
- [`docs/local-development.md`](docs/local-development.md)：启动、连接、登录说明，API 流程、Kubernetes 依赖和回归约定。
- [`deploy/kubernetes/README.md`](deploy/kubernetes/README.md)：Kubernetes 核心资源、附加组件、RBAC 和检查脚本。
- [`deploy/builder/README.md`](deploy/builder/README.md)：同机独立运行的 BuildKit 构建服务、凭证边界与配置方式。
- [`scripts/regression.sh`](scripts/regression.sh)：后端测试、竞态测试和前端构建入口。
- [`scripts/api-regression.sh`](scripts/api-regression.sh)：针对运行中真实 API 的只读/可清理黑盒回归。

项目部署配置现在通过 `/api/projects/:projectID/deployment-resources` 管理资源文件列表。每个文件只能包含一个 Kubernetes 对象，保存后发布端会逐文件应用；不同资源不再通过 `---` 拼接。
- [`scripts/reset-local-db.sh`](scripts/reset-local-db.sh)：只清空 TTP MySQL 业务表，保留数据卷和表结构。
- [`scripts/verify-local-db.sh`](scripts/verify-local-db.sh)：只读检查空库初始化结果。

## 数据模型约定

业务资源都通过 `space_id` 归属空间。项目到集群、发布到项目、发布 commit 到发布记录均使用外键约束；项目级唯一索引和空间级查询索引用于避免跨空间串数据。

发布使用 `release_fingerprint` 做项目内幂等键，`release_commits` 使用发布和 SHA 复合主键防止重复 commit，并以移除标记保留草稿编辑和审计所需的历史。`project_releases` 保存灰度的 stable/candidate 比例以及蓝绿的 blue/green 比例，并用 `CHECK` 约束校验总和。环境发布状态、环境快照和原始执行日志分别保存在 `release_runtime_targets`、`release_execution_logs` 中，服务重启后会从 MySQL 恢复。

详细本地流程见 [`docs/local-development.md`](docs/local-development.md)。仓库只在空库初始化默认管理员，不包含业务空间、集群、项目或发布的预置数据。

回归测试分为两层：`scripts/regression.sh` 不改数据库，执行后端单测、竞态测试、vet 和前端构建；`scripts/api-regression.sh` 默认只读检查真实 API。设置 `RUN_MUTATING_API_CHECK=YES` 后，它会创建带唯一后缀的临时空间、集群、项目和发布配置，结束时只删除这个临时空间并保留默认管理员，不会清空整个 MySQL 数据库。

集群连接配置入口在登录后的“集群管理”页面：可以登记 API Server 地址、kubeconfig 路径或集群内身份，并在保存后测试连接。具体字段含义和 K3s 路径见 [`docs/local-development.md`](docs/local-development.md) 的“集群配置”一节。

## 原生 Kubernetes 配置

Kubernetes 是真正负责创建和运行 Pod、Service、ConfigMap 等资源的系统；YAML/JSON 是告诉 Kubernetes“希望资源长什么样”的原生配置文件。Helm 只是把一组 YAML 做成可复用模板和安装包，不是 Kubernetes 的必需依赖。

本平台默认走更直白的方式：进入项目的“部署配置”，维护 Kubernetes 资源文件列表。每个文件只包含一个可以由 `kubectl apply -f` 应用的 YAML 或 JSON 对象；页面会自动格式化并检查 `apiVersion`、`kind`、`metadata.name`、重复资源和目标环境。不同资源使用不同文件，不再把多个资源拼成一个 `---` 文档流。

因此，普通开发者不需要为了发布去编写 Helm。只有在团队已经有 Helm Chart、需要按环境批量套模板时，才适合在后续接入 Helm 渲染；它和原生 YAML 编辑可以并存，不互相强制依赖。

当前代码已经完成资源文件的编辑、校验、保存和逐文件发布请求传递。真实 Kubernetes provider 目前会直接处理常见的命名空间资源：`Deployment`、`StatefulSet`、`DaemonSet`、`Job`、`CronJob`、`Service`、`ConfigMap`、`Secret`、`PVC`、`Ingress` 和 `HorizontalPodAutoscaler`；编辑器会逐个标出当前发布器能否处理的资源。其它资源会在发布前明确拒绝，避免误以为已经应用。镜像构建推送由 `ttp-builder` 使用 BuildKit 执行；项目用户只需要在仓库根目录提供 `Dockerfile`，构建平台和推送凭证由 TTP 平台运维配置。项目在空间设置中登记镜像仓库连接后，只需在项目中选择该连接，平台会按项目 ID 自动生成稳定的镜像仓库路径并在发布时使用；凭证始终只保存在服务端。未接入真实 Builder 时，平台会明确失败，不会把 commit 伪装成镜像。非滚动流量编排仍由独立的流量 provider 负责。provider 同时负责读取 Pod、日志、基础状态和修改 Pod 所属 Deployment。

真实 Kubernetes provider 发布 `Deployment` 后会继续等待 rollout：只有目标版本被控制器观察到、期望副本全部更新且 Ready/Available，发布才会成功；超时或控制器报告失败会把原因写入发布记录。默认等待 5 分钟，可通过 `CICD_KUBE_ROLLOUT_TIMEOUT_SECONDS` 调整。构建链路需要由部署者显式配置 `ttp-builder`、BuildKit、目标镜像仓库和仓库凭证映射；这些配置不进入项目页面或项目数据库。非滚动流量编排仍需要接入相应的流量 provider。

真实 Git 仓库接入的环境变量和安全边界见 [`docs/local-development.md`](docs/local-development.md) 的“GitHub / GitLab 仓库读取”一节。

### 环境资源配额

“发布环境”就是 TTP 管理的 Kubernetes namespace。创建或编辑环境时可以预设 CPU、内存、临时磁盘、持久化存储、Pod 数量和 PVC 数量；同一 TTP 空间中使用同一集群和环境标识的多个项目共享同一份预算。namespace 名称由空间 slug 和环境标识稳定生成，例如 `ttp-lab-dev`，项目资源文件不能改变它。

真实 Kubernetes provider 会在 namespace 上维护一个 TTP 所有的 `ResourceQuota` 和 `LimitRange`：

- `ResourceQuota` 限制 CPU/memory request、CPU/memory limit、临时磁盘、PVC 请求的总存储、Pod 数量和 PVC 数量。
- `LimitRange` 为没有显式填写 resources 的容器提供默认 request/limit，避免容器绕过资源预算。
- `RoleBinding/ttp-runtime` 会自动绑定到平台运行 ServiceAccount，使新生成的 namespace 创建后立即具备发布、日志、PVC 和配额同步权限。
- PVC 仍由项目资源文件创建和维护，但总容量和数量必须落在环境配额内；TTP 不会替项目偷偷创建 PVC。

默认环境预算是 CPU request/limit `2/4`、内存 `2Gi/4Gi`、临时盘 `10Gi/20Gi`、持久盘 `50Gi`、Pod `20`、PVC `10`。这些是 namespace 上限，不是对集群容量的超额承诺；实际集群仍需预留足够节点资源。

发布前在项目设置中配置该项目自己的仓库机器人：GitHub 至少 `Write`，GitLab 至少 `Developer`；受保护分支的合并还要按平台规则授予更高权限。平台会先检查 Token 对应账号和仓库权限，开发者不需要提供个人 Git 凭据。配置项和接口说明见 [`docs/local-development.md`](docs/local-development.md) 的“项目仓库机器人”一节。
