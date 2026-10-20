# SDK 修改盘点与重建基线

当前隔离候选已精确锁定 SDK 0.5.0 / Codex 0.163.0；七项补丁重新基于新版发布字节制作。候选 Linux 工具链为 Node 22.23.3 / npm 10.9.8，补齐 Python 运行依赖；详见 [0.5.0 兼容验收](SDK_050_COMPATIBILITY.md)。下文保留原 0.1.25 部署与历史验收记录，旧构建清单位于 `deploy/sdk/legacy/0.1.25/`。现有业务 Docker 本轮没有切换。

本基线从原开发 VPS 的私有安装目录 的实际安装文件提取，逐文件对比同版本官方 npm 发布包，并校验发布包的 SHA-512 integrity。范围是 Linux x64 扫描运行环境；没有将未经验证的 Windows/macOS 行为标为已验收。

## 已确认的修改

| 位置 | 修改 | 原先保存位置 |
| --- | --- | --- |
| SDK `dist/config.js` | `features.multi_agent_v2` 改为 `multi_agent: true`，保留当前第三方 Responses 模型的 V1 兼容策略 | node_modules |
| 插件 `references/core-scan.md` | baseline/investigator 使用 `fork_context: false` | 用户插件缓存 |
| 插件 `references/threat-model.md` | 独立架构评审使用 V1 参数 | 用户插件缓存 |
| 插件 `skills/fix-finding/SKILL.md` | 调查和补丁评审使用 V1 参数 | 用户插件缓存 |
| 插件 `skills/security-scan/references/scan-artifacts-and-ledger.md` | 工作列表的调度说明改为 V1 参数 | 用户插件缓存 |

应用层 `src/provider/model-provider.ts` 的 `multi_agent_version: "v1"` 是配套配置，不属于 npm SDK 文件修改。源码还包含第三方 Provider 接入、Chat/Responses 转换、日志处理等业务适配；这些与 SDK 补丁一起纳入源码基线。

四个插件文档原本只修改了缓存，自带 `_bundled_plugin` 仍为原版。新扫描采用独立 SDK 状态目录时会从自带插件建立新缓存，因此单纯备份旧缓存不足以还原行为。安装流程现在把相同修改应用到自带插件，供新运行目录复制。

`deploy/patch-codex-runtime.mjs` 是已有的 Windows spawn 补丁脚本，但盘点时 VPS 的 `dist/runtime.js` 与原版一致。Linux 重建不自动应用该 Windows 补丁；Windows 发布需要单独验收。

## 固定版本与实际解析

- 验证环境：Ubuntu 24.04，Python 3.12.3，Git 2.43.0。
- Node.js：22.23.2；npm：10.9.8。
- Chat 接口适配使用的 `undici`：8.10.2。干净安装发现原项目遗漏了这一直接依赖；现已按开发机实际版本补入 package.json 与锁文件，并与官方包逐文件比对。
- `@openai/codex-security`：0.1.25；内置插件：0.1.94。
- `@openai/codex-sdk` 与 CLI 包：0.149.1。
- 根目录额外安装的 Linux Codex：0.152.1-linux-x64，与官方发布包一致。
- **SDK 实际默认解析到嵌套的 0.149.1-linux-x64**，不是根目录额外安装的 0.152.1。嵌套二进制也与官方发布包一致。本次保留此行为，不混入运行时升级。
- 深度扫描不传 worker 覆盖，沿用插件默认 4；V1 子代理协议、深度扫描 discovery worker 数、Axiom 多任务并发上限是不同参数。

`deploy/sdk/manifest.json` 保存包版本、官方下载地址、integrity、文件哈希和补丁前后哈希。`inventory.json` 保存原始差异清单；`patches/` 供代码评审；`overrides/` 是经过哈希验证的替换文件。

## 安装与验证

必须保留仓库的 `deploy/sdk` 与 `code-audit-scanner` 相对位置。不要只复制 scanner 的 package.json。

```sh
cd code-audit-scanner
npm ci --no-audit --no-fund
npm run verify:sdk
npm run test:sdk-patches
npm run typecheck
npm test
npm run build
```

`postinstall` 自动应用补丁。它先校验所有目标文件，再写入；只接受原版或预期已修改版本。未知修改、缺少文件、多出未管理文件、补丁校验失败均会中止。`verify:sdk` 为只读检查。重复应用不会改变已正确应用的文件。

任何新 SDK 修改都应进入补丁清单并重新验证，不再把直接编辑 node_modules 当作交付方式。

## 干净环境重建

```sh
node deploy/sdk/rebuild.mjs --output /absolute/path/to/new-directory
```

输出目录必须不存在。脚本复制源码、锁文件、补丁和测试样例，使用空 HOME、空 npm 缓存与隔离临时目录；不继承 API Key、数据库连接和 Agent 凭据，也不复用现有 node_modules、SDK 工作台或插件缓存。

