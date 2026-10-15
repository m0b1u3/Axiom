# Axiom 合同基线

本模块定义 Server、Agent、Scanner 边界的数据，采用 JSON Schema Draft 2020-12 并生成 Go/TS 类型。Assignment 和完整结果保持 v1；新任务使用 Job v3 指定 ProgressSnapshot / ScanEvent v2，ScannerDescription 升级到 v4，心跳为 ExecutionHeartbeat v1。现有 HTTP、共享文件与 JSONL 保留；老任务的 v1/v2 job、legacy 进度/事件、结果及描述入口仍可读取。

Axiom 的用户入口为纯 Web。本文命令供开发与部署人员使用；Scanner 的进程接口由后台自动调用，使用者通过网页配置和管理扫描。审计能力由服务端 Scanner 调用 Codex Security SDK 提供。

入口合同包括 ExecutionRef、ExecutionAssignment、ScannerJob、ProgressSnapshot、ScanEvent、ScanResult、ExecutionCompletion、ScannerDescription，以及独立的 ResolvedModel、ScanPolicy、InputCapabilities 和 ScannerDescriptionV2；结果链路增加 ScannerJobV2、ResultCommit、ResultCapabilities 与 ScannerDescriptionV3；观察链路增加 ScannerJobV3、ProgressSnapshotV2、ScanEventV2、ExecutionHeartbeat、ObservationCapabilities 和 ScannerDescriptionV4；版本治理增加 AgentRuntime（ready / blocked 诊断）。公共字段和结构化证据均有类型；raw、extensions 和覆盖详情明确保留为 JSON，不作为数据库行或 SDK 类型暴露。

## 安装和验证

需要 Node.js 22.13 或更新版本，以及 Go 1.24 或更新版本。CI 分别使用 Node 22 和 24。

从本目录执行：

~~~sh
npm ci --ignore-scripts
cd go
go mod download
cd ..
npm test
~~~

npm test 会检查生成文件、构建 TS、检查 TS 类型用法与 Scanner 描述入口、执行 Go 单元测试、构建测试 codec，再运行共同样本和双向序列化测试。当前包含 132 个共同样本（63 个有效、69 个无效）和 5 个回归测试；Go 另验证 128 个任务/能力/预算组合。仅使用合成合同样本和模拟引擎，不调用模型，也不需要数据库或 API Key。Go 缓存可通过 GOMODCACHE/GOCACHE 设置到本机可写目录。

~~~sh
npm run generate
npm run check:generated
npm run build
~~~

生成器只依赖已固定的 Node 工具和 gofmt。生成文件提交到仓库，正常使用类型包不需要重跑生成器；CI 会拒绝源 Schema、兼容数据与生成文件不同步的改动。

## 定义与生成文件

- schemas/v1/definitions.schema.json 是字段与枚举的唯一可编辑定义源，其他 Schema 提供独立入口；v2/v3/v4 入口引用公共定义；旧描述、job 和结果的结构与约束保持可读。
- data/compatibility.json 保存错误别名、退出码、默认策略目录及字节限制。默认策略是目录数据，校验器不会自动填入缺失值。
- data/execution-compatibility.json 是任务、控制接口和 Scanner 格式组合的唯一矩阵源；生成 Go/TS 元数据与 [兼容矩阵](COMPATIBILITY.md)，Server 派发与等待诊断共用同一规则。
- tools/generate.mjs 从上述输入生成 Go/TS 类型、Schema 资源、codec、常量及 Scanner 的基线版本元数据。
- go/ 是独立模块 axiom/contracts，不依赖 Server、Agent、数据库或 SDK。
- ts/ 是独立类型与校验包，构建输出在 ts/dist/。
- fixtures/v1/manifest.json 声明共同样本和可选的预期执行身份。所有样本均为 Fixtures，不代表真实项目、漏洞、费用或扫描活动。
- tests/ 执行 Go → TS、TS → Go 和 TS → Go → TS 的语义比较，以及 Scanner 描述和旧 CLI 的模拟回归。

Go 校验器固定为 github.com/santhosh-tekuri/jsonschema/v6 v6.0.3，TS 校验器固定为 Ajv 8.17.1 和 ajv-formats 3.0.1。资源嵌入生成包，Go 的外部资源加载关闭，运行时不会为 Schema 发出网络请求。

类型生成器支持本目录明确使用的结构、引用、枚举、nullable、数组、映射和事件联合。不能表示的字段结构或不认识的关键字会使生成失败。if/then、范围、pattern、format 等约束由完整的 Schema 校验器执行，不能仅依赖静态类型。

## 使用方式

