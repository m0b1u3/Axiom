# SDK 推理强度与组件审计输入

SDK 固定为 `@openai/codex-security@0.1.25`。本功能沿用 Go Server → Go Agent → Node Scanner，以及现有 HTTP、共享产物和 JSONL 传输；用户在 Web 配置和查看输入。

## 网页配置与 SDK 映射

| Web 配置 | 冻结字段 | SDK 入口 |
| --- | --- | --- |
| 模型与授权 → 模型配置 → 推理强度 | `sdkParameters.reasoningEffort` | `CodexSecurity` 构造配置的 `codexOverrides.model_reasoning_effort` |
| 冻结组件计划 → 知识文档 | `sdkParameters.componentInput.knowledge` | `runComponentScans().scanOptions.knowledgeBasePaths`；每个组件使用校验后的相同版本正文 |
| 冻结组件计划 → 补充扫描指令 | `sdkParameters.componentInput.scanPrompt` | `runComponentScans().scanOptions.scanPrompt` |
| 冻结组件计划 → 扫描后处理指令 | `sdkParameters.componentInput.postScanPrompt` | `runComponentScans().scanOptions.postScanPrompt` |

推理强度可选 `none`、`minimal`、`low`、`medium`、`high`、`xhigh`。选择“沿用 SDK 默认”时不添加覆盖项；模型更新请求省略 `reasoningEffort` 会保留原值，显式空字符串会清除。Web 内部的默认选项标记不写入合同。保存期间控件禁用，失败后恢复并保留选值。

兼容 Chat Completions 的供应商仅在显式配置强度时转发 `reasoning_effort`，未配置时保留原行为。供应商和模型是否支持所选强度仍须真实模型验收；连接测试、配置显示和合成调用记录不证明上游实际采用该强度。

## 不可变输入与读取

Server 创建任务时冻结模型强度，以及所选知识版本的 ID、正文与 SHA-256。后续编辑模型、编辑或归档文档不会替换排队任务的输入。Server 在事务内校验知识引用；Agent 在该执行的私有目录生成文件，Scanner 再校验执行身份、文件归属与知识哈希后调用 SDK。

组件输入只补充知识与指令，不改变已冻结的组件计划、完整源码快照或路径边界。每个组件都接收相同的冻结知识和两段指令。这里不添加 `validationPrompt`、差异范围或其他扫描字段。

任务详情提供“本次冻结的 SDK 执行参数”，并分别展示知识版本、哈希、原始正文、多行指令和原始参数合同。配置值与实际执行观察分开表达。重试沿用冻结输入；历史任务没有附加参数时继续使用原有读取逻辑，不补造强度或知识引用。

## 合同与兼容边界

参数使用独立的 `SDKScanParameters` / `SDKParametersAssignment` v1 合同，分配绑定具体 `taskId` 和 `attempt`。Scanner 描述 v11 与控制 API 4 协商 `sdkParametersVersion: 1`；现有严格模型、Assignment 和 Job v1–v8 Schema 不增加字段。

只有具备已确认运行时能力的节点才能领取带参数的任务。旧节点、缺失或未知版本、执行身份不匹配等情形不会静默丢弃参数或降级执行。Agent 使用现有子进程机制向 Scanner 传递执行私有参数文件；这不是额外的用户操作入口。

迁移 022 为模型增加 `reasoning_effort`，为任务增加 `sdk_parameters_json`，不改写旧迁移或历史数据。Server、Agent、Scanner 和 Web 需要同步更新才能使用新能力；当前 Agent 还要求 Server 显式声明无配额调度，详见[任务管理](TASK_MANAGEMENT.md)。

## 原生数量参数

Deep Worker、子代理、组件并发与发现次数已改为 SDK 0.1.25 的安全整数规则；通过 Description v12 和控制 API 4 显式协商，旧数值域之外的任务不会交给旧节点，也不会自动截断。原有 Description v11 参数侧带仍可读取。详见[原生并发规则](SDK_NATIVE_CONCURRENCY.md)。

## 回归边界

- `contracts/tests/conformance.test.mjs`：Go/TS 对新参数、分配、描述以及非法值/版本/身份的判定一致。
- `code-audit-agent/pkg/execution/compatibility_test.go`：新能力显式协商，缺失或未知参数分配不得降级。
- `code-audit-scanner/test/sdk-parameters.test.ts`：构造配置、默认行为、兼容供应商转发和执行文件校验。
- `code-audit-server/internal/httpserver/model_reasoning_test.go`：模型保存、读取、旧客户端省略和显式清除。
- `code-audit-server/internal/httpserver/sdk_parameters_pipeline_test.go` 与 `code-audit-web/tests/sdk-parameters-web-acceptance.mjs`：通过既有组件集成流水线运行；浏览器创建普通及组件任务，修改模型并编辑/归档文档后，核对所有组件实际 SDK 调用入口中的强度、两段指令与旧版本正文，同时覆盖保存失败、窄屏及历史读取。

集成验收只写一次性测试数据库，使用标记明确的 Fixtures / Mock，在 SDK 推理、认证和发布边界替换实现，组件调度使用锁定版本的原生调度器。不执行真实规划、审计、SDK 登录或外部发布，不据此宣称真实模型效果或计费已验证。
