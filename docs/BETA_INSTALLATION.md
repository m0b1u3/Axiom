# Beta 安装、升级与回滚

发布候选：`0.1.0-beta.1`。本指南适用于 Linux amd64、单机、可信用户、自托管；Windows、ARM64、多租户和跨主机共享文件系统未认证。所有扫描配置与操作在 Web 完成，以下命令仅供管理员安装和维护。

## 安装前

需要 Docker Engine、Compose v2（支持 `up --wait`）、Python 3.10+ 与 Git；按源码构建需能访问锁定的 npm/Go 依赖和基础镜像仓库。当前提供源码候选与 Dockerfile，尚未发布预构建应用镜像。使用空闲端口，数据放在本机 Docker 卷，不与其他项目共用卷。

当前自动验收使用 SDK 替身，不代表真实供应商费用、真实工具权限和长期审计质量已经验证。正式镜像保留真实 SDK；用户在 Web 启动真实任务会访问所配置的模型服务。本轮发布准备不执行这些任务。

从 [仓库](https://github.com/m0b1u3/Axiom) 取得通过 CI 的完整提交或源码候选。源码候选附 SHA-256 与 `SOURCE_MANIFEST.json`，先核对归档校验，再解压并运行：

```bash
python3 deploy/release/source.py verify .
python3 deploy/release/check.py
```

直接 Git checkout 没有生成的 `SOURCE_MANIFEST.json`，只运行第二条。不要使用旧 `release/` 本地安装副本作为源码候选。

## 新安装

在仓库根目录生成私有配置；配置和备份放到仓库外。命令不会打印密钥。

```bash
python3 deploy/ops/stack.py init /absolute/private/axiom/production.env --project axiom-prod --port 8088 --release "$(cat VERSION)"
docker compose --env-file /absolute/private/axiom/production.env -f deploy/docker/compose.production.yml config --quiet
docker compose --env-file /absolute/private/axiom/production.env -f deploy/docker/compose.production.yml build
docker compose --env-file /absolute/private/axiom/production.env -f deploy/docker/compose.production.yml up -d --no-build --wait --wait-timeout 120
```

用本机编辑器读取私有配置中的初始管理员账号和密码，打开 `http://127.0.0.1:8088` 登录并在 Web 修改密码。检查系统设置中的执行节点与组件兼容状态。上传、模型配置、预算、范围、启动、取消、重试和报告下载均在 Web 完成。外部访问先配置宿主机 HTTPS 代理。

发布标签必须唯一，保留对应源码和镜像。应用镜像携带版本、源码仓库及许可证标签；精确镜像 ID 由备份清单保存。不要用同一个标签覆盖旧镜像。

## 升级候选

首个公开版本没有前一公开版本；当前验收覆盖冻结 PostgreSQL 012 → 013 迁移、同版本容器替换，以及可选内部镜像 → 候选镜像的升级。它们不表示已认证任意旧二进制组合。

1. 在 Web 完成或取消队列中的任务，下载需要单独保存的报告。
2. 使用原版本源码和配置备份；保留原应用镜像与 master key。备份工具在队列非空时拒绝执行。
3. 从新版本源码准备新配置，仅改变镜像选择，保留项目、密钥、数据库和数据卷。

```bash
python3 deploy/ops/stack.py backup /absolute/private/axiom/backups/before-beta --env /absolute/private/axiom/production.env
python3 deploy/ops/stack.py verify /absolute/private/axiom/backups/before-beta
python3 deploy/ops/stack.py retarget /absolute/private/axiom/production.env /absolute/private/axiom/candidate.env --release "$(cat VERSION)"
docker compose --env-file /absolute/private/axiom/candidate.env -f deploy/docker/compose.production.yml build
docker compose --env-file /absolute/private/axiom/candidate.env -f deploy/docker/compose.production.yml stop web agent server
docker compose --env-file /absolute/private/axiom/candidate.env -f deploy/docker/compose.production.yml up -d --no-build --wait --wait-timeout 120 server
docker compose --env-file /absolute/private/axiom/candidate.env -f deploy/docker/compose.production.yml up -d --no-build --wait --wait-timeout 120 agent
docker compose --env-file /absolute/private/axiom/candidate.env -f deploy/docker/compose.production.yml up -d --no-build --wait --wait-timeout 120 web
```

备份完成到停止 Web 之间保持维护窗口，避免新提交任务。升级按 Server 消费者、Agent/Scanner、Web 顺序进行。任一步失败即保留现场并停止后续更新，依据备份恢复到新项目。登录 Web 核对节点兼容、历史任务、完整结果、产物下载、预算、未知费用及清理记录；更新日常维护所用的配置路径。

## 回滚

回滚使用升级前备份、原版本源码及原镜像 ID，恢复到新项目和新端口。数据备份不包含镜像；换主机时需另行保存、加载原镜像。不要将旧二进制直接放回已经迁移的数据库。

```bash
python3 deploy/ops/stack.py restore /absolute/private/axiom/backups/before-beta --env /absolute/private/axiom/rollback.env --project axiom-rollback-beta --port 8089
```

恢复成功后重新登录 `http://127.0.0.1:8089`。旧会话应被撤销，账号、模型凭据、备份时的报告、费用与清理意图应保留；备份之后产生的任务不属于该恢复点。恢复不会自动继续清理。Web 核对完成后再切换 HTTPS 入口并停止失败版本，保留原项目以便排查。

实际业务项目不要执行 `down -v`。详细的备份范围、权限、日志和拒绝保护见 [部署指南](../deploy/README.md)。
