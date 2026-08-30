# 本地开发

本文只覆盖本平台的本地基础设施。`backend` 和 `frontend` 的启动命令以各自实现为准，本目录不在本次变更范围内。

## 目录和服务

- `migrations/001_init.sql`：MySQL 8.0 初始化 schema。Compose 首次创建 MySQL 数据卷时自动执行。
- `deploy/docker-compose.yml`：默认启动 MySQL；Redis 通过 Compose profile `cache` 可选启动。
- 默认端口：MySQL `3306`，Redis `6379`。

## 启动数据库

在平台根目录执行。密码由当前终端随机生成，不要把它们写入 git、shell 历史或共享文档。

```bash
cd ttp
export MYSQL_DATABASE=cicd_platform
export MYSQL_USER=cicd_app
export MYSQL_ROOT_PASSWORD="$(openssl rand -hex 24)"
export MYSQL_PASSWORD="$(openssl rand -hex 24)"
export CICD_BOOTSTRAP_ADMIN_PASSWORD="$(openssl rand -base64 24)"

docker compose -f deploy/docker-compose.yml up -d mysql
docker compose -f deploy/docker-compose.yml ps
```

需要 Redis 时：

```bash
docker compose -f deploy/docker-compose.yml --profile cache up -d
```

Compose 使用命名卷保存数据。初始化脚本只在空数据目录上运行；如果要在本地重新初始化，确认数据可丢失后再执行：

```bash
docker compose -f deploy/docker-compose.yml down -v
```

## 连接信息

应用从宿主机连接 MySQL 时使用 `127.0.0.1`，连接参数为当前终端中的 `MYSQL_USER`、`MYSQL_PASSWORD`、`MYSQL_DATABASE` 和 `MYSQL_PORT`。Go MySQL 驱动的 DSN 形态如下，密码只从环境变量读取：

```text
cicd_app:${MYSQL_PASSWORD}@tcp(127.0.0.1:3306)/cicd_platform?parseTime=true&charset=utf8mb4&loc=UTC
```

Compose 不预置业务账号、密码或 token。MySQL 第一次启动时，后端使用 `CICD_BOOTSTRAP_ADMIN_PASSWORD` 创建管理员；如果数据库中已经存在 `admin`，不会覆盖已有密码。没有设置该变量时，后端会拒绝初始化新管理员并退出。管理员账号只适用于本机开发，不能用于生产。

使用演示存储时设置 `CICD_DEMO_ADMIN_PASSWORD`，用户名仍为 `admin`。不设置时会生成不可预知的临时密码，适合单测，不适合需要登录的本地演示。

## API 流程

API 请求应始终先完成身份认证，再固定一个空间上下文；空间 ID 不能由客户端任意拼接来绕过服务端的成员校验。

1. `POST /api/auth/login`：提交用户名、密码，可选提交 `space_id`。响应包含 token、可访问空间和当前空间。
2. `GET /api/auth/me`：使用 `Authorization: Bearer <token>` 检查当前用户和 token 中的空间上下文。
3. `GET /api/spaces`：列出当前用户可访问的空间；切换空间时调用 `POST /api/auth/select-space`，成功后使用新 token。
4. `GET /api/projects`：按当前 token 中的空间读取项目；创建或更新项目时继续使用同一个空间上下文和 Bearer token。
5. `GET /api/projects/{project_id}/git/branches` 与 `/git/commits`：读取仓库分支和 commit，客户端提交 commit SHA 前应去重并保持稳定顺序。
6. `POST /api/projects/{project_id}/releases`：创建发布。请求包含 `commit_shas`、`strategy` 以及比例字段；相同项目、提交集合、分支、策略和比例重复提交时返回已有发布并标记 `duplicate=true`。
7. `DELETE /api/projects/{project_id}/releases/{release_id}/commits/{sha}`：仅草稿发布允许移除 commit；持久化实现应保留移除状态，不能用物理删除破坏审计和重复发布判定。最后一个有效 commit 不允许移除。
8. `POST /api/projects/{project_id}/releases/{release_id}/publish`：开始或再次发布。该入口只启动当前发布项的第一个未完成环境；首次正常发布即启动 DEV，不会自动串行跑完所有环境。
9. `POST /api/projects/{project_id}/releases/{release_id}/targets/{target_id}/publish`：明确推进一个环境。服务端会强制检查前置环境已经成功，不能跳过 DEV、UAT 或 PRE。
10. `POST /api/projects/{project_id}/releases/{release_id}/targets/{target_id}/retry`：只重试指定的失败环境，已经成功的环境不会重新部署。
11. `GET /api/audit-logs?limit=100`：读取当前空间的操作记录；服务端按 token 中的空间过滤，不能通过参数读取其他空间。

