# TTP · The Turbocharged Platform

本平台提供按空间隔离的项目、集群和发布控制面基础设施。当前仓库范围包含 MySQL 8 schema、本地开发 Compose 和接入文档；`backend`、`frontend` 的实现由各自模块负责。

## 快速开始

```bash
cd ttp
export MYSQL_DATABASE=cicd_platform
export MYSQL_USER=cicd_app
export MYSQL_ROOT_PASSWORD="$(openssl rand -hex 24)"
export MYSQL_PASSWORD="$(openssl rand -hex 24)"
export CICD_BOOTSTRAP_ADMIN_PASSWORD="$(openssl rand -base64 24)"
docker compose -f deploy/docker-compose.yml up -d mysql
```

首次启动后端时，`CICD_BOOTSTRAP_ADMIN_PASSWORD` 只在数据库还没有 `admin` 用户时用于创建管理员；已有管理员不会被覆盖。Redis 不是必需依赖，需要时执行 `docker compose -f deploy/docker-compose.yml --profile cache up -d`。Compose 不包含任何真实凭据；密码只存在于当前终端或本地未提交的环境文件中。

## 文件

- [`migrations/001_init.sql`](migrations/001_init.sql)：初始化 `users`、`spaces`、`space_members`、`projects`、`clusters`、`project_deployment_configs`、`project_releases`、`release_commits`、`release_state`、`release_batch_state`、`audit_logs`。
- [`deploy/docker-compose.yml`](deploy/docker-compose.yml)：MySQL 8.0.36 和可选 Redis 7.2。
- [`docs/local-development.md`](docs/local-development.md)：启动、连接、登录说明，API 流程、比例约束和未来 `client-go` 接入约定。

## 数据模型约定

业务资源都通过 `space_id` 归属空间。项目到集群、发布到项目、发布 commit 到发布记录均使用外键约束；项目级唯一索引和空间级查询索引用于避免跨空间串数据。

发布使用 `release_fingerprint` 做项目内幂等键，`release_commits` 使用发布和 SHA 复合主键防止重复 commit，并以移除标记保留草稿编辑和审计所需的历史。`project_releases` 保存灰度的 stable/candidate 比例以及蓝绿的 blue/green 比例，并用 `CHECK` 约束校验总和。

## TTP 发布模型

发布记录和发布批次是两个概念：发布记录属于某个开发者，绑定用户选中的分支和 commit 快照；批次是从 `main` 创建的一条共享测试分支，用来承载当前正在验证的多个发布项。批次不是一个“大版本”，也不能把整个批次直接合入 `main`。

一次发布的实际操作是：

1. 选择分支和一个或多个 commit，创建发布项。
2. 第一次真正点击发布时，平台以当时的 `main` 为基准创建开放批次，并把当前发布项加入批次；后续发布项继续加入同一个开放批次。
3. 每个发布项都按 `DEV → UAT → PRE → PROD` 独立推进。第一次发布请求只启动 DEV，后续环境必须由用户明确点击推进；其他人的发布项加入批次不会让已经完成的环境重新测试。
4. 发布项使用创建时保存的 commit 快照。仓库后来出现新 commit 不会悄悄替换正在测试的版本；要发布新 commit，重新创建一个发布项，它会从 DEV 重新开始。
5. PROD 成功后，只把当前发布项的代码合入 `main`。平台会检查 `main` 是否仍是批次记录的当前版本；发生变化或冲突时停止合入并要求重新确认/处理，绝不会用整个批次覆盖 `main`。

批次可以一直开放，直到其中的发布项都已合入、取消或终止，再由有权限的成员关闭。发布项可以重复发布；重试单个失败环境时，已经成功的环境不会再次部署。

详细本地流程见 [`docs/local-development.md`](docs/local-development.md)。正式模式不会在接口失败时回退到演示数据；业务状态来自 MySQL，Git 分支/提交来自配置的 Git 服务，Pod 和运行指标来自 Kubernetes。演示 Provider 只保留给单测和显式 `CICD_DEMO_MODE=true` 的本地演示。

本地无 MySQL 时可以使用演示存储。此时请设置 `CICD_DEMO_ADMIN_PASSWORD`，登录用户为 `admin`；不设置时平台会生成不可预知的临时密码，不会创建公开的默认密码。

集群连接配置入口在登录后的“集群管理”页面：可以登记 API Server 地址、kubeconfig 路径或集群内身份，并在保存后测试连接。具体字段含义和 K3s 路径见 [`docs/local-development.md`](docs/local-development.md) 的“集群配置”一节。

## 原生 Kubernetes 配置

Kubernetes 是真正负责创建和运行 Pod、Service、ConfigMap 等资源的系统；YAML/JSON 是告诉 Kubernetes“希望资源长什么样”的原生配置文件。Helm 只是把一组 YAML 做成可复用模板和安装包，不是 Kubernetes 的必需依赖。

本平台默认走更直白的方式：进入项目的“部署配置”，直接编辑完整的 Kubernetes YAML 或 JSON。多个资源可以放在同一个编辑器里，用 `---` 分隔；页面会自动格式化、检查 `apiVersion`、`kind`、`metadata.name`、重复资源和项目命名空间，并在保存后记录配置版本。发布流程会读取项目当前保存的 Manifest，演示模式会把它带入模拟发布运行时。

因此，普通开发者不需要为了发布去编写 Helm。只有在团队已经有 Helm Chart、需要按环境批量套模板时，才适合在后续接入 Helm 渲染；它和原生 YAML 编辑可以并存，不互相强制依赖。

当前代码已经完成 Manifest 的编辑、校验、保存和发布请求传递。真实 Kubernetes provider 目前会直接处理常见的命名空间资源：`Deployment`、`Service`、`ConfigMap`、`Secret`、`Ingress` 和 `HorizontalPodAutoscaler`；编辑器会逐个标出当前发布器能否处理的资源。其它资源仍可保存为配置，但发布前会明确拒绝，避免误以为已经应用。镜像构建推送和流量切换仍由后续 builder/registry 与流量 provider 接入；provider 同时负责读取 Pod、日志、基础状态和修改 Pod 所属 Deployment。

真实 Kubernetes provider 发布 `Deployment` 后会继续等待 rollout：只有目标版本被控制器观察到、期望副本全部更新且 Ready/Available，发布才会成功；超时或控制器报告失败会把原因写入发布记录。默认等待 5 分钟，可通过 `CICD_KUBE_ROLLOUT_TIMEOUT_SECONDS` 调整。镜像构建推送和非滚动流量编排仍未接入，不能把当前链路当作完整生产 CI/CD。

真实 Git 仓库接入的环境变量和安全边界见 [`docs/local-development.md`](docs/local-development.md) 的“GitHub / GitLab 仓库读取”一节。

发布前请把服务端配置的 Git 平台账号加入目标仓库：GitHub 至少 `Write`，GitLab 至少 `Developer`；受保护分支的合并还要按平台规则授予更高权限。平台会在发布页检查 Token 对应账号和仓库权限，开发者不需要提供个人 Git 凭据。配置项和接口说明见 [`docs/local-development.md`](docs/local-development.md) 的“平台 Git 服务账号”一节。
