# Axiom 自托管部署

正式配置为 `docker/compose.production.yml`：PostgreSQL、Go Server、Go Agent / Node Scanner、Nginx 静态 Web。现有 `docker/docker-compose.yml` 保留迁移环境的后端、端口和数据卷，Web 同样默认使用静态 Nginx；Vite 通过显式开发覆盖配置启用。

迁移配置通过私有 `.env` 中的 `AXIOM_IMAGE_PREFIX` 和 `AXIOM_RELEASE` 选择版本镜像，默认前缀为 `axiom-local`。升级时使用唯一标签，保留旧镜像和原凭据；应用镜像固定 `linux/amd64`，与锁定的 SDK 运行基线一致。Web 宿主机端口仍为 5173（可用 `AXIOM_WEB_PORT` 调整），Nginx 容器内端口为 8080，PostgreSQL 与共享数据卷保持原配置。已部署版本可用 `docker compose --env-file deploy/docker/.env -p docker -f deploy/docker/docker-compose.yml up -d --no-build --no-deps server agent web` 重新应用；升级前仍须完成空队列一致性备份。旧 Vite 镜像需要先切换为配套静态镜像，不能直接套用新基础配置。

当前 SDK 0.5.0、Scanner description 14、结果适配器 2，Server 迁移截至 028。[组件化审计执行](../docs/SDK_COMPONENT_SCANS.md) 和计价更新都需要配套 Server、Agent（包含 Node Scanner）和 Web；历史 input 6 / Job 8 等组合保持读取。开发和生产 Web Dockerfile 均安装合同包锁定的 AJV 运行时依赖。冻结组件计划后，用户在 Web 另行配置并启动组件审计；跨组件匹配费用未知，须明确确认，不能视为受扫描预算硬限制。升级前保留数据库、共享产物和旧镜像；创建新版计划或组件执行数据后，回退须使用独立恢复验证，不能只凭旧 Server 在空新增表上就绪就认定可写入兼容。

只更新现有部署的 Web 时，在私有 `.env` 设置 `AXIOM_WEB_RELEASE` 为已验收的静态 Web 标签，再执行上述命令的 `web` 单服务版本：`docker compose --env-file deploy/docker/.env -p docker -f deploy/docker/docker-compose.yml up -d --no-build --no-deps web`。Server、Agent 和数据卷仍按原配置运行。该变量为空时跟随 `AXIOM_RELEASE`；后续整套升级时应清空它或明确选择配套 Web 版本。静态版本之间回滚可恢复旧 Web 标签；跨 Nginx/Vite 回滚须同时恢复对应 Compose 配置、启动命令与容器端口，见 [静态 Web 运行基线](../docs/STATIC_WEB_BASELINE.md)。涉及数据库或合同变更时仍按下文恢复流程处理。

适用范围：可信用户、自托管，各服务共置于同一主机。当前 SDK 基线验证 Linux x64，正式配置固定 Server/Agent 为 `linux/amd64`；Apple Silicon Docker 使用模拟执行。DeepSeek 小项目已通过有限真实工作流验收，新计价实际费用与更多场景仍待验证；状态见 [当前状态](../docs/CURRENT_STATUS.md)。参考部署保持 DeepSeek + Responses + 原生 Schema，不自动切换格式或重试。

当前源码候选为 `0.1.0-beta.2`。完整的新安装、配置切换及回滚步骤见 [Beta 安装指南](../docs/BETA_INSTALLATION.md)，开源发布与 CI 门槛见 [发布流程](../docs/RELEASE_PROCESS.md)。

## 安装

在仓库根目录生成私有配置，文件放在仓库外：

```bash
python3 deploy/ops/stack.py init /absolute/private/path/production.env --project axiom-prod --port 8088
```

工具生成数据库密码、管理员初始密码、JWT secret、Agent token 和 32 字节 base64url master key，文件权限为 0600，输出只显示位置。读取文件取得初始密码，后续在 Web 修改。

发布前将 `AXIOM_RELEASE=local` 改为唯一发布标识，保留各版本镜像，勿复用发布标签：

```bash
docker compose --env-file /absolute/private/path/production.env -f deploy/docker/compose.production.yml build
docker compose --env-file /absolute/private/path/production.env -f deploy/docker/compose.production.yml up -d --no-build --wait --wait-timeout 120
```

