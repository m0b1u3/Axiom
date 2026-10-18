# Axiom 本地 Docker 迁移启动

此配置用于开发与迁移，Web 运行 Vite。正式自托管使用独立的 [部署指南](../README.md) 和 `compose.production.yml`，现有 `.env` 与开发服务无需更改。

本目录提供从 VPS 迁移到本地 Docker 的最小运行栈：

- PostgreSQL 16
- Axiom Server
- Axiom Agent
- Axiom Web（Vite 开发态）

## 一次性准备

1. 进入仓库根目录。
2. 复制示例环境变量：

```bash
cp deploy/docker/.env.example deploy/docker/.env
```

3. 按实际填写 `.env`，至少设置：
   - `AXIOM_JWT_SECRET`
   - `AXIOM_AGENT_TOKEN`

## 使用 VPS 备份恢复数据库（可选）

如果你要把 VPS 的数据库迁移到本地：

1. 先按你的密钥解密导出的 `axiom-vps-full.tar.gz.enc`
2. 从解密后的归档中取出 `axiom.dump`
3. 把 `axiom.dump` 放到 `deploy/docker/backups/axiom.dump`
4. 确认 `.env` 有：
   - `AXIOM_DUMP_FILE=axiom.dump`
   - `AXIOM_DUMP_DIR=./backups`

> `postgres` 会在首次初始化时自动执行 `deploy/docker/postgres-init/01-axiom-restore.sh` 做还原。

首次导入应使用全新的 Compose 项目及数据卷。已有部署先备份数据库和共享产物，在独立环境验证恢复；不要清空承载历史任务与报告的现有数据卷。维护步骤见 [部署指南](../README.md)。

### 远端直接抓包（可选）

在有网络权限的机器上直接从 VPS 抓：

```bash
scp -i /absolute/private/id_ed25519 admin@example-host:/absolute/private/backups/<backup-dir>/axiom.dump \
  deploy/docker/backups/axiom.dump
```

也可以先抓取 `task-management-*/source-*.tar.gz`，再本地 `tar -xzf` 找到 `axiom.dump` 再放入 `backups/axiom.dump`。

## 启动

```bash
cd deploy/docker
docker compose up -d --build
```

如果你的 Docker 没有运行在默认环境，先切换上下文（例如 Desktop）：

```bash
docker context use desktop-linux
```

## 访问

- Web：`http://127.0.0.1:5173`
- API：`http://127.0.0.1:8080`
- 默认账号是 `admin / <AXIOM_ADMIN_PASSWORD>`

## 继续开发

合同更新后，Server、Agent/Scanner 和 Web 应同步重建。Dockerfile 自动构建 contracts TS 包与 Scanner，Agent 优先启动编译后的 Scanner，SDK 固定为 0.1.25。Server 自动应用截至 `021_sdk_identities.sql` 的增量迁移，旧迁移及历史任务语义保持不变。组件审计任务仍使用 input v6 / job v8；当前 ScannerDescription v10 追加 SDK 身份版本 1，并保留历史描述与 Job 读取；网页会显示不兼容时的等待原因。用户在 Web 管理并冻结组件计划，再配置预算与并发、确认未知匹配费用后启动审计；失败组件重试、费用核对、历史证据与报告查看也在 Web 完成。Server 和 Agent 镜像仍安装 Git，原 base/head 差异流程继续保留。

- Server/Agent/Web 代码更新后，建议执行 `docker compose build <service>` 再启动。
- 如果你需要本地扫描执行但又不想消耗模型额度，保持 `AXIOM_SCAN_MOCK=1`。

## SDK 执行身份部署（2026-10-18）

现有本地业务与历史演示应用已同步更新到 `sdk-identities-20261018-01`，SDK 固定 0.1.25。设置 → SDK 执行身份提供 API Key、ChatGPT 设备码、账户查询、退出及撤销；任务绑定身份、Agent ID 和凭据版本，离线清理状态在 Web 保留。Agent 的 DataDir 和 ID 应保持稳定；持久会话位于该数据卷的 `.sdk-identities/`，不放进公开产物。Docker 构建上下文排除此目录。

验收使用离线备份副本、内部网络及显式 SDK Fixture；现有安装没有写入模拟身份、任务或快照，原 `AXIOM_SCAN_MOCK=1` 保留。这个变量只控制扫描模拟，认证验收需要上述独立隔离措施。备份和浏览器记录见 [本批部署说明](../../docs/SDK_IDENTITIES_DEPLOYMENT.md)。
