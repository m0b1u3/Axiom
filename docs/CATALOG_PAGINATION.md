# 资产与漏洞查询基线

2026-10-15。沿用 Go Server、Go Agent、Node Scanner / SDK，以及现有 HTTP、共享产物目录和 JSONL。本次只调整 Server 查询和 Web 消费方式，不启动真实审计任务，不增加数据库迁移。

## HTTP 兼容性

`GET /api/v1/snapshots` 与 `GET /api/v1/findings` 均沿用认证、权限和 `{data: ...}` 信封。在任一接口中指定 `page` 或 `pageSize` 即启用分页：

```json
{"data":{"items":[],"total":0,"page":1,"pageSize":50}}
```

漏洞响应另含 `counts`，键为 `""`（全部）、`critical`、`high`、`medium`、`low`、`info`；历史未知严重度仍计入全部且保留原值。数量来自全量匹配记录，与当前页行数无关。

| 参数 | 行为 |
| --- | --- |
| `page` | 正整数，默认 1；超出最后一页时回到最后一页；空结果为第 1 页 |
| `pageSize` | 1–100，默认 50；不设置总量截断 |
| `q` | 最多 256 个字符，去除首尾空白，按不区分大小写的字面子串搜索；`%`、`_`、`!` 不作为通配符 |
| `projectId` | 省略表示全部项目；分页模式下显式 `0` 表示未归属项目；正整数筛选指定项目 |
| `sort`、`order` | 使用下表的字段白名单，方向 `asc` / `desc`；相同值以 ID 倒序确定顺序 |

每次分页响应在同一个只读、可重复读事务中计算总数、严重度数量、页码和记录。页面之间并不冻结数据：并发上传、删除和状态更新会改变后续查询结果；刷新会读取最新数据。SQL 值均参数化，重复参数、无效数字/枚举、超长搜索词及不支持的排序返回 400，不静默扩大查询范围。

| 接口 | 搜索字段 | 附加筛选 | 排序字段与默认值 |
| --- | --- | --- | --- |
| 快照 | 原始文件名、归档哈希、内容哈希、源码路径、项目名 | `usage=referenced/unused`：存在/不存在扫描引用；`id`：精确快照 ID | `id`（默认倒序）、`originalFilename`、`archiveSha256`、`fileCount`、`totalSize`、`scanCount`、`createdAt` |
| 漏洞 | 标题、文件路径、分类、指纹、CWE | `severity`、`status=open/confirmed/false_positive/fixed`、`sourceSnapshotId`、`sourcePath` | `severity`（默认 Critical → Info）、`title`、`filePath`、`status`、`lastSeenAt` |

`sourcePath` 保留历史的区分大小写子串匹配。`sourceSnapshotId=0` 保留“不限定快照”的含义。漏洞先从 completed/partial 任务中按指纹选择最新 occurrence，再按该 occurrence 的项目和来源过滤；不会因为筛选而复活旧 occurrence。首次出现记录仍来自历史最早 occurrence，最近的非 open 人工处理状态仍跨 occurrence 继承。证据、Raw JSON 和单次扫描的漏洞/报告读取逻辑不变。

不传分页参数时，旧客户端继续得到数组：快照最多 200 项，漏洞池最多 300 项；原有快照 `projectId=0` 仍表示全部项目。旧漏洞数组的搜索/筛选改在截断前执行，修复旧记录被提前截断的问题。分页能力不依赖 Agent、Scanner 或 SDK 升级；旧 Web 可继续调用原接口，新 Web 应与本次 Server 一起部署。不存在自动回退到截断数组的分支。

## Web 行为

- 资产、漏洞、项目快照列表每页 50 条，提供首页、上一页、跳转、下一页和末页；排序、搜索、组合筛选均在 Server 执行。
- 资产支持项目/未归属、扫描引用筛选；漏洞支持严重度、处理状态、项目和 URL 中的来源范围。严重度数量遵循搜索、状态、项目和来源，但不受已选严重度限制。
- 列表条件进入 URL，刷新和浏览器前进/后退可还原。输入防抖 250 ms，旧响应不能覆盖新查询；条件变化回到第一页。
- 读取失败保留最后成功的记录、总数和页码，明确标为旧结果；初次读取失败显示未知数量，不显示零。删除后重新查询，最后一页消失时回到有效页码。
- 新建扫描的源码选择器可搜索、分页，并独立读取链接或已选定的历史快照；搜索/翻页保留已选源码。失效源码可明确清除并重选。项目关联选择器对全量未归属资产执行查询。
- 工作空间搜索向 Server 查询漏洞和资产，并提供全量结果入口。列表沿用现有表格、选择器、清理预览、Token、键盘和只读权限规则。

## 验证与复现

Store 测试覆盖 SQLite 历史读取与 PostgreSQL：237 个快照、357 个去重漏洞、多次 occurrence、失败任务、人工状态继承、中文/重音字符、字面通配符、CWE、完整遍历、引用过滤、排序和页码边界。HTTP/浏览器使用另一组 251 个快照、401 个去重漏洞和 2 条历史任务，不启动 Agent、Scanner、SDK 或真实模型。

先运行 `npm run build`、`npm test`（在 `code-audit-web`）；然后在 `code-audit-server` 指定独立本机 PostgreSQL：

```sh
AXIOM_TEST_DATABASE_URL='postgres://postgres:fixture-password@127.0.0.1:5432/axiom_test_input_catalog?sslmode=disable' \
AXIOM_TEST_WEB_INPUT=1 \
AXIOM_TEST_CATALOG_REPORT_DIR="$PWD/../output/playwright/catalog" \
go test -p 1 -count=1 -timeout=8m -run 'TestCatalog' ./internal/store ./internal/httpserver
```

测试会清空该专用数据库，禁止使用业务数据库。浏览器只连接隔离 Server；真实模型调用为 0，检查不创建扫描任务、不走旧直接删除接口、不回退旧数组查询。报告记录生产构建和测试脚本 SHA-256、实际执行场景与截图；失败时写入 failed 报告。CI 接入现有 `input-chain.yml`，公开产物仅包含合成数据。

本地验证：Web 30 项单元/API合同测试、Server 全包回归、原 25 组逐页浏览器场景、新增 16 组分页场景通过。分页截图 12 张，覆盖桌面、390px 窄屏、深色，已复核；旧 Server 数组响应和失效源码重选均有实际浏览器回归。42 个既有合同/兼容矩阵/迁移文件保持原哈希，Go 依赖边界无违规。CI 工作流已接入；本轮未推送，不能认定远端已验收。

分页避免把所有记录载入浏览器，不代表已认证百万记录、真实大型证据和长时间并发负载；这些性能与真实 SDK 验收仍独立保留。
