# 本地开发

本文覆盖本平台的本地基础设施、真实依赖和回归入口。默认启动为空库，`backend` 和 `frontend` 只连接真实 API。

## 目录和服务

- `migrations/001_init.sql` 至 `migrations/012_deployment_namespace_quotas.sql`：MySQL 8.0 初始化和增量 schema，其中 `007` 保存发布使用的不可变镜像摘要，`008` 清理已移除的项目级构建配置表，`011` 保存逐文件 Kubernetes 资源配置，`012` 保存环境资源配额。Compose 首次创建 MySQL 数据卷时按文件名顺序自动执行，后端启动时也会补齐表结构并执行已移除表的清理。
- `deploy/docker-compose.yml`：默认启动 MySQL；Redis 通过 Compose profile `cache` 可选启动。
- Compose 默认从国内公开镜像代理 `docker.m.daocloud.io` 拉取 MySQL 和 Redis。
- 默认端口：MySQL `3306`，Redis `6379`。
- `scripts/start-local.sh`：启动 MySQL、后端和前端；设置 `TTP_BUILDER_ENABLED=true` 时还会以独立进程启动同机 Builder。
- `deploy/builder/README.md`：BuildKit、OCI 仓库凭证映射和同机 Builder 的配置说明。
- `scripts/reset-local-db.sh`：仅清空 TTP MySQL 业务表，保留数据卷和表结构，必须显式确认。
- `scripts/verify-local-db.sh`：只读检查数据库是否只剩一个默认管理员。
- `scripts/regression.sh`：运行后端测试、竞态测试和前端构建。
- `scripts/api-regression.sh`：对运行中的真实 API 做只读检查；可选地创建临时资源并自动清理。
- `deploy/kubernetes/README.md`：Kubernetes API、监控组件、流量组件和 RBAC 要求。

## 启动数据库

在平台根目录执行。密码由当前终端随机生成，不要把它们写入 git、shell 历史或共享文档。

```bash
cd "/Users/yuebuy/GolandProjects/android-reverse-lab-control/cicd-platform"
cp .env.example .env
# 编辑 .env，至少替换数据库密码、CICD_JWT_SECRET，并设置 CICD_KUBE_CLUSTER_ID。

docker compose -p "${COMPOSE_PROJECT_NAME:-cicd-platform}" -f deploy/docker-compose.yml up -d mysql
docker compose -p "${COMPOSE_PROJECT_NAME:-cicd-platform}" -f deploy/docker-compose.yml ps
```

需要 Redis 时：

```bash
docker compose -p "${COMPOSE_PROJECT_NAME:-cicd-platform}" -f deploy/docker-compose.yml --profile cache up -d
```

Compose 使用命名卷保存数据。初始化脚本只在空数据目录上运行。日常需要清理 TTP 数据时，只清空业务表，保留 MySQL 数据卷、表结构和迁移状态：

```bash
CONFIRM_RESET=YES scripts/reset-local-db.sh
```

该命令会清空用户、空间、集群、项目、环境、发布、实验和审计记录；后端重新连接后会初始化唯一的默认管理员。脚本会优先复用当前 Compose 项目的 MySQL 容器，只有需要新建容器时才要求 MySQL 密码变量。不要使用 `docker compose down -v` 代替它，后者会删除整个 MySQL 数据卷。

后端启动后，可以用只读脚本验收结果：

```bash
scripts/verify-local-db.sh
```

期望结果是 `users=1`，空间、集群、项目、环境、发布、运行时日志、实验和审计记录全部为 `0`。

## 一键启动

准备好 MySQL 账号、JWT 密钥和 Kubernetes 集群标识后，在项目根目录执行：

```bash
export MYSQL_DATABASE=cicd_platform
export MYSQL_USER=cicd_app
export MYSQL_ROOT_PASSWORD='本地 MySQL root 密码'
export MYSQL_PASSWORD='本地 MySQL 应用密码'
export CICD_JWT_SECRET='本地 JWT 密钥'
export CICD_KUBE_CLUSTER_ID=local
scripts/start-local.sh
```

