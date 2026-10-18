# Axiom Web

Vue 3 + TypeScript + Vite，复用现有 Reka UI、TanStack Table 和 `design/` 设计规范。业务 API 保持同源 `/api/v1`；生产环境由静态 Web 服务提供页面并代理 API。

登录、项目与源码快照、扫描配置与控制、进度与证据、报告、模型配置、账户、预算和存储操作均在 Web 完成。Server、Agent 与 Scanner 是后台组件，业务用户无需操作命令行。

## 开发与验证

以下命令供开发者使用：

```sh
npm ci
npm run build
npm test
AXIOM_API_TARGET=http://127.0.0.1:8080 npm run dev -- --host 127.0.0.1 --port 5179
```

请将开发代理指向本机隔离 Server。完整逐页验收使用独立 PostgreSQL、真实 Server 和静态生产构建；不启动 Agent、Scanner、SDK 或真实模型。运行方式、完成/缺失/待验证清单及证据见 [前端完整性验收](../docs/FRONTEND_ACCEPTANCE.md)。既有输入、结果、进度等集成测试使用 SDK 替身。

正式 Linux amd64 单机安装见 [Beta 安装说明](../docs/BETA_INSTALLATION.md)。普通构建或前端验收不代表已完成真实 SDK 审计效果验证。

资产/漏洞列表及源码选择支持服务端全量分页、搜索和组合筛选。Web与Server需同步升级；旧数组API继续供历史客户端读取。参数、历史语义和独立浏览器测试见 [查询基线](../docs/CATALOG_PAGINATION.md)。

知识文档管理、固定版本选择和高级 SDK 配置已接入新建扫描及执行详情；具体参数、限制、历史语义与本机浏览器验收见 [扫描输入与知识库基线](../docs/SDK_INPUT_KNOWLEDGE.md)。

执行详情提供 SDK 原生报告、仓库历史 Inspector、结构化诊断和冻结用量估算；历史结果没有新版扩展时仍保留原始产物。验收见 [原生结果基线](../docs/SDK_NATIVE_RESULTS.md)。

2026-10-16：SDK 原生预检、独立复核、完整会话事件及未知费用的 Web 接入见 [SDK 操作基线](../docs/SDK_OPERATIONS.md)。保留 SDK 0.1.25、历史读取与原有传输；验收不执行真实模型、SDK 登录或外部发布。

2026-10-16：已核实 SDK 0.1.25 没有跨进程续扫入口。Web 新增过期中断登记、完整提交结果恢复入库与明确确认后的重新执行，保持原执行、费用和历史证据。迁移 017 仅保存恢复来源；既有合同、SDK 锁与七处补丁不变。详情见 [扫描中断恢复](../docs/SCAN_RECOVERY.md)。验证只使用本地 Mock / Fixture，业务 Docker 未升级。
