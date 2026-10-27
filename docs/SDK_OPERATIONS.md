# SDK 原生预检、独立复核与完整会话事件

当前 SDK 为 0.5.0，DeepSeek 小项目已有扫描、原生 Schema 独立复核、归档与图谱的有限真实证据，见 [当前状态](CURRENT_STATUS.md)。迁移 028 的平台单价估算与 SDK 原始费用分别保留，缺失仍未知，详见 [模型计价](MODEL_PRICING.md)。下文保留早期接入时的版本及验收记录。

2026-10-16。本批固定 SDK 0.1.25 及已有 7 份受管理补丁，保留 Go Server + Go Agent + Node Scanner、HTTP、共享产物与 JSONL。用户操作均通过 Web。验收只使用本地 SDK 预检、官方 Mock 与合成 SDK，不执行真实审计、SDK 账户登录或外部发布。

## Web 行为

- 新建扫描页面可选择“仅执行 SDK 预检”，冻结本次源码快照、模型、范围、深度参数与知识文档，只调用 `preflight()`。已有扫描也可对其冻结配置新建预检。
- 预检展示规范目标、模型、认证来源和输出计划；宿主机执行根目录替换为 `$SOURCE` / `$OUTPUT`。`verified:false` 明确表示没有验证账户或模型访问权限。预检不调用模型，费用为已知零。
- 已结束扫描的漏洞证据 Inspector 可发起独立 `validate()`，先确认未知费用。操作冻结来源执行、完整漏洞 JSON（包括证据中合法的 password/token 等字段）、源码内容哈希及模型配置，保留自己的任务 ID / attempt / 租约、取消与报告。
- `reportable`、`suppressed`、`not_applicable`、`deferred` 分别保留并显示原生值；不自动修改人工漏洞状态，不导入新的漏洞或覆盖来源证据。完整脱敏 SDK 返回值独立归档，报告原文以文本展示。
- SDK 0.1.25 的 ValidationOptions 只提供 repositoryPath、finding、auth、outputDir、signal。独立复核使用完整源码快照，不假装支持扫描路径子集、扫描提示词、知识文档、扫描预算或 SessionEvent 回调。
- 来源详情按每页 50 项查看关联操作。操作页可返回具体来源执行，并查看冻结的原始漏洞输入、原生结论、完整 JSON 与产物。
- SDK 操作在任务列表明确标识，不计入审计扫描数量/成功率/项目扫描数，不成为历史扫描对比基线，不提供会误导为“零漏洞扫描”的 HTML/PDF 扫描报告。原始操作报告和结果可下载。

## 统一合同与派发

新增独立 Schema：SDKOperation v1、SDKOperationResult v1、SDKSessionEvent v1、SDKSessionSummary v1、ScannerJob v5、ScannerDescription v6。Go/TS 类型均由统一 Schema 生成。

`sdk-operation-compatibility.json` 定义 input=3、result=1、observation=1、Job=5、Description=6；执行矩阵新增 sdk_operation profile。基础 Assignment v1、ScanInputAssignment v2、进度/事件 v2、结果/完成 v1 不变。操作 sidecar 和知识输入 sidecar 必须绑定同一 execution。缺少标记、跨 attempt、未知版本、旧 Scanner 或缺失能力在 SDK 前拒绝，不能回退成普通扫描。

Server 原子保存冻结操作与来源，负责权限、租约、结果校验和 PostgreSQL 迁移 016；Agent 负责源码哈希、知识物化、私有环境及进程组；Scanner 是唯一 SDK 实现边界。操作与扫描使用同一按需执行链路，平台不设置全局/模型/项目并发配额或固定执行槽位；存储、身份、租约、费用和取消检查保持原语义。

现有 001–015 迁移与历史 Schema 保留。迁移 016 新建 sdk_operations，扩展输入版本检查允许 3。来源记录受到引用保护，运行/未知费用证据和历史报告默认不自动清理。

## 费用语义

