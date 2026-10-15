# code-audit-server

Go HTTP API、业务数据库、任务调度与结果索引。**创建扫描立刻返回 pending。** Server 独占业务 PostgreSQL 连接、迁移、Provider 预设和密钥解密；Agent 通过现有 HTTP 控制接口领取和上报。

产品操作入口为 Web；下方命令和 curl 示例只用于开发、接口调试与管理员维护。正式部署见 [部署指南](../deploy/README.md)。

默认：

- listen `127.0.0.1:8080`
- 数据源优先 `AXIOM_DATABASE_URL`；SQLite 仅保留历史读取、迁移和单元测试兼容
- 旧 SQLite 默认路径 `../code-audit-agent/data/agent.db` 与 `AXIOM_AGENT_DB` 保留在 Server 配置层，Agent 不读取
- 首启确保用户 `admin` / `admin`（可用 `AXIOM_ADMIN_PASSWORD` 改）

```bash
cd code-audit-server
go test ./...
go run ./cmd/server
```

```bash
curl -s -X POST http://127.0.0.1:8080/api/v1/auth/login \
  -d '{"username":"admin","password":"admin"}' -H 'Content-Type: application/json'

curl -s http://127.0.0.1:8080/api/v1/projects -H "Authorization: Bearer TOKEN"
curl -s -X POST http://127.0.0.1:8080/api/v1/scans \
  -H "Authorization: Bearer TOKEN" -H 'Content-Type: application/json' \
  -d '{"sourceSnapshotId":1,"provider":"openai","model":"gpt-5.6-sol"}'
```

Agent 服务在 `code-audit-agent` 目录启动。正式并发执行需要 PostgreSQL、`AXIOM_SERVER_URL` 和 `AXIOM_AGENT_TOKEN`；扫描配置及启动均在 Web 完成。

## 数据库归属与维护

Store、SQLite 导入、PostgreSQL 迁移位于 `internal/store`；历史 SQL 001–009 及 SQLite schema 保持原字节。Server 启动时执行迁移。迁移工具已移到 Server：

```bash
cd code-audit-server
# 由管理员设置 AXIOM_DATABASE_URL 与原 AXIOM_MASTER_KEY，勿指向测试库
# 只导入历史 SQLite；不会启动扫描或模型
go run ./cmd/migrate --source /absolute/path/to/legacy.db
```

正式 Server 镜像包含 `/opt/axiom/bin/axiom-migrate`。旧 `agent enqueue` 管理工具移至 `cmd/enqueue`，仅保留管理员兼容入口；用户创建任务继续使用 Web。

步骤 7 新增增量 SQL 011：Argon2id 密码、持久化会话、admin/operator/viewer 权限、登录限制和操作记录。旧密码成功登录后迁移；退出、改密、角色变更和停用会在 Server 撤销会话。旧 Web 令牌需重新登录，历史任务和报告保留。Web 系统设置提供账户和会话管理；详细接口、升级和边界见 [认证基线](../docs/AUTHENTICATION_BASELINE.md)。

## 上传 ZIP → Source Snapshot

```bash
curl -s -X POST http://127.0.0.1:8080/api/v1/uploads \
  -H "Authorization: Bearer TOKEN" -F "file=@fixtures.zip"
```

服务端 `internal/snapshot` 消毒解压（仅 `.zip / .tar.gz / .tgz`）：

- 拒绝：Zip Slip / 绝对路径 / `C:\` 盘符 / ADS 冒号 / 符号链接 / 硬链接 / tar 特殊文件
- 限额：归档 2GB、解压 10GB、文件 20 万、单文件 100MB、zip 压缩比 1000（防炸弹）
- 同归档 SHA256 去重；目录即不可变快照 `data/snapshots/snap-<sha16>/`
- 生成 `content_hash`（内容树哈希，与文件顺序无关）用于版本对比

库表 `source_snapshots`；`GET /api/v1/snapshots` 列出。扫描引用 `snapshotPath`（见 `deploy/e2e-upload-smoke.py` 冒烟）。

## HTML / PDF 报告

- `GET /api/v1/scans/{id}/report` — HTML（attachment）
- `GET /api/v1/scans/{id}/report.pdf` — 服务端渲染 HTML 后经 headless 浏览器（Edge/Chrome，`AXIOM_BROWSER` 可覆盖）`--print-to-pdf` 转 PDF；未找到浏览器返回 501

## HTML 报告

```bash
curl -s -o report.html http://127.0.0.1:8080/api/v1/scans/1/report \
  -H "Authorization: Bearer TOKEN"
```

报告由服务端从数据库生成（统计 + 漏洞明细 + 修复建议），内容经 HTML 转义，Web 端「导出报告」按钮走同一接口。
