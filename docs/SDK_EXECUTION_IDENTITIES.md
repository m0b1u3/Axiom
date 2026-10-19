# SDK 0.1.25 执行身份与会话管理

本批接入公开 `loginApiKey()`、`loginChatGPTDeviceCode()`、`account()`、`logout()`。所有操作由 Web 发起，继续使用 Go Server、Go Agent、Node Scanner 和 HTTP / 共享产物 / JSONL。SDK 版本仍为 0.1.25；不引入用户终端登录步骤。SDK 自身的 Codex 子进程由 Scanner 管理。

验收使用临时数据库、本地服务和标记为 Fixtures / Mock 的 SDK 替身；没有真实登录、模型规划、审计或外部发布。本次只更新源码，已有 Docker 部署尚未应用本增量。

## Web 工作流

1. 在模型配置中选择原生 OpenAI Responses 模型，可勾选“扫描时使用 SDK 执行身份提供凭据”。此模式不保存模型 API Key，创建任务时必须选择身份。
2. 设置 → SDK 执行身份：创建名称、类型及绑定 Agent ID。Agent ID 可在执行节点中核对；只有相匹配的就绪节点可以领取。
3. API Key 身份通过密码输入框保存并登录；设备码身份通过明确的登录动作显示授权网址、设备码和本地等待截止。设备码不会自动打开外部页面。
4. 查询 SDK 账户状态，查看最后检查时间。`authenticated` 仅表示 SDK 认证结果，不证明 Trusted Access、模型权限、套餐可用性或供应商账单。
5. 新建扫描、组件建议和冻结组件计划执行时选择身份。任务冻结身份 ID、名称、Agent、类型和凭据版本；派生预检、复核、失败组件重新执行及普通重试继承冻结绑定。
6. 撤销立即阻止新领取并请求取消运行任务。绑定节点确认 SDK 退出且清除本地凭据后，Web 才显示清理完成；节点离线时保持待清理，可显式重试。

Axiom 账户、SDK 执行身份、模型配置是不同对象。登录、设备码、操作历史和撤销仅管理员可用；有读取权限的账户可查看身份元数据，任务操作沿用现有角色权限。当前首版仍是可信用户的自托管应用，不宣称多租户或不同 OS 用户之间的强隔离。

## 合同和版本

`contracts/schemas/v1/sdk-identities.schema.json` 统一定义：

| 合同 | 边界 |
| --- | --- |
| SDKIdentityRef / Snapshot | 身份、绑定节点、凭据版本、最后检查和本地清理状态；没有密钥或账户原始详情 |
| SDKIdentityOperation | 动作、生命周期、本地截止、受限设备指引及通用错误 |
| SDKIdentityClaim | 控制队列租约、明确的 `leaseSeconds`；仅 API Key 登录携带一次性私有密钥输入 |
| SDKIdentityReport | 操作、身份、节点、版本、租约令牌与递增序号；重复终态幂等，冲突报告拒绝 |
| SDKTaskIdentity | 与执行 taskId / attempt 绑定的冻结身份侧带 |
| SDKDeviceInstructions | 固定授权来源、设备码及本地截止；不接受任意 URL |

Go / TS 类型和校验器由统一 Schema 生成。新增 ScannerDescription v10 声明身份版本 1；业务输入 1–6、Job 1–8、结果 1、进度/事件 2、控制 API 4 继续使用原版本。策略来自 `contracts/data/sdk-identity-compatibility.json`。

Server 发现端点显式声明能力；新 Agent 遇旧 Server 不发送身份字段。身份领取必须有最近 30 秒内 ready、兼容矩阵匹配、描述 v10 和控制 API 4 的节点上报；旧节点仍可执行原有任务，无法领取身份凭据或降级为其他账户。

迁移 021 只追加身份、操作、任务绑定三张表。历史 Schema、迁移 001–020、SDK 锁与七处既有补丁保持不变；历史任务缺少身份引用时继续使用原有凭据规则。

## 身份与执行隔离

Server 只保存身份元数据，以及尚未终结的 API Key 登录操作的加密输入。成功、失败、取消或超时都会清除待处理密文；浏览器提交成功后清空密码输入。API Key 不出现在任务输入、进程参数、公开身份快照和结果里。

Agent 从受校验的引用派生私有目录：

```text
<DataDir>/.sdk-identities/<Agent ID hash>/<identity ID>/generation-N/
  sdk/                  SDK 持久凭据目录
  session.json          无密钥的冻结身份标记
  selected-api-key      仅 API Key 身份，Agent 自有文件
  executions/           每次执行的私有 SDK 状态
```

目录权限 0700，Agent 自有输入/密钥文件 0600；拒绝路径组件中的符号链接。Agent 不解析 SDK auth JSON。登录操作私有输入在进程结束后删除；任务只收到无密钥侧带，选中的 API Key 通过环境注入，ChatGPT 身份显式使用 `auth: "chatgpt"` 并移除环境 API Key。执行状态不放进公开产物目录，工作台快照仍只复制不透明 SQLite，不包含凭据。