脚本会自动读取根目录 `.env`（也可以通过 `TTP_ENV_FILE` 指定），等待 MySQL 健康后同时启动后端 `8790` 和前端 `5173`。首次启动会在项目根目录的 `.runtime/credential.key` 初始化项目 Git Token 和镜像仓库凭证的加密密钥，后续重启自动复用。`CICD_GIT_CREDENTIAL_KEY` 只用于初始化缺失的 key 文件；key 文件存在时始终以文件为准，避免旧 shell 环境变量覆盖已保存的凭证。按 `Ctrl-C` 会停止本次启动的前后端进程，但不会删除 MySQL 数据卷。空库首次连接时自动创建 `admin` / `ttp`；已有用户密码不会被启动过程覆盖。

如需本机完成镜像构建，先按 [`deploy/builder/README.md`](../deploy/builder/README.md) 安装并启动 BuildKit，设置 Builder 的私有 registry 凭证映射，再设置 `TTP_BUILDER_ENABLED=true` 后运行相同脚本。脚本会启动 `ttp-builder` 作为独立进程；TTP API 不会获得镜像仓库密码。

## API 黑盒回归

后端和前端启动后，使用默认管理员执行只读检查。空库初始化后的登录账号固定为 `admin`，密码固定为 `ttp`：

```bash
export CICD_REGRESSION_USERNAME=admin
export CICD_REGRESSION_PASSWORD=ttp
scripts/api-regression.sh
```

需要验证项目、环境、Manifest、真实 Git 分支和发布单生命周期时，显式开启可清理的变更检查：

```bash
RUN_MUTATING_API_CHECK=YES scripts/api-regression.sh
```

该模式只删除脚本创建的临时空间，保留默认管理员和其他既有业务数据。真实 Git、镜像构建和 Kubernetes 权限由外部依赖决定，脚本遇到缺失时会明确报告，不伪造成功。

## 连接信息

应用从宿主机连接 MySQL 时使用 `127.0.0.1`，连接参数为当前终端中的 `MYSQL_USER`、`MYSQL_PASSWORD`、`MYSQL_DATABASE` 和 `MYSQL_PORT`。Go MySQL 驱动的 DSN 形态如下，密码只从环境变量读取：

```text
cicd_app:${MYSQL_PASSWORD}@tcp(127.0.0.1:3306)/cicd_platform?parseTime=true&charset=utf8mb4&loc=UTC
```

Compose 不预置空间、集群、项目、环境或 token。后端第一次连接空库时只初始化固定的默认管理员；已有用户数据不会被启动过程覆盖。

## API 流程

API 请求应始终先完成身份认证，再固定一个空间上下文；空间 ID 不能由客户端任意拼接来绕过服务端的成员校验。

1. `POST /api/auth/login`：提交用户名、密码，可选提交 `space_id`。响应包含 token、可访问空间和当前空间。
2. `GET /api/auth/me`：使用 `Authorization: Bearer <token>` 检查当前用户和 token 中的空间上下文。
3. `GET /api/spaces`：列出当前用户可访问的空间；切换空间时调用 `POST /api/auth/select-space`，成功后使用新 token。
4. `GET /api/projects`：按当前 token 中的空间读取项目；创建或更新项目时继续使用同一个空间上下文和 Bearer token。
5. `GET /api/projects/{project_id}/git/branches` 与 `/git/commits`：读取真实仓库的分支和历史提交；用户选择的是分支，创建发布单时服务端读取并保存该分支的 HEAD 快照。
6. `POST /api/projects/{project_id}/releases`：创建发布单。请求包含 `branch`、目标环境、`strategy` 以及比例字段；相同项目、分支快照、策略和比例重复提交时返回已有发布并标记 `duplicate=true`。
7. `DELETE /api/projects/{project_id}/releases/{release_id}/commits/{sha}`：仅草稿发布允许移除 commit；持久化实现应保留移除状态，不能用物理删除破坏审计和重复发布判定。最后一个有效 commit 不允许移除。
8. `POST /api/projects/{project_id}/releases/{release_id}/publish`：推进发布状态。发布进入运行态后应视为不可变，并写入 `audit_logs`。
9. `GET /api/audit-logs?limit=100`：读取当前空间的操作记录；服务端按 token 中的空间过滤，不能通过参数读取其他空间。

