# Axiom
<!-- impeccable:product-schema 1 -->
## Platform
web — Vue 3 / TypeScript frontend in `code-audit-web`.
## Product Purpose
AI 源代码安全审计工作空间，支持从项目、扫描任务到漏洞与代码证据的研判流程。商业级体验是目标，不代表已完成商业就绪认证。
## Users
应用安全工程师及管理代码审计配置的团队成员；具体采购角色仍待确认。
## Capabilities and Constraints
当前前端已连接现有 `/api/v1` API，保留登录、项目、扫描、漏洞、代码快照、模型配置和系统设置能力。工作台与全局搜索使用真实对象。模型启用为新扫描选择唯一默认配置；编辑时留空 API Key 保留服务端已有凭据。模型测试、保存、删除等是真实 API 操作，不再是演示交互。
## Brand Commitments
用户明确认可 C 主方案与字体（“这个可以，字体也不错”）。保留几何蓝色 A、AXIOM / Security Console 字标。默认浅色，深色参考深蓝黑截图，主题选择本地保存。以 Vercel、Claude、OpenAI 式克制质感为方向，强调字体、留白、细边框和少量短促状态动画，不使用持续装饰动画。A/B 和 `prototype/` 保留为历史设计比较，不作为当前实现依据。
## Evidence on Hand
实际实现位于 `code-audit-web/src`。本地 Vite 5179 代理原开发环境的私有服务器，已完成真实登录及工作台、模型配置、漏洞证据的只读检查。`tests/contracts.test.mjs` 的三个客户端契约/编译 CSS 根节点反色回归测试通过；最终生产构建通过。本地隔离接口已验证重试后自动经历排队、扫描和完成，以及原始 JSON 的 Enter 键展开。本次验证未进行生产写操作或部署；不据此声称全部变更流程、安全性、无障碍或商业就绪已获认证。


Deployment update (2026-09-09): user clarified that VPS is the development environment. Frontend synchronized to 原开发环境的私有前端目录, prior version backed up, server tests and production build passed. Remote login, real data and dark mode verified. Backend business-data mutations remain untested.

## 路径守卫移除与并发规划（2026-09-09）

按用户明确要求，移除Axiom被动路径守卫及其结果字段、进度指标、界面提示和专属测试；历史产物保留，SDK自身执行机制不变。数据库与任务并发方案见 `docs/DATABASE_CONCURRENCY_PLAN.md`。当前仍使用SQLite和串行Agent，尚未迁移PostgreSQL或开启任务并发。