## TTP 发布批次规则

发布项是“某个人要发布的 commit 快照”，发布批次是“多人共享的临时测试分支”，两者不能混为一个整体版本：

- 第一次真正发布时，平台从当时的 `main` 创建一个开放批次分支，并把当前发布项加入其中；后续发布项加入同一个仍开放的批次。
- 每个发布项独立走 `DEV → UAT → PRE → PROD`。点击开始发布只启动 DEV，完成一个环境后，用户再点击“发布到下一个环境”。
- 新发布项加入批次不会改变已经通过的环境，也不会把正在测试的 commit 自动替换掉。要测试新 commit，重新创建发布项，从 DEV 开始。
- PROD 成功后，只合入当前发布项的代码；批次分支只是测试用的共享集合，不能整体合入 `main`。
- 合入前会再次确认 `main` 没有批次之外的新提交。发生冲突时保留发布和测试记录，暂停合入，处理冲突后再重试。
- 发布项可以重复发布；也可以只重试某个失败环境。批次要等其中的发布项都已合入、取消或终止后才能关闭。

## 部署配置编辑器

项目详情里的“部署配置”页面直接编辑 Kubernetes 原生 YAML/JSON，不要求项目里存在 Helm Chart。

- 第一次打开时会生成一个可运行的基础模板，包含 Deployment、Service 和 ConfigMap；点击“放入基础模板”可以随时恢复这份起点，但必须显式保存才会替换已保存内容。
- 一个编辑器可以放多份资源，YAML 文档之间用 `---` 分隔；切换到 JSON 时，多份资源会转换成 JSON 数组，不会丢掉后面的文档。
- “检查配置”会先做浏览器端检查，再由服务端检查语法、资源身份、重复资源、大小上限和项目命名空间。保存成功后版本号递增，后续发布读取该版本。
- 项目命名空间是边界：带 `metadata.namespace` 的资源必须和项目命名空间一致；集群级资源可以不填写 namespace，但真实集群权限仍应由 Kubernetes RBAC 控制。
- API：`GET /api/projects/{project_id}/deployment-config` 读取，`PUT` 保存，`POST .../validate` 校验，`POST .../convert` 转换格式。所有接口都继承当前 JWT 的空间和项目权限。

这个编辑器解决的是“配置怎么写、怎么保存、发布时用哪一版”的问题。当前演示运行时会模拟从 commit 到 Pod 的过程；真实 Kubernetes provider 目前会实际写入并更新 `Deployment`、`Service`、`ConfigMap`、`Secret`、`Ingress` 和 `HorizontalPodAutoscaler`，并等待 Deployment rollout。其它 Kubernetes kind 会在发布前被明确拒绝，直到对应的资源适配器接入，不能把演示成功当成真实集群已经更新。`Secret` 内容会随项目配置保存，生产环境接入前应把存储替换为加密或外部密钥管理，并限制查看权限。

## 集群配置

登录并进入空间后，打开左侧的“集群管理”。点击“添加集群”或已有集群的“编辑配置”，填写集群名称、Kubernetes API Server 地址和连接方式。