依次执行锁文件安装、全文件校验、补丁测试、TypeScript 检查、扫描器测试、构建和 SDK 模拟扫描。运行验证还检查 V1 配置、四份插件文档、默认深度 worker 数、实际 Codex 版本，以及新缓存中真正安装的插件内容。成功后写入 `REBUILD_OK.json`。重复验证可显式传 `--cache /path/to/npm-cache` 复用官方包下载缓存，仍使用全新 HOME 和 node_modules，npm integrity 与完整文件哈希继续校验；验收记录会说明是否使用缓存。

此验证不调用付费模型，不证明第三方模型在真实服务上的完整审计质量，也不代替 Windows/macOS 验收。当前仍需要网络获取锁定的 npm 包；离线依赖仓库和客户安装包属于后续发布阶段。

## 备份与升级

本次原始依赖与旧插件缓存已备份到 原开发环境的私有备份目录（不随源码发布），包含修改前原样安装树、缓存、官方包与差异报告。不要把密钥、auth.json、业务数据库或运行日志写入源码仓库。

升级 SDK 时应另建分支：比较上游变化、重新移植仍有必要的补丁、更新 manifest 与锁文件、完成独立重建后再切换。不要静默跳过补丁失败，也不要用更高版本覆盖当前可用基线。

## 2026-09-10：深度扫描凭据转发修复

真实并发演练发现，第 27 个隔离任务的主协调器可以使用 DeepSeek，而 4 个内部 worker 连续报 `Missing environment variable: DEEPSEEK_API_KEY`。原因是 bundled plugin 的 `.mcp.json` 只转发上游已有供应商凭据名，没有转发 Axiom 使用的第三方凭据名。

新增第 6 处固定补丁：`_bundled_plugin/.mcp.json` 的 `env_vars` 加入 `DEEPSEEK_API_KEY`、`DASHSCOPE_API_KEY`、`MOONSHOT_API_KEY`、`ZHIPU_API_KEY`、`CUSTOM_API_KEY`。只写变量名，不写任何密钥值。已有 V1 配置与 4 处文档补丁保留，内部 worker 数仍为默认 4。

`verify-runtime.mjs` 同时检查 bundled 配置和一次全新插件安装后的配置。完整隔离重建路径为 `/opt/Axiom-rebuild-task-management`，扫描器回归测试增加 worker 失败日志检查。SDK 原始审计报告仍是历史基线，新增变更由 manifest、override 与第 6 个 diff 单独记录。

应用层同时修复 `task_complete.error` 被会话过滤器丢弃的问题：错误以 error 级别进入活动流，保留线程/worker 标识并脱敏，即使关闭推理日志也会保留错误。此次问题不会再只表现为反复“会话开始”。

## 公开能力接入基线（2026-10-16）

当前完整集成范围固定到 `@openai/codex-security 0.1.25`。升级另做兼容验收。
`deploy/sdk/integration.json` 按 25 个能力组记录公开 API、现有实现证据、缺口与实施顺序；这些组不是完成百分比。
`integration-check.mjs` 只解析 16 份声明文件并核对哈希，不导入 SDK 运行时、不登录、不调用模型、不发布数据。
检查已接入 `input-chain.yml`；本地使用 Scanner 的 `verify:sdk-integration` 和 `test:sdk-integration`。

### 已落地的增量

- 独立 `SDKLifecycle` Schema 生成 Go/TS 类型，经现有进度 `extensions.sdkLifecycle` 传递；历史 Schema 和传输方式保持不变。
- Scanner 记录认证来源（始终 `verified:false`）、Trusted Access、产物就绪/归档、扫描开始和接近预算上限。来源、权限和执行结论分别表达。
- 接近上限回调始终返回 `undefined`，不提高已批准的单次/任务总预算；未知估算保留 `null`。
- Web 在执行详情展示生命周期及原始快照；旧任务没有扩展时继续正常读取，未知扩展版本单独提示。
- 不记录登录凭据、账户详情或回调中的宿主机目录。API Key 和设备码身份现已接入，状态、隔离与凭据边界见 [执行身份基线](SDK_EXECUTION_IDENTITIES.md)。

### 剩余实施顺序与完成条件

