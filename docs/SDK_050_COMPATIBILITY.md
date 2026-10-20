# SDK 0.5.0 隔离兼容候选

本文件记录隔离验收阶段：当时生产基线为提交 `5e50ddc` 的 SDK 0.1.25，候选位于 `codex/sdk-050-compatibility`。该阶段只验收已有产品能力的版本适配，不部署、不调用真实模型、不登录真实 SDK 账户、不发布外部数据。随后用户授权的 Docker 更新已完成，见 [SDK 0.5.0 部署记录](SDK_050_DEPLOYMENT.md)。

## 版本与边界

| 项目 | 历史基线 | 候选 |
| --- | --- | --- |
| Codex Security | 0.1.25 | **0.5.0，精确锁定** |
| Codex / Codex SDK | 0.149.1；另有 0.152.1 平台别名 | 0.163.0；Linux/Windows 别名同步锁定 |
| 内置插件 | 0.1.94 | 0.1.95 |
| Scanner description | 12 | **13，SDK result adapter 2** |
| 控制/输入/结果/事件协议 | HTTP、共享产物、JSONL | 继续原有传输；控制 API 4、Job 1–8、结果 1、事件 2 |
| Linux 候选运行时 | 旧版构建清单保留 | Node 22.23.3 / npm 10.9.8，Python 3.11 |

版本规则集中于 `contracts/data/sdk-adapter-compatibility.json`，生成 Go/TS 元数据。原描述 Schema 1–12 不改写；描述 13 严格绑定 0.5.0 与 adapter 2。SDK 参数/并发能力的旧文件继续记录其 0.1.25 引入基线，不冒充新版本验收证据。新的执行矩阵摘要阻止旧 Server/Agent 与候选直接混用。

| 组合 | 处理 |
| --- | --- |
| 候选 Server + Agent + Scanner 0.5.0 | 本次隔离验收组合 |
| 旧 Server + 候选 Agent，或候选 Server + 旧 Agent | 执行矩阵不匹配，领取前阻止；不得绕过检查 |
| 描述 13 宣称 SDK 0.1.25、未知 SDK 或 adapter 1 | 描述校验拒绝 |
| 历史 0.1.25 结果、事件、证据、报告 | 保留原版本解释与读取，不重标为 0.5.0 |
| 0.1.25 私有父扫描工作台 → 0.5.0 | 读取历史记录，但拒绝导入私有状态，明确新建扫描 |
| 人工混装其他旧 Scanner 描述 | 通用合同读取器仍保留旧版本能力；不属于候选发布包的认证组合 |

## 结果、证据和费用

- 0.5.0 删除 `coverage.completeness`。完整保留 coverage、`deferred`、`resolvedDeferred`、`openQuestions` 和未知字段；执行结论取自 manifest。覆盖缺口不自动变成执行失败，也不补造“完整覆盖”。旧版 `completeness` 保持原意。
- 证据适配保留 `codeEvidence`、`rootCause.code`、`attackPath.dataflow`、嵌套 reachability 与明确的 `evidenceRefs`；支持旧别名。不把无法映射的引用附到整个漏洞，不按数组位置猜测关系。
- 图谱只将与冻结源码及指定行完全匹配的代码标为“已观察”。模型复核文字继续是推断；版本、源码、执行身份或引用损坏时，图谱明确拒绝/降级，完整报告仍可读。
- 0.5.0 定价可以返回短上下文估算基准及范围。本轮估算保存完整 `nativeEstimate`，包括定价日期、范围和未知字段。基准与区间不冒充最终账单；范围不确定或无上界时，结果费用保持未知，使用既有 Web 对账。组件按相同规则处理并停止继续派发，不把未知记为零。
- 原生 Markdown/SARIF、模型、仓库级历史及诊断继续按归档引用和哈希查询，不重建或覆盖历史报告。

## Worker 与用量事件

0.5.0 原生 `onWorkerStatus` 仅报告 dispatch；不再产生 Worker preflight。旧 preflight 事件继续可读。`onWorkerEvent(observed)` 只证明发现了持久化 Worker 会话，初始状态是 unknown；只有结构化 SessionEvent 改变运行状态，迟到的 observed 不回退已知状态。