- 选择“服务器上的 kubeconfig”时，路径必须是 CI/CD 后端服务器上的路径；K3s 常见路径为 `/etc/rancher/k3s/k3s.yaml`。Context 留空会使用文件中的 `current-context`。
- API Server 地址用于标识和核对目标集群；真正的访问地址、证书和权限由 kubeconfig 提供。后端服务器必须能访问 kubeconfig 中的 `server` 地址，不能把只在另一台机器上有效的 `127.0.0.1` 当成远端地址。
- 选择“集群内身份”时，平台进程需要运行在 Kubernetes Pod 内，并通过 ServiceAccount 访问 API Server。
- 保存后会自动测试连接。测试失败仍会保留配置并将集群标记为“离线”，修正后可以反复点击“测试连接”。kubeconfig 内容和路径不会通过 API 返回给前端。
- 集群连接配置会持久化在服务端；服务重启后，第一次读取项目运行态、监控或执行发布时，平台会自动按保存的 kubeconfig 路径恢复连接，不需要再次手工点击“测试连接”。

本地演示模式使用虚拟运行时，点击测试连接只验证页面流程，不会访问真实 Kubernetes 集群。接入真实集群时，需要关闭演示模式并让后端使用 `CICD_RUNTIME_PROVIDER=kubernetes`，再通过页面登记 kubeconfig 路径。

当前 API 不把 `space_id` 拼在资源 URL 中；服务端从 JWT 的 `space_id` claim 取得空间，并在每次资源访问时校验用户是否属于该空间。切换空间后必须用接口返回的新 token 替换旧 token。

所有服务端查询都必须带 `space_id`，并在资源关系上校验同空间复合外键。管理员的跨空间能力应由授权层显式授予，不应通过省略过滤条件实现。

## 监控面板

当前项目监控页只展示 Pod 运行态：总数、就绪数、运行中数、等待数、失败数、重启次数，以及 Pod 的 namespace、IP、节点和启动时间。列表和统计直接来自当前发布环境的 Kubernetes API，不再使用前端模拟 Pod 或模拟指标。

项目监控页的 Pod 列表和 Pod 详情会从 metrics-server 读取每个 Pod 的 CPU、内存即时使用量；没有指标返回时显示为 `-`，不使用模拟数值。metrics-server 不提供 Pod 磁盘使用量、网络流量或历史曲线，这些指标需要后续接入 Prometheus/cAdvisor 后才能展示。集群管理页的资源指标同样只有在真实指标源接入后才有意义。

## 策略比例

`project_releases` 同时保存四个比例字段，便于灰度和蓝绿发布统一记录：

- `canary`：`stable_percent + candidate_percent = 100`，蓝绿字段必须为 0。
- `blue_green`：`blue_percent + green_percent = 100`，稳定/候选字段必须为 0。
- `rolling`：固定为 stable 100%，其他比例为 0。

比例校验由 MySQL 8 `CHECK` 约束和服务端业务校验共同承担。发布指纹 `release_fingerprint` 应由服务端对规范化后的项目、仓库、分支、策略、比例和排序后的有效 commit SHA 计算 SHA-256。

## 未来接入 client-go

未来的 `client-go` 应作为 API 客户端接入，不直接连接 MySQL 或 Redis：

- 用一个可替换的 HTTP transport 注入 base URL、超时和 TLS；默认不记录 `Authorization`、Cookie 或响应中的敏感字段。
- 登录后保存 token 和过期时间，所有请求自动携带 Bearer token；收到 401 时由调用方决定是否重新登录，不在底层无限重试。
- 将空间上下文作为显式 client 状态或每次请求参数，切换空间后替换 token，避免并发请求误用旧空间。
- 为发布请求提供结构化输入：策略、stable/candidate 或 blue/green 比例、commit SHA 列表；收到 `duplicate=true` 时返回已有发布 ID，而不是重复触发发布。
- 将删除 commit 建模为草稿发布操作，并把 409（发布不可变、最后一个 commit）保留为可判断的错误类型。
- 生产环境优先使用服务端签发的短期 token 和正式密钥管理；本地开发的环境变量不应进入 client-go 的默认配置。