基础镜像按本轮已验证的 digest 固定。Agent 构建保留公开 npm 缓存；缓存完整时可用 `build --build-arg AXIOM_NPM_OFFLINE=1 agent` 禁止 npm 回退下载，缺失缓存会明确失败。此模式保留锁文件及 SDK 原生包/7 个补丁的完整校验，依赖升级需同步重新验收。

默认 Web 为 `http://127.0.0.1:8088`，Server 与 PostgreSQL 无宿主机端口。外部访问由宿主机 HTTPS 代理接入 Web。Web 同源代理 API，`/api/internal/` 只供 Agent 在私有 Server 网络访问。

模型配置、上传、设置预算和范围、启动、取消、重试、查看证据及结果均在浏览器完成。以下命令供管理员维护。

## 配置与运行状态

- 生产模式在连接数据库前校验密钥、密码与 PostgreSQL 数据源，开发模式保留旧默认值。
- `AXIOM_SCAN_MOCK=0` 为正式默认值。测试 SDK 替身不装入发布镜像。
- `AXIOM_MAX_RECOVERIES=0` 为正式默认值，租约过期后由用户在 Web 决定重试。设置 1–10 可允许对应次数的自动重新执行。恢复创建新尝试，可能再次计费；启用任务总预算时，过期未知费用须先在 Web 核对，不自动恢复。
- Web 可分别设置单次估算上限与任务总预算。重试取单次上限与剩余额度中的较小值；未知费用及耗尽状态会阻止累计预算重试。历史任务保持原有单次语义。费用均为估算，详见 [预算与对账基线](../docs/TASK_BUDGET_BASELINE.md)。
- Web 系统设置 → 存储与清理：默认领取前保留 1024 MiB，探测失败或空间不足时暂停新领取，既有心跳、取消和完成提交继续处理。遗留目录默认宽限 24 小时，预览后须明确执行；历史报告和未知费用证据不自动删除。失败可从记录继续，规则见 [存储生命周期基线](../docs/STORAGE_LIFECYCLE_BASELINE.md)。
- `/healthz` 检查 Server 存活，`/readyz` 实测数据库连接；Agent health 要求最近 45 秒内成功上报 presence。Compose 等待依赖就绪。
- 日志写入 Docker，单文件 10 MiB、保留 3 个；使用 `docker compose ... logs --tail 200 server agent web` 查看。
- 应用容器使用非 root、只读根文件系统及独立 `/tmp`；`/app/data` 保存共享快照和产物。共享目录模型面向可信用户。
- Unix Agent 退出时停止 Scanner 进程组；Scanner 发现父 Agent 已退出时停止整组，心跳 deadline 限制在租约安全窗口内。Windows 强杀进程树仍待覆盖。

## 一致性备份

先在 Web 完成或取消队列中的任务：

```bash
python3 deploy/ops/stack.py backup /absolute/private/path/backups/release-20261014 --env /absolute/private/path/production.env
python3 deploy/ops/stack.py verify /absolute/private/path/backups/release-20261014
```

工具关闭 Web 写入口、排空 Server 请求，确认没有排队或执行中的任务后才停止 Agent。队列非空则拒绝备份并恢复服务。数据库、产物、配置在同一停止写入窗口导出。此工具仅支持本 Compose 管理的 PostgreSQL，拒绝与 POSTGRES_* 不匹配或外部数据库地址。

未完成的清理意图和 `.axiom-trash` 隔离目录会一起备份。恢复后不会自动清理，管理员在 Web 查看原记录并明确继续；不要手动删除隔离目录或清理记录。源管理根配置改变时保留数据并拒绝自动重定位。

目录权限 0700、文件 0600，包含 `database.dump`、`data.tar.gz`、`runtime.env`、校验清单及应用镜像 ID。配置和 SDK 状态可能含凭据，当前未加密，异地保存需使用你管理的加密存储。归档仅支持普通文件与目录，遇到链接、设备或不安全路径会拒绝。

必须保留原 master key。新账户密码使用独立的 Argon2id，旧 HMAC 账户在成功登录后迁移；尚未迁移时更换 JWT secret，须以 `AXIOM_LEGACY_PASSWORD_SECRET` 保留原签名密钥用于验证。配置里的管理员密码仅用于初次建户；Web 改过后，恢复仍使用修改后的密码。恢复保留账户和历史数据，并撤销旧会话，所有浏览器需要重新登录。账户管理、会话撤销和操作记录入口均在 Web 系统设置；规则见 [认证基线](../docs/AUTHENTICATION_BASELINE.md)。

