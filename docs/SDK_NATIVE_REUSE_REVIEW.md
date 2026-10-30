# SDK 原生能力复用与自研边界检查

2026-10-28；基于当前本地源码、锁定的 `@openai/codex-security 0.5.0`、公开声明及随包实现。检查涵盖扫描、复核、组件、授权、报告、用量、恢复、证据、传输和平台存储。此轮只做隔离验收，不调用真实模型、不发起登录、不部署 VPS，也不改历史数据、单价和费用。

Axiom 是 Web 管理平台，SDK 是审计执行引擎。已有原生能力直接调用；平台只管理用户、输入、执行、证据与业务记录。名称相同但语义或信任边界不同的功能不能直接互换。

同日后续收敛已将公共 SDK 文件恢复为官方原始字节，旧七处补丁只用于独立兼容运行包；本文后续表格记录此前检查及保留理由。当前连接选择和状态绑定以 [SDK 运行环境隔离](SDK_RUNTIME_ISOLATION.md) 为准。

## 本轮已收敛

| 原实现 | 当前实现 | 验证依据 |
| --- | --- | --- |
| 失败回收时自行解析完整 SDK 三份 JSON，未验证原生封存 | 已完成或带封存标记的结果交给公开 `loadContract()`；SDK 检查 Schema、Scan ID、引用与封存哈希。原生拒绝后不回退到宽松解析 | 官方 SDK Mock 生成封存结果，原生读取通过；篡改 findings 后拒绝回收 |
| `AuditCheckpoints` 再保存一份截断会话日志、复制 JSON、记录空 Worker 并覆盖完成状态 | 删除第二套写入器；使用 SDK 原生检查点、完整回调日志 `SDKSessionJournal` 和只读中间结果投影。SDK 决定扫描和覆盖结论 | 注入空 Worker 完成事件，事件仍保存，SDK 的完成结果不被平台擅自降级；原生覆盖缺口照常保留 |
| 默认提示自行规定子任务补交次数、接管规则、额外检查点目录 | 删除重复编排规则，遵循 SDK 工作流；保留中文、用户关注范围、只读边界和精确引用要求 | 提示与原生执行隔离测试 |
| 通过 SDK 安装文件路径猜测插件目录 | 使用公开 `bundledPluginRoot()` 定位，再调用 SDK 自带指导文件解析 helper | 引导测试与官方 SDK Mock |
| Deep 使用已废弃的小时别名 `maxTimeHours` | 现有分钟配置直接传公开 `maxTimeMinutes` | SDK 原生纯校验器验证两种设置得到完全相同的 Deep 限制；外层硬超时及整理宽限不变 |

完整实现见 `code-audit-scanner/src/artifact/load-raw-result.ts`、`src/engine/codex-security-engine.ts`、`src/contract/scan-prompt.ts`、`src/contract/deep-policy.ts`、`src/runtime/audit-bootstrap.ts`。

## 已经直接使用 SDK，无需重新实现

| 能力 | SDK 原生入口 | Axiom 仅保留的接入工作 |
| --- | --- | --- |
| Standard / Deep 审计 | `CodexSecurity.run()` | 冻结参数、派发、取消信号、进度落盘与归档 |
| 子任务、Worker、发现轮次、扫描内重试 | `run()` 的原生工作流与回调 | 显示真实状态；不再添加另一套子任务完成判定，不额外重放模型请求 |
| 独立复核 | `CodexSecurity.validate()` | 冻结漏洞和源码、创建独立操作、检查正文、归档结论；人工漏洞状态独立 |
| 可利用性复核模式 | `validationPrompt` 原生定制验证阶段 | 提供用户业务背景与中文报告要求，保留原生 Schema；不自写第二个验证执行器 |
| 预检 | `CodexSecurity.preflight()` | 提供页面与保存结果；预检不被当成模型访问成功 |
| 组件建议、规范化、组件调度 | `planComponents()`、`normalizeComponentPlan()`、`runComponentScans()` | 计划版本、人工确认、范围覆盖与预算/组件身份绑定 |
| Git 提交差异 | `DiffTarget.refs()` | 上传仓库安全处理、冻结 base/head、预览及 HEAD 证据绑定 |
| 知识与附加指令 | `knowledgeBasePaths`、原生 prompt 选项 | Web 文档版本管理和执行私有物化；不自写知识检索引擎 |
| ChatGPT / API Key 登录、账户查询、退出 | `loginChatGPTDeviceCode()`、`loginApiKey()`、`account()`、`logout()` | 页面、角色权限、账户选择、操作记录和凭据隔离 |
| 原生报告与结果 | `ScanResult`、`toJSON()`、`loadContract()`；SDK 生成 Markdown/SARIF | 归档原始字节、完整性检查、浏览器预览与下载 |
| Token / Worker / 生命周期数据 | `onUsage`、`onSessionEvent`、`onWorkerEvent` 等公开回调 | 脱敏、跨进程合同、按执行保存、分页查询；不伪造 SDK 未提供的字段 |

