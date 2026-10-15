# code-audit-web

Vue 3 + TypeScript + Vite + Element Plus 最小控制台。

- 登录（走 `/api/v1/auth/login`）
- 项目（列表 / 新建）
- 扫描任务（列表，5 秒轮询）
- 新建扫描（先跑 Agent；创建后立刻返回 `pending`）
- 扫描详情（3 秒轮询 `GET /scans/:id`，展示阶段/进度/漏洞表）

```bash
cd code-audit-web
npm install
npm run dev      # http://127.0.0.1:5173，/api 代理到 :8080
npm run build
```

先启动：

```bash
cd code-audit-server && go run ./cmd/server        # 127.0.0.1:8080
cd code-audit-agent && AXIOM_SCAN_MOCK=1 go run ./cmd/agent   # 可选，消费任务
```