`onUsage` 经 `SDKUsage` 统一 Schema、生成的 Go/TS 类型、进度扩展及 `scan.usage` 事件持久化。零与缺失分别保存；SDK 标记缓存写入未报告时，保存该标记并显示未知，不采用其归一化零值。Token 不等于价格、完整结算或任务总量。组件 Worker/Token 带组件索引，避免并行组件使用相同 Worker 编号时混淆。组件父任务不会把部分已观察费用显示为完整总额。

## 七项补丁

所有补丁从 **0.5.0 原始 npm 字节**重新制作，不覆盖回旧版工作流。官方包 integrity、完整文件清单、before/after SHA-256、审查记录分别在 `deploy/sdk/manifest.json`、`inventory.json`、`patch-review-050.json`。0.1.25 清单和全部补丁保存在 `deploy/sdk/legacy/0.1.25/`。

| 补丁 | 候选处理 |
| --- | --- |
| `dist/config.js` | 保留 Axiom V1 `multi_agent` 兼容策略；保留其余新版配置/权限 |
| `references/core-scan.md` | 只将委派参数改为 `fork_context: false` |
| `references/threat-model.md` | 同上 |
| `skills/fix-finding/SKILL.md` | 同上，不调用修复能力 |
| `scan-artifacts-and-ledger.md` | 同上，保留新版移除旧排序 helper 的改动 |
| `.mcp.json` | 在新版允许列表上补五个已有 Provider 环境变量；不传 Server/数据库凭据 |
| `custom-validation-prompt.js` | 将兼容性哈希绑定到本次 core guide；源码漂移继续拒绝 |

原生 Codex 0.163.0 的本地 feature listing 已确认 `multi_agent` 仍可启用。此证据只确认配置可解析，**没有验证第三方模型委派、发现率、修复或登录质量**。测试只使用替代执行器；插件 Markdown 是构建数据，不是仓库开发指令。

新版 `resolve_security_md.py`、`config_preflight.py` 已移除。宿主指导文件解析改用随包 Node `resolve-security-md` helper；默认扫描提示也改为当前插件的 `launch_codex_security_mcp --helper resolve-security-md`，不再让模型调用不存在的 Python 脚本。SDK 其他本地工作流仍需要 Python，因此候选 Agent 镜像补装 Python 3。

## 恢复与未接入的新能力

公开 `CodexSecurity` / `ScanOptions` 没有稳定的 `resume()` / `resumeScanId` 合同。上游 CLI 的内部 Deep resume 不作为产品入口。Web 继续区分“恢复完整提交结果入库”和“明确重新执行”；不复制或升级现有业务 SDK 凭据、会话、私有工作台。Web 读取/选择父扫描及创建任务时拒绝跨 SDK 复用，Scanner 仍保留第二道版本校验；旧报告可继续查看。

0.5.0 删除的 `server.startFindingsServer` 已从可用 API 清单移除。新增 `patch`、hosted、records、策略生成、严重度/所有者分析、项目配置、`exportArtifact` 和独立比较等列为待实现。本轮未开放这些新模型操作。`stopAfterConsecutiveErrors`、`maxTimeMinutes`、实验 `cyberAccessProgram` 尚未提供独立 Web 配置；既有超时、预算、扫描范围和推理强度保持原义。不能据此宣称完整集成 0.5.0 的全部新能力。

## 本轮实测（2026-10-20）