代码入口：`code-audit-scanner/src/engine/`、`src/runtime/sdk-identity.ts`、`src/contract/sdk-scan-input.ts`。能力声明及版本核对见 `deploy/sdk/integration.json` 和 `deploy/sdk/integration-check.mjs`。

## 确实需要保留的自研逻辑

| 自研逻辑 | 为什么 SDK 原生不能直接替代 | 主要代码 |
| --- | --- | --- |
| Web、项目、平台用户权限、PostgreSQL、操作记录、备份与部署 | SDK 不是 Axiom 的多用户 Web 管理系统；原生内部工作台存储也不等于平台业务数据库 | `code-audit-web/`、`code-audit-server/`、`deploy/` |
| ZIP 上传、不可变源码快照和内容哈希 | SDK 会检查自己的目标；平台还须证明上传、Agent 读取、报告和后续复核绑定的是同一份源码 | `internal/snapshot/`、`shared/sourceidentity/`、Agent 的 `internal/scanner/` |
| 任务队列、租约、Agent 绑定、取消、历史 attempt、结果恢复入库 | SDK 运行一次审计，不承担跨服务派发和数据库事务。结果已完成但网络上报中断，与模型断点续扫是不同事项 | Server `internal/store/execution.go`、Agent `internal/worker/`、`contracts/` |
| 账户目录隔离、任务身份/凭据代次冻结、刷新回存保护 | SDK 负责一个本地登录上下文；平台须防止多用户、多任务或撤销后的旧身份串用 | `src/runtime/identity-session.ts`、`sdk-identity.ts`、Server/Agent 身份代码 |
| 平台单价、历史快照、费用与预算账本 | 按用户要求保留通用采购单价。SDK 内置价格估算与实际采购价、历史版本和跨任务账本不等价，不重新启用其费用表 | `docs/MODEL_PRICING.md`、`src/runtime/token-budget.ts`、Store 计价代码 |
| 独立复核的 Responses 用量采集 | SDK 0.5.0 的 `ValidationOptions` 没有 `onUsage`；不能把扫描回调能力当成复核回调 | `src/runtime/execution-token-usage.ts`、`src/proxy/reliable-upstream.ts` |
| 中间候选的只读、脱敏、不可变版本投影 | SDK 保存草稿和检查点；Axiom 需要在执行中/失败后通过 Web 查看，明确未验证状态，不把草稿当成已封存报告 | `src/runtime/interim-findings.ts`、Server 中间发现/复核代码 |
| 证据关系图和源码精确一致性 | SDK 有原生证据与语义复核，但当前封存校验不将 `codeEvidence.code` 与指定源码行逐字比较。原生 `sourceExcerpt` 限于已封存 Git 版本，目录快照返回空；且它不是逐条证据比较器 | `src/converter/finding-evidence.ts`、Server `internal/evidence/graph.go`、`runtime/source-evidence.mjs` |
| 提交前摘录检查与服务端再次核对 | 前者辅助模型提交完整原行；后者独立核对归档与冻结源码。不同信任边界不能用“模型说检查通过”互相替代；摘录工具不会改写证据，源码匹配也不代表漏洞动态复现 | `runtime/source-evidence.mjs`、`internal/evidence/graph.go` |
| 报告正文完整性检查、可信原始产物访问、错误脱敏 | SDK `validate()` 的格式契约只要求非空 report 字符串，不保证完整评估正文。平台下载还须防止跨执行文件访问；不重写报告结论 | `src/engine/sdk-operation.ts`、`src/artifact/result-publisher.ts`、Server 产物接口 |
| 业务报告 HTML / PDF、人工漏洞状态、精确历史对比 | SDK `exportArtifact()` 提供 JSON/CSV/SARIF 和威胁模型 Markdown，没有同等的 Axiom HTML/PDF 或人工工作流。精确身份对比与需额外模型调用的语义匹配不同 | Web 报告/漏洞页面、Store 历史记录 |
| 硬超时及进程组终止 | 原生 `maxTimeMinutes` 在 Standard 是建议审查时长，在 Deep 是发现时限；整理阶段可更久，不能替代平台最后的进程/租约清理 | `src/contract/deep-policy.ts`、Agent 进程控制 |

