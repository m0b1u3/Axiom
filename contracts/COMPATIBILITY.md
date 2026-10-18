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

最新 Server 支持控制接口 1–4，并发布矩阵摘要。带摘要的 Server 与 Agent 必须使用同一份矩阵；摘要不一致会阻止模型启动，并上报配套组件诊断，不能据包版本猜测兼容。旧组件缺少运行元数据时，以已声明合同能力判定，缺失的版本显示为未知。新 Agent 只用 presence 探测旧 Server：仅 404/405 可尝试下一接口；认证、网络和数据错误不降级。领取失败不切换接口，已领取任务的固定合同不降级。不支持的格式在模型启动前拒绝。

升级顺序：Server 消费者及迁移 → 配套 Agent/Scanner → Web。旧 Agent/Scanner 只能领取其支持的任务，普通新 Web 任务保持输入/结果/观察 1/1/1；高级参数和知识文档任务为 2/1/1，使用 base Assignment v1 + ScanInputAssignment v2，绑定同一 execution，再物化 Job v4。SDK 独立操作为 3/1/1，另绑定 SDKOperation v1，物化 Job v5，由 Scanner Description v6 声明；缺少操作 sidecar 或 Job v5 能力会在 SDK 前拒绝。Git 差异任务为 4/1/1，使用 GitDiffAssignment v1 和 Job v6、Description v7，严格绑定冻结 base/head 与源码哈希；旧节点不得领取或降级。组件审计为 6/1/1，绑定 ComponentScanRequest v1 与冻结计划，物化 Job v8、由 Description v9 声明；缺少源码、执行、版本或预算绑定时在 SDK 前拒绝。旧 Server 的已知 profile 无法领取 input=2，不能回退或丢弃新参数。历史任务、结果、日志、覆盖报告和 HTML/PDF 报告读取器保留。未知合同不能推断为兼容。运行诊断每 10 秒上报，30 秒后失效；在线槽位与通过预检的可用槽位分别展示。缺少、无效或未知版本的 Scanner 描述会阻止新 Agent 领取任务，并向新版 Server 上报诊断。

Schema 核心字段变化（含严格对象中的新增 optional 字段）必须新增 schemaVersion，先发布消费者，再切换生产者。已有任务及 attempt 的合同组合不修改、不降级。旧 reader 保留；仅在证明无入口引用且没有历史写入需求后移除 adapter。升级后用独立 Fixture 验证任务创建、取消、结果/日志/报告读取和组件组合，不以生成类型通过代替进程验收。

SDK 执行身份侧带为版本 1，由 ScannerDescription v10 和 Server 发现字段 sdkIdentityVersion=1 共同声明。身份控制队列需最近 30 秒内 ready、矩阵匹配、控制 API 4/描述 10 的节点报告；身份任务另核对冻结的 Agent/凭据版本，且同一身份的父执行串行。新 Agent 遇旧 Server 不下发身份字段；旧节点保留历史 profile，不能领取身份任务。此增量不改变上述历史 profile 或严格 Schema。