## GitHub / GitLab 仓库读取

演示模式使用内置示例仓库。关闭演示模式后，项目保存的仓库地址会绑定到只读 Git provider，发布页面会从真实仓库读取分支和 commit；开发者不需要编辑 Helm 文件。

```bash
export CICD_DEMO_MODE=false
export CICD_GIT_PROVIDER=auto       # auto、github 或 gitlab
export CICD_GIT_TOKEN='仅放在当前进程环境中'
export CICD_GIT_ALLOWED_HOSTS='git.example.com' # 自建 Git 服务必填，可逗号分隔
# 自建 GitLab/GitHub Enterprise 可指定 API 根地址，例如 https://git.example.com/api/v4
export CICD_GIT_API_BASE_URL=''
export CICD_GIT_TIMEOUT_SECONDS=15
```

`auto` 会识别 `github.com` 和 `gitlab.com`；自建 Git 服务请显式设置 provider，并把仓库/API 的主机加入 allowlist。token 只通过请求头发送，不会拼进 URL、日志或错误响应。当前 provider 只负责读取仓库信息、分支和 commit；真正的镜像构建推送和流量切换仍由后续 registry/release provider 接入，Kubernetes provider 已支持有限范围的原生资源发布。

### 平台 Git 服务账号

所有会改变 Git 引用的操作都使用服务端配置的同一个平台账号，不使用登录用户自己的 Git 账号。这个账号需要先被加入每个目标仓库；平台只在服务端保存 Token，页面只显示账号名和检查结果。

- GitHub：把平台账号作为仓库 Collaborator，权限至少为 `Write`。如果要让平台无条件合并受保护分支，通常还需要 `Maintain` 或 `Admin`，并满足组织的 SSO 和分支保护规则。
- GitLab：把平台账号加入项目，权限至少为 `Developer` 才能创建临时分支；受保护分支的合并通常需要 `Maintainer`，还要满足项目的 Approval、Protected Branch 和 Push Rules。
- 一个 Token 只能对应一个平台账号。服务启动后，发布页会先调用授权检查，确认 Token 身份、目标仓库可见性和写权限；账号不匹配、未加入仓库或只有只读权限时，仍可保存草稿，但“开始发布”和平台内合并会被阻止。

服务端配置示例（不要把真实 Token 写入仓库或日志）：

```bash
export CICD_GIT_SERVICE_USERNAME='cicd-bot'
export CICD_GIT_SERVICE_DISPLAY_NAME='CI/CD 发布机器人'
export CICD_GIT_SERVICE_EMAIL='cicd-bot@example.com'
export CICD_GIT_TOKEN='只在服务进程环境中设置'
```

### Kubernetes 发布等待

真实 Kubernetes 发布在资源写入成功后不会立即算成功。平台会继续读取
Deployment 状态，确认目标版本已经被控制器观察到，并且期望数量的副本都已
更新、Ready、Available；如果控制器报告 `ProgressDeadlineExceeded` 或副本创建
失败，发布会失败并保留原因。默认等待 5 分钟，可以按集群启动时的发布耗时调整：

```bash
export CICD_KUBE_ROLLOUT_TIMEOUT_SECONDS=300
```

这里的等待只适用于真实 Kubernetes provider。演示模式仍然使用内置运行时，便于
在没有集群的情况下走通页面流程。

可用 `GET /api/git/service-account` 查看脱敏后的平台账号，用 `GET /api/projects/{project_id}/git/access` 检查当前项目仓库。响应不会包含 Token、密码或私钥。当前内置 GitHub/GitLab provider 的远程写操作仍以 `MergeOperator` 能力为准；如果 provider 只支持读取，页面会明确显示“暂不可发布”，不会假装已经完成分支创建或合并。