## Kubernetes 资源文件编辑器

项目详情里的“部署配置”页面维护 Kubernetes 资源文件列表，不要求项目里存在 Helm Chart。

- 每个文件只能包含一个 Kubernetes 对象，文件内容可以直接保存为 `.yaml` 或 `.json` 后使用 `kubectl apply -f`。
- 文件通过 `GET/POST /api/projects/{project_id}/deployment-resources` 列出和创建，通过 `PUT/DELETE .../{resource_id}` 单独保存或删除，也可以调用 `POST .../validate` 先校验。
- 服务端会检查语法、资源身份、文件大小、项目命名空间和集群级资源边界；发布时按列表逐文件应用，不再拼接多文档内容。
- 当前发布器会对已适配的常见 namespaced 资源执行 TTP 镜像、环境变量和归属标签注入；列表中未适配的资源会明确标记并在发布校验阶段拒绝。

这个编辑器解决的是“资源文件怎么写、怎么保存、发布时按什么顺序应用”的问题。Secret 内容会随资源文件保存，生产环境应使用正式的加密存储或外部密钥管理。

## 环境资源配额

在“发布环境”中配置资源预算。TTP 按“空间 + 集群 + 环境标识”保存一份共享配额，因此同一空间下的多个项目如果选择同一集群和 `dev` 环境，会共同使用同一个 namespace 和同一份预算，而不会各自创建一份无限制的 namespace。

创建或编辑环境时可填写：

- CPU request/limit 总量；
- 内存 request/limit 总量；
- 临时磁盘 request/limit 总量；
- PVC 请求的持久化存储总量；
- Pod 数量上限和 PVC 数量上限。

真实 Kubernetes provider 会把预算同步到目标 namespace 的 `ResourceQuota`，并通过 `LimitRange` 为未写 `resources` 的容器补上默认 CPU、内存和临时磁盘 request/limit。创建或更新环境时，TTP 还会在生成 namespace 中自动维护 `RoleBinding/ttp-runtime`，绑定到平台运行 ServiceAccount，让动态 namespace 不再需要人工逐个授权。默认预算为 CPU `2/4`、内存 `2Gi/4Gi`、临时盘 `10Gi/20Gi`、持久盘 `50Gi`、Pod `20`、PVC `10`；容器默认值为 CPU `100m/500m`、内存 `128Mi/512Mi`、临时盘 `256Mi/1Gi`。

PVC 仍然需要作为项目资源文件由用户维护。TTP 只负责 namespace 的 PVC 数量和总存储上限，不会替项目隐式创建或扩容 PVC。配额是 namespace 上限，不是对集群容量的预留；多个空间的配额总和仍应由运维人员结合集群节点容量规划。

## 集群配置

登录并进入空间后，打开左侧的“集群管理”。点击“添加集群”或已有集群的“编辑配置”，填写集群名称、Kubernetes API Server 地址和连接方式。

