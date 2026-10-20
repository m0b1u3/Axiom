# 执行合同兼容矩阵

<!-- Generated from data/execution-compatibility.json. -->

包版本用于诊断；任务派发以实际声明的合同能力为准。HTTP 控制接口版本与文档 schemaVersion 分别管理。

| 任务格式 | 输入 / 结果 / 观察 | 最低控制接口 | Scanner 描述 | Job | 进度 / 事件 |
| --- | --- | --- | --- | --- | --- |
| legacy | 0 / 0 / 0 | 1 | 1 | 旧参数 | 0 / 0 |
| input | 1 / 0 / 0 | 2 | 2 | 1 | 0 / 0 |
| result | 1 / 1 / 0 | 3 | 3 | 2 | 0 / 0 |
| observations | 1 / 1 / 1 | 4 | 4 | 3 | 2 / 2 |
| scan_input | 2 / 1 / 1 | 4 | 5 | 4 | 2 / 2 |
| sdk_operation | 3 / 1 / 1 | 4 | 6 | 5 | 2 / 2 |
| git_diff | 4 / 1 / 1 | 4 | 7 | 6 | 2 / 2 |
| component_planning | 5 / 1 / 1 | 4 | 8 | 7 | 2 / 2 |
| component_scan | 6 / 1 / 1 | 4 | 9 | 8 | 2 / 2 |

最新 Server 支持控制接口 1–4，并发布矩阵摘要。带摘要的 Server 与 Agent 必须使用同一份矩阵；摘要不一致会阻止模型启动，并上报配套组件诊断，不能据包版本猜测兼容。旧组件缺少运行元数据时，以已声明合同能力判定，缺失的版本显示为未知。历史控制客户端可用 presence 探测旧 Server：仅 404/405 可尝试下一接口；认证、网络和数据错误不降级。领取失败不切换接口，已领取任务的固定合同不降级。不支持的格式在模型启动前拒绝。

升级顺序：Server 消费者及迁移 → 配套 Agent/Scanner → Web。旧 Agent/Scanner 只能领取其支持的任务，普通新 Web 任务保持输入/结果/观察 1/1/1；高级参数和知识文档任务为 2/1/1，使用 base Assignment v1 + ScanInputAssignment v2，绑定同一 execution，再物化 Job v4。SDK 独立操作为 3/1/1，另绑定 SDKOperation v1，物化 Job v5，由 Scanner Description v6 声明；缺少操作 sidecar 或 Job v5 能力会在 SDK 前拒绝。Git 差异任务为 4/1/1，使用 GitDiffAssignment v1 和 Job v6、Description v7，严格绑定冻结 base/head 与源码哈希；旧节点不得领取或降级。组件审计为 6/1/1，绑定 ComponentScanRequest v1 与冻结计划，物化 Job v8、由 Description v9 声明；缺少源码、执行、版本或预算绑定时在 SDK 前拒绝。旧 Server 的已知 profile 无法领取 input=2，不能回退或丢弃新参数。历史任务、结果、日志、覆盖报告和 HTML/PDF 报告读取器保留。未知合同不能推断为兼容。运行诊断每 10 秒上报，30 秒后失效；在线/就绪节点和明确的调度模式分别展示；正数固定槽位仅保留为旧节点元数据。缺少、无效或未知版本的 Scanner 描述会阻止新 Agent 领取任务，并向新版 Server 上报诊断。

Schema 核心字段变化（含严格对象中的新增 optional 字段）必须新增 schemaVersion，先发布消费者，再切换生产者。已有任务及 attempt 的合同组合不修改、不降级。旧 reader 保留；仅在证明无入口引用且没有历史写入需求后移除 adapter。升级后用独立 Fixture 验证任务创建、取消、结果/日志/报告读取和组件组合，不以生成类型通过代替进程验收。

当前 Agent 使用显式协商的无配额调度：Server 必须声明 taskDispatch=unbounded，控制 API 4 的 presence 必须同时声明 dispatchMode=unbounded 与 slots=0。未知或缺失策略不回退到旧 Server；旧正数槽位上报仍可读取。迁移 023 支持该 0 值，任务队列不再受全局/模型/项目配额或组件权重限制。Web 保留旧配置和历史记录读取，退役写入返回 410；effectiveCapacity=null，不虚构容量大数。该策略属于控制面发现与存活上报，不改变历史 Assignment/Job/Result Schema。

SDK 参数侧带为版本 1，由 Description v11 与 Server sdkParametersVersion=1 协商；在保持历史严格模型与 Job Schema 不变的前提下，绑定 execution 传递推理强度和组件知识/指令。未知版本、旧节点和缺失侧带在模型启动前拒绝；历史任务缺少侧带时沿用原默认值。

SDK 执行身份侧带为版本 1，由 ScannerDescription v10 和 Server 发现字段 sdkIdentityVersion=1 共同声明。身份控制队列需最近 30 秒内 ready、矩阵匹配、控制 API 4/已知描述 10、11 或 12 的节点报告；身份任务另核对冻结的 Agent/凭据版本，且 v12 配套节点允许同一身份的父执行并行，凭据快照与写回版本校验仍隔离。缺少身份能力的历史客户端不下发身份字段；当前 Agent 另要求上述无配额策略；旧节点保留历史 profile，不能领取身份任务。此增量不改变上述历史 profile 或严格 Schema。

SDK 原生数量域由 sdkConcurrencyVersion=1 与 Description v12 声明。既有 Worker、子代理、组件并发和发现次数字段只扩宽到安全整数域，字段结构与历史读取不变；超过旧域的任务需显式能力及新鲜 v12 节点上报，不能交给旧节点或自动截断。迁移 024 将数值列扩为 BIGINT；Web 主动配置的预算、超时与范围保留。同身份并行仅对已确认该能力的新节点开放，ChatGPT 使用执行私有凭据快照与原子 CAS 写回。详见 [SDK 原生并发规则](../docs/SDK_NATIVE_CONCURRENCY.md)。