独立复核没有 SDK 公开的 maxCostUsd、费用回调或可靠用量，不设置伪预算、推算费用或填零。其独立账本保持未知，来源扫描的单次/总预算和累计费用不变。Web 可按真实账单依据人工核对，保留原始未知值及操作记录。

复核不自动重放，租约丢失后结束该 attempt 并保留未知费用。通用 retry 不能绕过确认；再次执行需从 Web 新建操作。操作不接受扫描总预算设置，增加预算不能成为未知费用的绕行方式。原生预检使用本地已知零，不产生模型费用。

## 完整会话事件

扫描的每一类 `onSessionEvent` 回调均写入 `logs/sdk-session-events.jsonl`，包含 execution、连续序号、时间、thread/worker 与脱敏后的完整回调结构。活动流仍按业务需要显示阶段和推理，但不再借活动过滤器删去原生会话证据。

使用同步的有界写入避免无限 Promise 队列；每条最多 8 MiB、每次日志最多 64 MiB。容量、非法数据、目录或写入故障均记录 incomplete、received/persisted/omitted 数量和原因，不能默默宣称完整，也不将观察器问题冒充扫描失败。

正式归档通过 `extensions.sdkSession` 提供独立版本的完整性快照和精确 ArtifactRef。Web 按最多 50 条、8 MiB 分页；返回前核验完整日志的哈希、大小、执行身份、序号与记录数。大小/哈希/身份异常返回领域错误；历史或未知扩展仍可通过核心结果/原始产物读取。

运行中或进程被终止、没有提交 SDK 结果时，只能读取该 attempt 固定路径中的已保存日志快照；标记 uncommitted，明确总数与完整性未知。完整行逐条校验，未完成尾部保留在 partial JSONL 下载中。此入口不会把中断日志升级为正式归档或推断完成百分比。

独立 `validate()` 的公开参数没有 SessionEvent 观察器；操作页明确说明没有此回调，保留真实执行阶段和返回报告，不伪造原生事件。

## 验收与交付边界

- 跨语言合同覆盖预检/复核、四类 disposition、未知与已知零、跨执行身份、缺失标记、错误完整性计数及未来事件类型；历史样例继续回归。
- Scanner 覆盖全类型事件持久化、凭据脱敏、容量/创建故障、公开本地 preflight 和官方 Mock。Mock validate 不会落入真实 public validate 调用。
- 实际 Server → Agent → Scanner 进程使用合成 SDK，验证冻结参数、凭据隔离、费用、历史对比隔离、原生响应超限保留、幂等完成、原生事件分页及哈希篡改拒绝。
- PostgreSQL 故障测试覆盖公共容量/存储准入、租约失效不重放、未知费用/来源证据保护、禁止伪费用和不受支持预算。
- Chromium 覆盖 Web 配置预检、Inspector 复核确认与失败保留、四类结论、惰性原文、分页与下载、取消后日志读取、权限、桌面与窄屏。截图为明确的 Fixtures / Mock。功能回归另验证独立复核取消及人工对账不会改写 SDK 原始未知费用。

本地测试记录与截图保存在 `output/playwright/sdk-operations/`。业务 Docker 服务未升级，未提交或推送 Git，远端 CI 未运行；工作流已接入此验收。SDK 真实复核质量、供应商实际账单及真实登录/发布仍待用户另行授权验收。

升级顺序为 Server 与迁移 → 配套 Agent/Scanner → Web。矩阵摘要不同会阻止派发；不要仅替换旧二进制并把 input=3 当普通扫描。需要回退旧版本时应按既有备份恢复流程恢复相匹配的应用与数据库，并保留新操作数据的独立备份。

本地验收结果：226 项跨语言测试（221 个样例、5 个回归）、768 个版本/能力/预算组合、136 项 Scanner、42 项 Web 单测、13 组新增浏览器检查通过。Server 全包（含 PostgreSQL/SQLite、故障与既有浏览器回归）及 Agent 全包通过；54 份历史 Schema/迁移/SDK 锁定文件逐字节未改变。记录见 `output/playwright/sdk-operations/verification.json`。