未封存的失败草稿继续走有限的观察读取；不尝试把它强行加载成已完成 SDK 合同。旧 `checkpoints/evidence.jsonl` 与 `coverage.axiomCheckpoints` 只保留历史读取，既有文件不删除、不回填。

## 有条件保留的兼容代码，不是永久业务能力

| 代码 | 当前保留原因 | 可删除/替换的条件 |
| --- | --- | --- |
| 第三方 Responses 传输、namespace 工具兼容、模型目录 | 实际已发现的第三方接口差异；原生 OpenAI 路径不走这层代理。协议仍为 Responses，默认严格 Schema，不引入 Chat 适配 | 对应服务原生接受相同工具/输入格式、用量与完整完成事件后，逐项对照移除 |
| 共享 Retry-After 冷却与用量回执 | 跨 Scanner 进程共享供应商限流信息、服务平台单价预算；模型请求重试仍只有 SDK 一个所有者 | SDK 提供等价的跨进程协调与采购价预算入口；不能同时增加第二层重试 |
| 只读工作台进度观察 | 公开回调没有阶段收据计数、单位和完整范围总数；该适配只补充观察值，并标明来源，不生成百分比或覆盖结论 | SDK 公开提供等价字段后，改为只用回调 |
| 父扫描状态复制、Deep 恢复桥、陈旧运行标记处理 | 平台按执行隔离本地状态；SDK 0.5.0 没有公开 `resume()` 或状态导入导出 API。Deep 桥调用锁定版本原生恢复条件和执行路径，不自写恢复协调器 | SDK 提供公开、可迁移的等价状态/恢复 API 后替换；当前须保留源码/身份/节点/版本与排他锁核验 |
| 历史 7 处 SDK 兼容补丁（已隔离） | V1 委派配置与 `fork_context` 文档配套、第三方凭据转发及说明文件哈希只保留于 legacy 运行包；公共包已恢复官方 V2 原始字节 | 对应第三方模型完成原生 V2 等价验收后再移除其兼容选择。`fix-finding` 文档补丁对应的 Web 功能尚未启用，不作为平台必需业务能力；当前规则见运行环境隔离文档 |

这批补丁没有修改 `finalize_scan_contract.py` 的证据校验，也没有把原生 Schema 改成 Chat。它们本轮保持原字节与指纹，避免在没有模型对照的情况下破坏已绑定的恢复状态；保留原因和后续退出条件不等于宣称所有补丁永久必要。见 `deploy/sdk/patches/`、`manifest.json`、`recovery-baseline.json`。

原生 `matchScanFindings`/语义去重、`patch`、对外发布、策略生成、托管扫描等尚未接入的独立功能，不在此轮自动启用，也不由自研版本顶替。它们可能调用模型、修改源码或写外部系统，需要各自的输入、权限、费用和结果工作流；能力表继续如实标为未接入。

## 验收记录

机器可读摘要：`docs/acceptance/sdk-native-reuse-20261028.json`。包含 311 项完整 Scanner 测试、8 项 Web 历史结果测试、官方 SDK Mock、原生封存校验与篡改拒绝、历史中间证据读取、原生 Deep 设置等价性、SDK 公开能力/补丁完整性核对及构建结果。

这轮收敛后的代码没有重新调用真实模型；先前 GPT / DeepSeek 的真实验收记录保留各自当时的版本范围，不挪用为此次修改后的真实模型验收。