TypeScript 构建后：

~~~ts
import {decode} from '@axiom/contracts';

const job = decode('ScannerJob', jsonText, {taskId: '42', attempt: 2});
console.log(job.policy.mode);
~~~

Go：

~~~go
job, err := contracts.DecodeScannerJob(data, &contracts.ExecutionRef{
    TaskID: "42",
    Attempt: 2,
})
if err != nil {
    return err
}
~~~

业务边界使用 decode/Decode 校验；直接 JSON.parse 或 json.Unmarshal 不提供合同保证。Server、Agent 已引用独立 Go module，Scanner 通过本地 @axiom/contracts 包消费 TS 校验器；不向合同包引入数据库或 SDK。

Go 的 Optional[T] 保留三种不同情形：属性缺失、明确的 null、明确的零值。生成字段使用 Go 1.24 的 omitzero；必填 nullable 字段使用指针，原始 JSON 使用 json.RawMessage。事件 payload 在 Go 中保持 JSONValue，生成的 AsPhaseChangedData、AsActivity 等方法根据事件类型校验并返回对应类型；TS 事件为可按 type 收窄的联合类型。

## 冻结的语义

输入和核心结果禁止未知字段，扩展仅放在明确的 raw、extensions、coverage.details 等区域。修改核心结构、范围或枚举时必须更新合同版本与兼容策略，不能因为字段 optional 就假定旧接收方兼容。生成文件头记录 Schema 与兼容数据的摘要。

taskId、snapshotId 使用十进制字符串，attempt、sequence、revision 为安全范围内的整数。JSON 数值按有限的 binary64 值处理，原生 JSON 扩展也限制在 JS 安全数值范围内；需要更大精度的数据应使用字符串。金额字段是引擎估算，不是硬金额承诺。unknown、null 与已观测的零值分别表示；空 findings 数组仍可搭配 partial 覆盖。

JSON 文本必须是有效 UTF-8，字符串必须由 Unicode 标量组成。两端都拒绝单独的代理码位和非有限数字，避免 Go 替换字符而 JS 保留字符导致证据往返变化；中文与合法 emoji 保持原值。

Assignment 的临时凭据是独立侧带，ScannerJob 不接收密钥字段。除 Schema 外，校验入口还检查期望执行身份、Completion 内外身份一致性，以及凭据绑定名称与模型描述的一致性。凭据样本只使用 fixture 标记；业务集成时不得将 Assignment 的临时凭据整包归档。

日期执行 format 校验，并要求 UTC 的 Z 形式。校验不插入默认值、不转换字符串为数字、不删除未知属性。Go typed codec 会将 Schema 允许的整数写法 2.0、2e0 归一为整数后解码，以与 JS 数值语义一致。

默认策略目录记录 standard、3600 秒扫描时限和 default 验证模式；deep 收尾宽限为 600 秒。Server 创建任务时解析并保存策略，Agent 与 Scanner 使用这份时间预算。workers 字段缺失意味着保留 SDK 默认值。Web maxCostUsd 留空或 null 表示未设置，数据库保留 NULL，ScannerJob 与 SDK 调用省略该字段；0 和负数被拒绝。

终态退出码固定为 completed=0、partial=2、failed=2、canceled=7、timeout=8。错误别名由显式目录映射，消息文本不能决定错误类别。文档总大小上限 32 MiB，进度 1 MiB，单条事件 2 MiB；字符范围和传输字节范围分别验证。

文件存在性、快照内容哈希、目录权限、源码与产物目录归属、租约和幂等提交不能只靠 JSON Schema 保证。Agent 在启动 SDK 前核对快照哈希与输出目录边界，以 0600 权限原子发布 job.json；完整完成合同绑定任务与 attempt，Server 在归档事务中验证租约、Agent 归属和模型快照。同内容重送成功，内容冲突返回 completion_conflict。

## Scanner 描述

现有入口增加 --describe 查询，在读取凭据或创建输出目录之前返回元数据；引擎改为在正常扫描分支中加载。

~~~sh
cd ../code-audit-scanner
npm run scan -- --describe
~~~

该命令使用现有 tsx 启动方式。合同测试通过原生 TS 加载适配运行它，不安装或加载 SDK。

输出区分 baselineVersions 和 runtime.versions：Assignment 为 v1，Job 支持 v1/v2/v3，进度与事件支持 v2，结果和完成支持 v1；jobFile、maxCostUsd、versionedResults、atomicResultCommit 开启。新版 Job v3 的事件为 scan_event（v2），structuredEvents 为 true；旧 Job v1/v2 保留原格式。配置中的 SDK 版本与 ScanResult 中记录的实际 SDK/插件/补丁版本分别表示。

