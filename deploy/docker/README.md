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

首次导入时请先清空旧卷（如果本地已有旧数据）：

```bash
cd deploy/docker
docker compose down -v
```

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

输入合同接入后，Server、Agent 和 Scanner 应一起重建。Dockerfile 自动构建 contracts TS 包与 Scanner，Agent 优先启动编译后的 Scanner。Server 自动应用 截至 009_observation_contract 的增量迁移；旧任务预算保持 NULL。新快照任务需要支持 job 输入和预算能力的执行节点，网页会显示不兼容时的等待原因。用户通过网页操作，内部 job/描述接口无需手动执行。

- Server/Agent/Web 代码更新后，建议执行 `docker compose build <service>` 再启动。
- 如果你需要本地扫描执行但又不想消耗模型额度，保持 `AXIOM_SCAN_MOCK=1`。
