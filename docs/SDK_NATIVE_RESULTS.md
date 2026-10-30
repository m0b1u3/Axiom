# SDK 原生结果、导出与诊断基线

当前 SDK 为 0.5.0，DeepSeek 小项目已有扫描、原生 Schema 独立复核、归档与图谱的有限真实证据，见 [当前状态](CURRENT_STATUS.md)。迁移 028 的平台单价估算继续保留。2026-10-28 已在本地移除 SDK 内置单价估算调用、费用回调展示和独立估算入口；新扫描不将 SDK 金额写入平台结果或进度，Token 统计保留。旧费用及原始 SDK 归档仍可读，缺少必要单价或用量时费用未知，详见 [模型计价](MODEL_PRICING.md)。下文保留早期接入时的版本及验收记录。

2026-10-15。固定 SDK 0.1.25 和既有受管理补丁；保留 Go Server + Go Agent + Node Scanner、HTTP、共享产物和 JSONL。所有业务入口在 Web，本批不执行真实审计、SDK 账户登录或外部发布。

## 已实现的 Web 行为

执行详情的归档结果中增加“SDK 原生结果与导出”。页面分别提供原生 Markdown/SARIF 的状态、原文预览、下载、字节数、SHA-256，以及原生 Scan/Target ID。预览只展示原文，不执行 Markdown 中的 HTML、脚本或外部资源。

只有已归档且通过检查的原生产物才显示可用入口。SDK 未生成文件时明确显示“未生成”；不从 Axiom HTML/PDF 报告合成一个所谓的原生报告。Markdown 检查非空 UTF-8 文本，SARIF 检查 UTF-8、JSON、2.1.0 版本及 runs 数组；这是读取前的格式检查，不声称完成全部 SARIF 规范认证。格式核验上限为 8 MiB，Web 原文预览上限为 256 KiB；超限或格式异常保留原始归档引用，专业用户仍可经原生产物列表读取完整文件。

下载继续使用原有结果产物接口，复制为私有临时快照并核对归档字节数和 SHA-256。文件在完成后发生变化会返回 409，页面保留领域内错误。不存在、未保留或未列入归档引用的文件不能按路径直接下载。

## 仓库级漏洞历史

Scanner 将 SDK 实际返回的 repositoryFindings 规范化为独立合同，保留 Finding/Occurrence/Scan/Target ID、开放或关闭状态、最近扫描确认、已知起始时间、已知 Scan ID 和匹配 Finding ID。完整原始字段继续保存在 extensions.sdk。

Web 支持本次返回历史的搜索、分页和 Inspector。SDK closed 不自动变成 Axiom resolved；confirmedInLatestScan:false 不被改写为已确认。未提供历史、明确返回空列表和历史格式不受支持分别展示，不推断整个仓库没有漏洞。

Server 在完成归档的同一事务中建立 SDK Scan ID → Axiom task/attempt 的派生索引。查找必须来自当前归档中的历史引用，并匹配同一不可变源码内容哈希；不能用任意 ID 枚举其他来源。匹配结果分页返回，重复原生 ID 保留全部候选，不自动选一个；找不到时仍显示原生 ID。链接带 attempt，打开指定执行记录，缺失的执行不会默默显示当前结果。

索引仅使用统一合同字段，Server/Agent 不解析 SDK 数据库。旧结果未保存新版扩展时继续提供原始 SDK JSON 和历史产物下载，不伪造缺失的结构化关联。清理目标任务后，其他报告里的原生 ID 仍保留；派生索引随对应尝试删除。

## 结构化警告与观察器错误

SDKDiagnostic v1 保存执行身份、独立序号、回调时间、来源、kind、观察器、消息、错误名称/代码和 details。warning 的 target_changed 与 observer_error 分开；不依靠消息关键词判断，也不将观察器失败自动当成扫描失败。

SDKDiagnostics v1 保存两类总数、最近最多 32 条记录和 omittedCount。Web 按最新记录优先展示，可滚动、键盘展开和查看原始记录。长文本明确标注截断；错误栈和任意环境对象不归档，已知凭据值与密钥形态脱敏。

Scanner 独立写入 logs/sdk-diagnostics.jsonl，完成前等待写入队列。新结果引用完整有序日志，核对序号、执行身份、条数和归档哈希；核验上限为 32 MiB。日志写入或核验失败时保留最近快照并明确提示。原始日志引用仍受既有下载校验保护。

旧 ScanEvent v2 禁止新增字段，因此其结构不改写：原事件继续记录警告文本，版本化诊断经 ProgressSnapshot 的扩展、最终结果扩展及上述 JSONL 传递。观察器错误可能影响进度/日志的完整性；终态由实际执行及归档 SDK 结果决定。

## 历史 estimateScanCost 用量估算（已停用）

早期 Scanner 曾调用 SDK 公开的 estimateScanCost(model, usage)，使用 SDK 返回的 turn.model 与 turn.usage。该接入现已移除：不再调用函数，不再生成估算，Web 不再提供“SDK 用量估算”及“SDK 回调估算”。新 SDKNativeResult 的 usageEstimate 为 null；旧归档中的对象保持原值，只用于历史读取。

数据注明 SDK 0.1.25、计算方法、返回轮次范围、模型、原始用量和计算时间。已知 0、缺模型、缺用量、不支持的定价/用量格式分别表示。模型不支持时不使用配置模型或当前市场价格猜一个金额。

