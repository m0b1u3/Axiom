# 静态 Web 运行基线

`deploy/docker/docker-compose.yml` 与 `compose.production.yml` 均使用 `Dockerfile.web.production` 构建 Nginx 静态 Web。迁移配置保留宿主机端口 5173、现有数据库、共享产物卷和后端服务定义；它仍不等同于完整生产配置。Go Server、Go Agent、Node Scanner、SDK 0.1.25 及 HTTP / 共享产物 / JSONL 传输保持不变。

## Web 运行方式

Web 在构建时编译 Vue，运行时使用非 root Nginx，容器内端口 8080。根文件系统只读，临时文件使用 `/tmp`，移除 Linux capabilities 并启用 `no-new-privileges`。日志写入 Docker，单文件 10 MiB、保留 3 个；Web 不挂载业务数据或 SDK 身份目录。

- SPA 深层路径刷新回到 `index.html`；入口文档不持久缓存，带内容哈希的 `/assets/` 缓存一年并标记 immutable。缺失资源返回 404，避免把 HTML 当作 JavaScript。
- `/api/` 同源转发至 `server:8080`，保留认证头、附件名与响应状态。上传和响应关闭代理缓冲，SSE 及时送达；单请求体上限为 2049 MiB，与 Server 的 2 GiB 归档限制及 multipart 开销配合。后端仍执行自己的上传和业务校验。
- API 连接超时 5 秒、读取空闲超时 120 秒。Docker DNS 每 10 秒重新解析，Server 替换后无需重启 Web。
- `/healthz` 只检查 Web 存活，`/readyz` 检查 Server 及数据库就绪，后者用作容器健康检查。后端故障不会被表示为 Web 就绪。
- 用户入口拒绝 `/api/internal/` 和 Vite 的源码、依赖及 HMR 路径；Agent 仍直接使用私有 Server 网络。
- 缓存位置也包含 `nosniff` 与 `same-origin` Referrer-Policy 响应头。

## 开发与升级

Vite 开发覆盖文件为 `deploy/docker/compose.web-development.yml`，要求 Docker Compose 2.24.4+。在仓库根目录执行：

```bash
AXIOM_WEB_RELEASE=web-development docker compose --env-file deploy/docker/.env -p docker \
  -f deploy/docker/docker-compose.yml -f deploy/docker/compose.web-development.yml up -d --build --no-deps web
```

覆盖只调整 Web；它使用独立镜像标签、容器端口 5173、可写根目录与 Vite 启动命令。返回静态运行时，移除覆盖文件，并选择已构建、验收的静态镜像标签。

更新现有部署时，先确认队列无运行中扫描或身份操作，备份数据库、产物、SDK 凭据、私有环境配置及旧容器配置，并在独立网络验证恢复。为新 Web 使用唯一标签，在私有 `.env` 中只修改 `AXIOM_WEB_RELEASE`，随后使用基础配置 `up -d --no-build --no-deps web`。保留原宿主机端口、网络与后端镜像；本轮模拟部署继续保持 `AXIOM_SCAN_MOCK=1`。不要直接用完整生产配置接管已有迁移数据卷。

静态 Web 版本之间回滚可恢复旧 `AXIOM_WEB_RELEASE` 后重新应用 Web。首次从 Vite 切换时，回滚还须恢复旧 Compose 配置：旧镜像用 `npm run dev`、容器端口 5173 和可写根目录；静态镜像用 Nginx、容器端口 8080 与只读根目录。不能只把旧 Vite 标签放进新 Nginx 配置。回滚只替换 Web，不恢复或删除业务数据；原后端、凭据及旧镜像继续保留。

## 隔离验收

```bash
python3 deploy/ops/static_web_acceptance.py \
  --image axiom-local/web:<candidate> \
  --baseline-image axiom-local/web:<previous-vite-release> \
  --work /absolute/private/path/static-web-acceptance
```

`--work` 必须是新目录；`--baseline-image` 可选。需要 Docker、Python 3、Web 的 `playwright-core` 依赖与 Chromium。CI 安装 Chromium；macOS 默认使用已安装的 Google Chrome，也可用 `AXIOM_TEST_BROWSER_PATH` 指定可执行文件。

测试启动带专用标签的临时容器和内部网络，只挂载公开合同 Fixture 与模拟 HTTP 服务，不挂载数据库、真实身份或模型凭据。浏览器使用 `Fixtures / Mock` 身份，经真实 Nginx 代理访问模拟服务；没有 API 路由替换，没有真实登录接口、SDK 调用或外部发布。

验收覆盖基础 Compose 与显式开发覆盖、SPA 直达和刷新、缓存及安全头、内部 API 与开发路径边界、SSE 首事件、约 9 MiB 上传完整性及流式到达、超大请求拒绝、报告附件名和内容、后端故障与就绪恢复、新 IP 自动解析、键盘访问原始诊断和下载、深浅色及窄屏溢出。可选旧 Vite 镜像回退同样使用隔离后端。结束后只删除本轮标签匹配的资源。

`report.json` 和浏览器截图记录这些合成场景，不代表真实账号、模型调用或真实审计已验收。GitHub Production deployment baseline 工作流运行同一验收，只上传公开摘要与模拟界面截图，不上传私有环境文件、数据库、日志或备份。