同一身份同时只领取一个父执行，防止多个隔离副本覆盖刷新后的会话；不同身份仍受全局/模型/项目并发控制，组件内部按已冻结配置由 SDK 调度。扫描正常关闭后，Scanner 使用公开 `importAmbientAuth()` 不透明复制刷新凭据，原子替换身份的凭据目录，再删除每次执行的私有状态。不会解析 token 字段或复制工作台、源码到凭据持久层。保存失败会移除本地可用标记并报告进程异常，不回退到旧凭据或其他账户。强制杀进程可能使刷新保存无法完成；Web 的状态是最后检查记录，需要显式查询账户或重新建立身份。

身份控制动作共用 Agent 工作槽、Server 并发与存储容量准入，具有 10 分钟本地截止及独立租约，不启动模型也不占用扫描预算账本。Agent 使用 Server 下发的租约，网络阻塞不能超过安全续租窗口；失去租约会停止 SDK 进程组。登录不会自动重放。

## 撤销、取消与未知状态

| 情况 | 行为 |
| --- | --- |
| 设备码等待时取消 | Server 清除设备码和待处理密钥；Agent 取消原生句柄；再次登录必须由用户发起 |
| 节点失联或超时 | 操作转 expired，身份状态 unknown，迟到成功不恢复认证 |
| 再次登录 | 增加凭据版本；旧任务保留原绑定，不能自动采用新会话 |
| 撤销 | 同一准入事务中增加版本、标记 revoked、禁止新领取并请求停止运行任务 |
| 退出清理 | 等待运行任务及已取消身份操作的租约/终结确认，再退出并删除此身份的全部本地版本；其他身份不受影响 |
| 退出失败或节点离线 | 保持 revoked + cleanupPending；显式重试，直到节点确认清理 |
| 已完成任务和报告 | 继续显示原身份版本、结果和费用，不因退出而删除或重新绑定 |

撤销的是 Axiom 的身份使用权和本地会话，不等同于供应商吊销 API Key 或终止其他设备的账户会话。认证后的费用仍沿用已有 SDK 估算与未知费用语义，不冒充实际账单或承诺 ChatGPT 套餐覆盖。

## 接口

| 方法 / 路径 | 权限 / 语义 |
| --- | --- |
| GET `/api/v1/sdk-identities` | 读取；仅元数据，no-store |
| POST `/api/v1/sdk-identities` | 管理员；创建待配置身份 |
| GET `/api/v1/sdk-identities/{id}/operations` | 管理员；受限指引与操作历史，no-store |
| POST `/api/v1/sdk-identities/{id}/operations` | 管理员；action + 观察到的 generation；仅 API 登录接受 apiKey |
| POST `/api/v1/sdk-identity-operations/{id}/cancel` | 管理员；取消未结束的非 logout 动作 |
| POST `/api/internal/sdk-identities/claim` | Agent；能力、节点和租约校验，敏感响应 no-store |
| POST `/api/internal/sdk-identities/report` | Agent；完整绑定、序号及幂等校验 |

设备授权网址仅允许 `https://auth.openai.com/codex/device`。SDK 的 `details`、原生 stdout/stderr 不对 Web 发布，失败文本统一为可恢复的领域提示。当前没有回调式 `loginChatGPT()` Web 入口，设备码方式已经覆盖本批远程自托管登录需求。

## 验收

入口为 `TestSDKIdentitiesPipeline`、Agent 的 `TestIdentityLeaseLossStopsNativeProcessUsingServerLease`、Scanner / Web 身份单元测试以及 `tests/sdk-identities-web-acceptance.mjs`。数据库夹具只允许显式配置的 loopback `axiom_test_input*` 库；浏览器测试拒绝现有 5173/5174 部署，并拦截外部请求。

覆盖 API Key / 设备码 / 新进程账户查询、刷新会话回存、节点及能力拒绝、同一身份执行互斥、任务/结果版本绑定、撤销运行任务后清理全部凭据版本、取消不重放、失联及迟到报告、重复与冲突终态、容量限制、只读权限、失败重试和桌面/窄屏交互。现有输入、结果、预算、组件、历史读取及模块边界继续回归。

本地证据位于 `output/playwright/sdk-identities/verification.json`，浏览器截图和 11 项交互记录位于同目录的 `browser/`；不提交私有测试日志或凭据文件。真实账户、供应商政策、模型访问、计费与长期 token 刷新仍需单独授权验收。

后续部署应按 Server 迁移 → 配套 Agent/Scanner → Web 更新，保持 Agent ID 和私有 DataDir 稳定。备份该目录会包含账户凭据，需按现有私有备份流程保护；搬迁或丢失本地会话后从 Web 重新配置，不能仅靠业务数据库恢复登录状态。