- 选择“服务器上的 kubeconfig”时，路径必须是 CI/CD 后端服务器上的路径；K3s 常见路径为 `/etc/rancher/k3s/k3s.yaml`。Context 留空会使用文件中的 `current-context`。
- API Server 地址用于标识和核对目标集群；真正的访问地址、证书和权限由 kubeconfig 提供。后端服务器必须能访问 kubeconfig 中的 `server` 地址，不能把只在另一台机器上有效的 `127.0.0.1` 当成远端地址。
- 选择“集群内身份”时，平台进程需要运行在 Kubernetes Pod 内，并通过 ServiceAccount 访问 API Server。
- 保存后会自动测试连接。测试失败仍会保留配置并将集群标记为“离线”，修正后可以反复点击“测试连接”。kubeconfig 内容和路径不会通过 API 返回给前端。
生产和本地真实运行都使用 `CICD_RUNTIME_PROVIDER=kubernetes`。后端必须能读取 `CICD_KUBECONFIG` 或运行在有 ServiceAccount 的集群内，并且 `CICD_KUBE_CLUSTER_ID` 必须对应页面中登记的集群。自动 RoleBinding 默认绑定 `ttp-system/ttp-runtime`，如果实际运行账号不同，请设置 `CICD_KUBE_SERVICE_ACCOUNT_NAMESPACE` 和 `CICD_KUBE_SERVICE_ACCOUNT_NAME`。

当前 API 不把 `space_id` 拼在资源 URL 中；服务端从 JWT 的 `space_id` claim 取得空间，并在每次资源访问时校验用户是否属于该空间。切换空间后必须用接口返回的新 token 替换旧 token。

所有服务端查询都必须带 `space_id`，并在资源关系上校验同空间复合外键。管理员的跨空间能力应由授权层显式授予，不应通过省略过滤条件实现。

## 监控面板

项目详情和集群监控页使用 Grafana 风格的时间序列面板，当前按时间范围查看近 15 分钟、1 小时、6 小时或 24 小时，并支持手动刷新和自动刷新。面板覆盖主机资源（CPU、内存、Swap、磁盘、磁盘读写、网络收发、Load）、Kubernetes 运行状态（节点、Pod、部署可用度、重启、Pending、失败、CrashLoop、OOMKilled）以及应用服务质量（请求量、错误率、P50/P95/P99 延迟）。

Pod、节点身份和就绪状态来自 Kubernetes API；资源指标统一来自 Prometheus。TTP 安装监控时会同时配置 Prometheus、node-exporter、kube-state-metrics，并让 Prometheus 通过 Kubernetes API 抓取 Kubelet/cAdvisor。采集链路未就绪时页面显示指标不可用，不补 0，也不显示示例序列。

## 策略比例

`project_releases` 同时保存四个比例字段，便于灰度和蓝绿发布统一记录：

- `canary`：`stable_percent + candidate_percent = 100`，蓝绿字段必须为 0。
- `blue_green`：`blue_percent + green_percent = 100`，稳定/候选字段必须为 0。
- `rolling`：固定为 stable 100%，其他比例为 0。

比例校验由 MySQL 8 `CHECK` 约束和服务端业务校验共同承担。发布指纹 `release_fingerprint` 应由服务端对规范化后的项目、仓库、分支、策略、比例和排序后的有效 commit SHA 计算 SHA-256。

## 发布执行边界

- 发布单以分支为入口，但创建时会把当前 HEAD 的 commit、提交信息和环境快照保存下来；之后分支继续变化，重新发布仍使用这个历史快照，不会悄悄带入新 commit。
- 真实 Kubernetes provider 当前只执行 `rolling`；`canary`、`blue_green` 和 A/B 需要额外的流量 provider（例如 APISIX、Envoy 或团队自己的网关插件）。
- 正式发布必须配置真实 `ttp-builder`。项目只需在仓库根目录提供 `Dockerfile`，并在项目设置中选择空间级镜像仓库连接；镜像仓库路径由平台按项目 ID 自动生成。Builder 的默认镜像仓库、凭证引用和构建平台由平台运维环境配置。没有 Builder 时，平台会明确返回 `image_build_unsupported`，不会把 commit SHA 冒充镜像，也不会报告 Kubernetes 发布成功。
- 发布进入执行态后版本快照不可编辑；失败环境可以单独重试，取消会关闭尚未完成的环境，并保留执行日志。

真实 Kubernetes 发布在资源写入成功后还会等待 Deployment rollout，确认目标版本已被控制器观察到且副本 Ready/Available。默认等待 5 分钟，可通过 `CICD_KUBE_ROLLOUT_TIMEOUT_SECONDS` 调整；超时或控制器失败会保留具体原因。