## 已接入的输入链路

Web 上传源码并选择模型配置、费用预算、范围、时限与深度参数。Server 保存模型/策略快照，新增快照任务使用输入版本 1；旧任务保留版本 0 和未设置预算。内部 v2 presence/claim 传递 InputCapabilities 与 Assignment，旧 claim 跳过新版任务。没有兼容节点时，网页显示等待兼容执行节点，不降级或忽略预算。

ScannerJob 是 Agent 写入的进程输入，不是用户功能入口。数组和多行业务背景保留 JSON 语义，任务 ID 保持字符串。密钥只随临时 Assignment 侧带传递，再注入选定环境变量，不进入 job 文件；来自 Agent 环境的其他模型凭据与登录态被剥离。

模型参数使用创建时快照。密钥仅在当前配置的供应商、端点、协议及环境绑定仍与原连接一致时允许轮换；修改或删除连接后使用原任务的加密凭据快照，避免把另一连接的密钥传给原端点。重试沿用模型与扫描参数；修改这些参数后应在网页新建扫描。

Scanner 构建前需要安装 contracts 根目录依赖并构建 TS 包；本地包通过 file:../contracts/ts 链接。Docker 清单已包含相同构建步骤与独立 Go 合同 module。

完整输入验收位于 code-audit-server/internal/httpserver/input_pipeline_test.go，需要专用 axiom_test_input PostgreSQL 数据库、已构建 Scanner 与 Web。AXIOM_TEST_WEB_INPUT=1 还执行真实浏览器上传/预算校验/创建扫描；仅替换 SDK 实例为测试实现，Server、Agent 和 Scanner 保持实际调用链。测试会清空该专用数据库，拒绝非本机或不以 axiom_test_input 开头的数据库地址。不可指向业务数据库。

步骤 4 的结构化进度/事件与步骤 5 的 Go 模块边界已接入。测试 codec、SDK 替换注册器和加载适配器只服务验收，不可作为生产传输或引擎实现。

## 已接入的结果链路

新增迁移 008_result_contract.sql，既有任务和 attempt 保留结果版本 0，新 Web 快照任务固定为结果版本 1。内部 v3 presence/claim 协商输入与结果能力；结果需要 Job v2、Result/Completion v1 和 Commit v1，旧执行节点不会领取新版结果任务。HTTP v3 与文档 schemaVersion 独立。部署顺序为 Server 消费者先升级，再部署配套 Agent/Scanner；仍有新版结果任务时，应保留支持它们的 Server 消费者。

Scanner 把 SDK toJSON 快照写入私有原生产物，使用同一份快照构建完整结果；原生 findings、coverage、turn、费用与未知扩展保留在 extensions.sdk 中。具名字段保存模型配置 revision、引擎/插件/已核验补丁版本、Scanner 起止时间、置信级别与理由、结构化代码证据、验证与溯源。数值置信度只接受原生报告，不从级别合成百分比。费用未知为 null，已观测的 0 保持 0；缓存、推理和未来计费字段仍完整保留并可在网页查看。

结果先写临时文件、sync 并原子 rename；随后发布 normalized/result.commit.json，绑定执行身份、结果版本、字节数与 SHA-256。POSIX 上同步父目录，Windows 上使用文件 sync 与 rename。Agent 先读标记，再验证有界结果文件、大小、哈希、Schema 和预期 taskId/attempt；有效已提交结果按明确策略优先于进程晚期异常，进程退出和差异同时归档。取消、超时的执行终态与已提交 SDK 结果分别保存。标记仅用于完整性检查，不是安全证明或恢复 checkpoint。

Agent 传递完整生成 DTO，Server 在一个事务中归档 ExecutionCompletion 并建立 Finding 搜索索引。模型必须与任务快照一致；SDK 实际报告的模型保留在原生 turn 中，与配置模型分别展示。历史归档独立于后续重试与人工状态。空漏洞列表与空对比列表返回数组，避免 null 被误用为完整结果或使页面崩溃。

32 MiB 是合同文档的字节上限。Scanner 为完成信封预留 64 KiB，超限明确失败并保留原生产物，不截断字段。Go 的合同编码关闭 HTML 转义，避免代码证据里的 <、>、& 在跨语言往返后膨胀；大小限制与字符限制仍分别执行。

Web 展示费用、原生 Token 用量、配置/报告模型、引擎元数据、证据和执行差异，并支持完整 JSON 与原生产物下载。产物只按归档引用读取，Go os.Root 限制到该次输出目录；复制为私有下载快照，核对大小/SHA-256 后提供字节，避免校验与发送之间源文件变化。已损坏或未保留的产物会明确返回错误。