该函数使用锁定 SDK 内置定价及已有用量，不能当作开扫前报价或供应商账单。未提供的 Worker/轮次用量无法补计；任务总预算、最后收到的费用、人工对账和重试剩余额度沿用原账本，不被此入口覆盖。读取不需要操作权限，也不会修改任何账本。

## 合同、容量与历史读取

新增四个独立根 Schema：SDKNativeResult、SDKCostEstimate、SDKDiagnostic、SDKDiagnostics，均为 v1 并生成 Go/TS 类型。结果放在 extensions.sdkNative，诊断快照放在 extensions.sdkDiagnostics；基础结果、Job、描述、进度和事件版本、执行兼容矩阵均不改写。

内外执行身份、诊断累计与保留条数、序号连续性均由 Go/TS 校验。补充产物引用必须确实存在于完整结果的归档列表。未知或损坏的可选扩展单独提示，核心结果、旧报告和完整 JSON 继续可读。

完整 SDK JSON 是依据。补充历史/用量展示过大时明确显示容量限制并保留完整 SDK 原始字段；诊断可缩减显示集合并增加 omittedCount，完整日志仍保留。补充信息不能把原本可提交的完整结果挤成超限失败。原有 32 MiB 合同和完成信封预留仍适用，原始结果自身超限继续明确失败，不截断证据。

迁移 015 仅追加 sdk_native_scans 派生索引表和索引；SQLite 使用独立追加表，原 schema.sql 不改写。001–014 和历史 Schema 保持原值。数据库备份自然包含新索引表；旧组件忽略新可选扩展。部署时先升级 Server/迁移，再升级配套 Agent/Scanner 与 Web，旧生产者生成的结果继续使用历史入口。

## API

- GET /api/v1/scans/{id}/attempts/{attempt}/sdk-cost-estimate：仅为兼容历史客户端读取已冻结的 SDKCostEstimate，绝不重新计算。新结果未生成 SDK 估算时返回 410；没有原生扩展的旧结果为 404，不支持的扩展为 409。
- GET /api/v1/scans/{id}/attempts/{attempt}/sdk-history-links?scanId=...&page=1：仅查询归档中引用的原生 ID，按同源码查找，每页 50 条；保留全部候选。
- 原生产物与诊断日志继续 GET /api/v1/scans/{id}/attempts/{attempt}/artifacts?path=...。

以上沿用 read 权限及既有产物下载记录。新接口不调用 SDK、数据库之外的账户或发布服务；SDK 解释仍只存在于 Scanner。

## 合成验收与边界

跨语言样本覆盖有效原生结果、已知零/未知估算、SDK 关闭状态、警告/观察器来源、未来版本、错误身份、无效引用与诊断计数/序号。Scanner 使用实际公开估算函数及官方 Mock，另覆盖损坏产物、脱敏、完整日志和大体积历史。

真实 Server、临时 PostgreSQL、Agent 和 Scanner 的端到端验收仅在 SDK 实例处替换合成实现。覆盖八种原生结果场景、父执行关联、53 个同 ID 候选分页、来源隔离、幂等归档及 SARIF 完成后篡改拒绝。浏览器覆盖原文预览/下载、费用未知、权限、历史/未知扩展、错误恢复和精确执行跳转；禁止站外请求。

构建 Scanner/Web 后，可在 Server 目录以本机 axiom_test_input* 专用数据库运行：

```sh
AXIOM_TEST_DATABASE_URL='postgres://postgres:fixture-password@127.0.0.1:5432/axiom_test_input_native?sslmode=disable' \
AXIOM_TEST_WEB_INPUT=1 \
AXIOM_TEST_NATIVE_REPORT_DIR="$PWD/../output/playwright/sdk-native" \
go test -p 1 -count=1 -timeout=12m ./...
```

测试会清空专用数据库，不得指向业务库。CI 已接入原生结果浏览器报告上传；本批远端 CI 未执行，现有业务 Docker 栈未升级。真实审计效果、供应商账单、SDK 登录和外部发布仍未验收。


## 本批本地验收记录

200 项跨语言合同/回归、131 项 Scanner 测试、38 项 Web 单元测试、6 项 SDK 能力基线检查及 Server/Agent 全回归通过。既有 320 个能力组合检查保留；新增 53 个同 ID 候选的分页、来源隔离和幂等测试通过。

已安装 SDK 0.1.25 的官方 Mock 额外核对实际 Markdown 产物、原生 Scan ID 与父状态。修复 macOS /var 与 /private/var 目录别名造成的误拒绝，仍按真实目录边界、归档引用和哈希校验。API 原生场景在修复后再验收，全部使用合成数据。

17 组真实 Chromium 检查通过，桌面/窄屏截图已查看；源码及构建哈希保存在浏览器报告。4 个 Go module 依赖图与 7 项边界拒绝检查、工作流 actionlint、SDK 7 处受管理补丁只读验证通过。

受保护基线共 53 个文件保持原哈希，包括 33 个历史 Schema、14 个迁移、执行矩阵、输入兼容数据、原 SQLite schema、SDK 清单与依赖锁。验证汇总在 output/playwright/sdk-native/verification.json；该目录是本地合成产物，不提交。远端 CI、业务 Docker 栈升级和真实模型均未执行。