## GitHub / GitLab 仓库读取

项目保存的仓库地址会绑定到真实 Git provider，发布页面从真实仓库读取分支和 commit；开发者不需要编辑 Helm 文件。

```bash
export CICD_GIT_PROVIDER=auto       # auto、github 或 gitlab
export CICD_GIT_ALLOWED_HOSTS='git.example.com' # 自建 Git 服务必填，可逗号分隔
# 自建 GitLab/GitHub Enterprise 可指定 API 根地址，例如 https://git.example.com/api/v4
export CICD_GIT_API_BASE_URL=''
export CICD_GIT_TIMEOUT_SECONDS=15
# 可选。未设置时，TTP 会在 CICD_GIT_CREDENTIAL_KEY_FILE 指向的位置自动生成并复用密钥。
export CICD_GIT_CREDENTIAL_KEY_FILE='/var/lib/cicd-platform/credential.key'
```

`auto` 会识别 `github.com` 和 `gitlab.com`；自建 Git 服务请显式设置 provider，并把仓库/API 的主机加入 allowlist。token 只通过请求头发送，不会拼进 URL、日志或错误响应。当前 provider 负责读取仓库信息、分支和 commit，并校验项目机器人权限；镜像构建、滚动发布和流量切换分别由对应的 builder、Kubernetes runtime 和流量 provider 负责。

### Kubernetes 发布等待

真实 Kubernetes 发布在资源写入成功后不会立即算成功。平台会继续读取
Deployment 状态，确认目标版本已经被控制器观察到，并且期望数量的副本都已
更新、Ready、Available；如果控制器报告 `ProgressDeadlineExceeded` 或副本创建
失败，发布会失败并保留原因。默认等待 5 分钟，可以按集群启动时的发布耗时调整：

```bash
export CICD_KUBE_ROLLOUT_TIMEOUT_SECONDS=300
```

这里的等待只适用于真实 Kubernetes provider。没有可访问集群时，服务不会启动为可发布状态。

### 项目仓库机器人

每个项目在“项目设置”中配置自己的 GitHub/GitLab 机器人账号和 Token，不使用 TTP 登录账号，也不依赖固定用户名。保存前 TTP 会调用 Git 平台接口核验 Token 身份、仓库可见性和写权限；账号不匹配、未加入仓库或权限不足时不会保存。

- GitHub：把项目机器人作为仓库 Collaborator，权限至少为 `Write`。如果要让平台合并受保护分支，通常还需要 `Maintain` 或 `Admin`，并满足组织 SSO 和分支保护规则。
- GitLab：把项目机器人加入项目，权限至少为 `Developer`；受保护分支的合并通常需要 `Maintainer`，还要满足 Approval、Protected Branch 和 Push Rules。
- Token 只通过 HTTPS 请求发送到 TTP 后端，由后端用 `CICD_GIT_CREDENTIAL_KEY` 加密保存。接口只返回平台、用户名、配置状态和检查结果，Token 不回显、不写日志、不进入 Git。
- 项目 Git Token 使用 AES-GCM 加密保存。`CICD_GIT_CREDENTIAL_KEY` 可以由密钥管理或环境变量注入；未设置时服务首次启动会生成并持久化实例密钥。密钥文件或显式密钥丢失后，历史凭证无法解密，需要重新配置。

项目级接口为 `GET/PUT/DELETE /api/projects/{project_id}/git/credential`，仓库访问检查为 `GET /api/projects/{project_id}/git/access`。所有项目的分支、提交、发布前检查、发布和重试请求都会先加载并绑定该项目凭证。

Kubernetes 集群的核心 API、监控组件、流量组件和最小 RBAC 参考 [`deploy/kubernetes/README.md`](../deploy/kubernetes/README.md)。可以用 `./deploy/kubernetes/check-prerequisites.sh --namespace release --service-account ttp-runtime` 检查实际发布账号的权限；脚本只读，不安装或修改集群组件。