结果验收共用 input_pipeline_test.go / result_pipeline_test.go：真实 Server、PostgreSQL、Agent、Scanner 只在 SDK 实例处替换为合成实现；覆盖完整结果、空/部分结果、SDK 鉴权失败、取消、晚期退出、身份错误、缺失标记、坏 JSON、哈希/大小异常、超限和产物篡改。浏览器验证费用、证据、原生 ID、完整下载、未知费用与执行异常；不调用模型。


## 进度与事件链路（步骤 4）

ObservationCapabilities 单独声明 Job v3、Progress/Event v2、Heartbeat v1。迁移 009 为新 Web 快照任务和 attempt 固定 observation_contract_version=1；旧记录为 0。内部 HTTP v4 presence/claim 协商能力，旧节点不会领取新观察合同的任务。结果与输入合同版本不随之改写。

Scanner 是 state/progress.json 和 logs/events.jsonl 的唯一写入者。SDK 回调与只读工作台观察进入同一队列，revision 和 sequence 分别单调递增。未知阶段以 unknown + rawPhase 保留；files、phaseCounts、deep 分开表示，计数缺失为 null，上报 0 保持 0，发现次数上限不表示覆盖率。工作台观察只在 Scanner 内处理，Server 不读取 SDK 文件或数据库。观察去重、按阶段清理旧计数，并阻止旧阶段覆盖较新的回调。

Snapshot 保存最新活动摘要、线程身份/父线程/状态/观察时间、调度信息、累计费用估算、Token、连接重试及带来源的错误。完成一个线程轮次只表示 waiting，不能据此认定整个审查成功。Snapshot 保留最近最多 256 个线程，字节预算不足时缩减保留集合并标明 workersTruncated；完整活动记录留在事件中。活动的截断和省略路径有明确字段，超大事件受字节限制。

ExecutionHeartbeat 直接携带 progress 对象或 null，核对内外 taskId/attempt。Server 在锁定任务行的事务中核对所有者和租约，只接收更大的 revision；重复/旧 revision 续租但不替换进度，null 不擦除已有快照。任务与 attempt 同时归档进度和接收诊断。取消请求保留在任务上，attempt 继续表达实际执行，心跳将 cancel_requested 返回给 Agent。最终进度确认接收后才提交完整结果，进度本身不能改变业务终态。

事件以 (taskId,attempt,sequence) 去重；message 不参与新版事件的状态判断。Server 增量索引完整换行，按 sequence 补拉，处理半行、重复、混合身份、超大记录和文件截断，并公开无效记录数量/诊断。旧 n/after 继续使用；新版 n 映射到 sequence。Web 提供阶段/单位/来源/时间、Worker、估算费用、结构化错误、原始快照、原始事件和已加载 JSONL 下载。断线恢复仍通过原轮询接口。

部署顺序为 Server 消费者（含迁移 009）→ 配套 Agent 与 Scanner → Web。新任务排队时若只有旧观察能力，显示 observation_incompatible；旧历史始终使用其固定格式。生产扫描仍直接调用 SDK，浏览器验收只替换 SDK 模型调用为合成回调。

## Go 控制接口信封（步骤 5）

`go/control` 保存现有 HTTP presence/claim 信封、能力字段、本机共享路径，以及明确命名的 Legacy DTO。它们是 Go 控制接口类型，手工维护既有 JSON 形状；不冒充生成的跨语言 Schema。Assignment、Heartbeat、Completion 和 Scanner 输入/产物继续使用现有生成类型，Schema 版本和传输方式不变。

本包只包含数据定义，不包含 HTTP 客户端、数据库行、SDK 类型、凭据解密或文件访问。Agent 的 HTTP 客户端留在 Agent，数据库行到合同的投影留在 Server。现代 Agent Lease 保留校验后的 Assignment 和 ExecutionRef；旧数据只能经独立 Legacy 适配器执行。

Provider 预设位于 Server 配置层。Agent 使用 Assignment 的完整连接描述和选定凭据，不查供应商注册表；新供应商的 `_API_KEY` 环境变量也不会作为其他 Web 任务的备用登录。源码树哈希位于独立的 `axiom/sourceidentity` 标准库模块。

Server/Agent/合同/源码哈希四个 Go 模块由 `deploy/check_go_boundaries.py` 检查真实构建、测试及 module 图，`go-boundaries.yml` 在 CI 拦截跨应用依赖和 Agent 的数据库依赖。旧任务 JSON、执行身份、模型参数快照和密钥轮换均有回归测试。