| 验收 | 结果与边界 |
| --- | --- |
| Go / TS 合同 | 429 项通过；旧结果、新字段、SDK/adapter 不匹配、外来执行身份、Token 零/未知均覆盖 |
| Scanner | 187 项通过；包含锁定 SDK 的本地预检、组件调度与官方 Mock、源码变更、失败/取消及不确定费用 |
| Web 单元 | 71 项通过；包含两版结果、覆盖记录和定价范围读取 |
| Go Server / Agent | 全模块回归通过；隔离数据库验证历史结果、执行身份、故障恢复及父状态拒绝复用 |
| 浏览器 | 新旧图谱各 28 项、0.5.0 专项 8 项通过；另有输入、组件、身份、恢复等现有浏览器回归。核对窄面板截图、键盘、原始证据、报告预览及移动端 |
| 七项补丁 / 静态 SDK 集成 | 12 项测试通过；补丁漂移检查确认 7 项一致、0 个文件需更新 |
| Linux amd64 原生运行时 | 无网络只读容器通过真实 SDK 官方 Mock、版本化 Job/结果/提交 SHA-256、Codex feature listing 和本地插件安装 |
| 隔离安装升级与回滚 | 20 项通过；用内部固定提交 `02515c1` 的 0.1.25 镜像升级到候选 0.5.0，再从升级前备份恢复原镜像。历史报告字节、费用账本、未知费用、清理意图与模拟凭据字节/权限均保留 |
| 维护与依赖边界 | 维护测试 17 项、依赖策略测试 7 项、发布脚本测试 14 项通过；真实 Go 依赖图无跨模块/数据库越界，本地源码发布检查通过 |
| 现有部署保护 | 七个业务/演示容器的 ID、镜像、启动时间和挂载与操作前一致，现有 Agent 仍安装 0.1.25；不查询业务数据库，不读取现有凭据内容 |

升级演练使用全新 `axiom-accept-*` 项目、独立数据卷、模拟模型和模拟身份；测试 Agent 的网络不具备外网访问能力，完成后只清理该测试栈。它证明合成数据的升级保留与回滚，不证明真实 ChatGPT 凭据格式迁移、真实模型表现或供应商账单已验收。表中记录本地隔离验收结果；源码提交后的远端 CI 另以对应提交的 GitHub Actions 结果为准。

## 重验入口

先在独立工作树安装依赖，使用空 HOME / CODEX_HOME / SDK 状态目录及仓库外 TMPDIR。不要挂载生产卷，不导入现有身份凭据。数据库使用新建的 `axiom_test_input*` 实例；模型、规划、身份 API 必须由测试执行器替代。

```sh
npm --prefix contracts ci --ignore-scripts
npm --prefix contracts run build
npm --prefix code-audit-scanner ci
npm --prefix code-audit-scanner run verify:sdk
npm --prefix code-audit-scanner run test:sdk-patches
npm --prefix code-audit-scanner run verify:sdk-integration
npm --prefix code-audit-scanner run test:sdk-integration
npm --prefix contracts test
npm --prefix code-audit-scanner test
npm --prefix code-audit-web run build
npm --prefix code-audit-web test
```

`TestSDK050EvidenceCompatibilityPipeline` 运行真实 Server/Agent/Scanner 和替代模型边界；`AXIOM_TEST_WEB_EVIDENCE=1` 开启新旧图谱与 0.5.0 专项浏览器验收。新容器中 `--network none` 运行 `deploy/sdk/verify-runtime.mjs` 验证原生 SDK Mock、本地插件安装、Python 与 Codex V1 配置，不使用任何业务卷。

部署验收器按实际安装 SDK 选择各自的固定执行矩阵，不将 0.1.25 运行时改标成候选。旧基线元数据保留在 `deploy/sdk/legacy/0.1.25/execution-baseline.json`；候选和旧版分别在升级前后核对。使用新建的私有 Fixture 配置运行 `deploy/ops/acceptance.py --env <旧版测试配置> --candidate-env <候选测试配置> --work <独立验收目录>`；不得传入业务配置。

本轮测试证据位于忽略目录 `output/sdk050/`，汇总见 `acceptance-report.json`。它们是合成验收，不代表真实模型效果或真实账户迁移已验收。隔离验收结束时生产 Docker 未切换；后续实际部署、备份和回滚前提见 [部署记录](SDK_050_DEPLOYMENT.md)。提交范围是源码、合同、测试及脱敏记录，原始运行产物与私有备份继续保留在本地忽略目录。