| 顺序 | 能力 | 完成条件 |
| --- | --- | --- |
| 1 | 扫描输入、结果与观察 | 已接入；原生结果见 [结果基线](SDK_NATIVE_RESULTS.md)，全类型 SessionEvent、未提交日志与故障语义见 [操作基线](SDK_OPERATIONS.md)。 |
| 2 | 原生预检、独立复核 | 已接入，见 [操作基线](SDK_OPERATIONS.md)；共用准入/租约/取消，复核无公开费用回调，未知费用保留且不自动重放。合成验收通过后仍不代表真实复核质量已验收。 |
| 3 | Git 差异与组件审计 | 已提交 base/head 的冻结、Standard 目标和 HEAD 证据已接入，见 [Git 差异基线](SDK_GIT_DIFF.md)；[组件规划与计划管理](SDK_COMPONENT_PLANS.md) 已接入，包括不可变源码绑定、编辑修订、覆盖检查与人工冻结。[组件审计执行](SDK_COMPONENT_SCANS.md) 已接入，含父任务、组件执行、整数预算预留、部分失败和选择性重新执行。工作区差异仍待接入；规划/匹配费用未知时不能宣称有完整总额控制。 |
| 4 | ChatGPT/API Key 身份 | API Key、设备码登录、account/logout、冻结任务/Agent/凭据版本及刷新回存已接入，见 [执行身份基线](SDK_EXECUTION_IDENTITIES.md)；回调式 loginChatGPT 仍待设计。验收仅模拟登录。 |
| 5 | 外部集成与语义去重 | Linear/自定义发布预览及明确提交、幂等和 indeterminate 对账；GitHub 导入保留原始告警与来源；Findings API 与 Go/PostgreSQL 适配；语义匹配证据与现有指纹去重分开。 |

所有能力均纳入纯 Web 产品范围，后台继续 Go Server + Go Agent + Node Scanner。
Server 负责身份/权限、配置、操作记录与数据库；Agent 负责领取、租约、执行边界和产物；Scanner 是唯一 SDK 适配边界。
新增操作复用 HTTP、共享产物及 JSONL；必须共用现有并发和容量准入，不能另起无预算、无租约的旁路。
验收使用合成 SDK、临时数据库和浏览器；真实模型、真实登录、外部发布仍未验收。

0.1.25 的公开类没有 `fix()` 或 `resume()`。插件能力和内部字段不作为稳定 SDK 方法；重试不能标成续扫。
完整能力集成尚未完成，能力清单的 `planned`/`partial`/`adapter_required` 项均是剩余工作。

### 本次合成验收记录

2026-10-15：6 项 SDK 基线检查、169 项 Go/TS 合同检查、121 项 Scanner 测试、32 项 Web 单元测试及 Agent 全回归通过。Go Server 的输入/结果/故障流水线原有 29 项子场景通过；生命周期持久化及浏览器进度场景在修正定位器和统一测试预算后重跑通过，包含断线恢复、日志下载与 Web 取消。桌面/窄屏截图已检查，记录见 `output/playwright/sdk-lifecycle/verification.json`（本地验收产物，不提交）。42 份历史 Schema、兼容矩阵及迁移文件哈希保持不变。远端 CI 本次尚未执行；真实审计、账号登录、外部发布均为零次。


### 扫描输入与知识库增量（2026-10-15）

当前批次完成 subagents、三类指令、原生元数据与 Web 知识文档管理。高级任务使用输入版本 2、ScanInputAssignment v2、Job v4 与描述 v5；现有 HTTP、共享产物和 JSONL 保持不变。迁移 014 追加知识版本、冻结输入及私有父扫描状态，不改写 001–013 或原 SQLite schema。

父扫描是同一快照上新建的独立扫描，有自己的费用预算。SDK 0.1.25 需要原生 workbench 中的父记录，因此 Scanner 在 SDK 关闭后用 SQLite 一致性复制导出最多 8 MiB 的工作台状态，绑定父执行和结果哈希；Server 保存不透明副本，Agent 不新增数据库依赖。只复制 workbench.sqlite3，凭据与 codex-home 保持隔离；不使用内部恢复字段，不承诺续扫。

SDK 0.1.25 的官方 mock 已通过父扫描、知识文档与工作流的实际公开调用验证；其余端到端流程在 SDK 实例边界使用 Fixture。两者均不调用模型、不登录真实账户、不发布外部数据。验收入口与限制见 [扫描输入与知识库基线](SDK_INPUT_KNOWLEDGE.md)。能力清单只提升本批已有证据的四组，其余 planned/partial/adapter_required 继续表示未完成。


### 原生结果、导出与诊断增量（2026-10-15）

已接入 SDK 原生 Markdown/SARIF 的状态、受控预览/下载、仓库历史及同源码的 Web 执行关联；SDKCostEstimate 按返回轮次用量冻结，不覆盖费用账本。SDKDiagnostic/SDKDiagnostics 与独立 JSONL 保留结构化警告和观察器错误，既有事件合同保持原值。迁移 015 仅添加派生索引，旧结果和报告读取保留。边界、容量及合成验收见 [本批基线](SDK_NATIVE_RESULTS.md)。

2026-10-16：已核实 SDK 0.1.25 没有跨进程续扫入口。Web 新增过期中断登记、完整提交结果恢复入库与明确确认后的重新执行，保持原执行、费用和历史证据。迁移 017 仅保存恢复来源；既有合同、SDK 锁与七处补丁不变。详情见 [扫描中断恢复](SCAN_RECOVERY.md)。验证只使用本地 Mock / Fixture，业务 Docker 未升级。