## 恢复、升级与回滚

恢复到从未使用过的项目、新配置文件和新端口：

```bash
python3 deploy/ops/stack.py restore /absolute/private/path/backups/release-20261014 --env /absolute/private/path/restored.env --project axiom-restore-20261014 --port 8089
```

先核对大小、SHA-256 和归档路径，拒绝覆盖已有项目、容器、卷或配置。要求本地应用镜像 ID 与备份记录一致；换机器先导入原发布镜像，数据备份不含镜像。恢复失败时新项目保留供排查，原项目保持原状。

升级前空队列备份并保留旧镜像，依次更新 Server（消费者及迁移）、配套 Agent/Scanner，再更新 Web；组合与拒绝规则见 [合同兼容矩阵](../contracts/COMPATIBILITY.md)。Server 启动时串行执行迁移。步骤 5 将数据库与历史 SQL 移到 Server 内部，SQL 001–009 的字节及编号保持不变；步骤 6 新增 010，仅保存节点运行诊断与上报时间，不改已有任务/attempt 格式；Agent 只通过 HTTP 访问控制面。原 `AXIOM_DATABASE_URL`、共享产物目录及旧 SQLite 配置保持兼容。Server 镜像包含管理员 SQLite 导入工具 `/opt/axiom/bin/axiom-migrate`，需要使用原 master key。正式 Agent 镜像通过 AXIOM_BUILD_VERSION 注入 AXIOM_RELEASE 标识，Web 可查看构建标识、Scanner 包/描述版本及能力；本地普通构建显示 development。构建标识用于定位配套镜像，合同能力与矩阵摘要决定兼容，不能仅比较包标签。历史任务、日志、结果、覆盖与 HTML/PDF 读取器保持，兼容适配器不因版本标签变化而删除。

单独退回二进制不保证迁移与合同兼容；回滚用旧备份及对应镜像恢复到新项目，经 Web 核对登录、模型、快照、结果和下载后，再切换 HTTPS 入口。

实际部署项目勿执行 `down -v`。测试清理仅作用于事先确认不存在的专用验收项目。

## 无费用验收

静态 Web 代理可独立验收，不连接业务数据库或真实身份：

```bash
python3 deploy/ops/static_web_acceptance.py --image axiom/web:<release> --work /absolute/private/path/static-web-acceptance
```

测试在内部 Docker 网络启动模拟后端，浏览器使用显式 Fixtures / Mock 身份，检查 SPA 刷新、缓存、SSE、流式上传下载、故障恢复与后端 IP 变化；可用 `--baseline-image <previous-vite-image>` 额外验证旧 Web 回退。工作目录必须尚不存在。完整说明见 [静态 Web 运行基线](../docs/STATIC_WEB_BASELINE.md)。

```bash
python3 deploy/ops/stack.py init /absolute/private/path/acceptance.env --project axiom-accept-local --port 0
docker compose --env-file /absolute/private/path/acceptance.env -f deploy/docker/compose.production.yml build
python3 deploy/ops/acceptance.py --env /absolute/private/path/acceptance.env --work /absolute/private/path/acceptance-work
```

测试挂载明确标记 Fixture 的 SDK 替身，Agent 网络阻断外部连接。验证静态 Web、真实浏览器、重启/替换、数据库断连、备份恢复及损坏/覆盖保护。需要 Web 的 Playwright 依赖和 Chromium，CI 自动安装。结束后清理本轮项目与卷，测试备份和报告留在私有目录。

配置依据：[Compose 就绪顺序](https://docs.docker.com/compose/how-tos/startup-order/)、[Docker 卷持久化](https://docs.docker.com/engine/storage/volumes/)、[Nginx 代理参数](https://nginx.org/en/docs/http/ngx_http_proxy_module.html)。

大源码、跨租约长任务、同模型并行、Agent 强制退出与 Web 恢复，以及固定内部提交的镜像安装升级回滚，见 [无模型稳定性与发布验收](../docs/STABILITY_RELEASE_BASELINE.md)。该流程使用新项目及合成数据，继续保留真实 SDK 与长期运行的验证边界。
