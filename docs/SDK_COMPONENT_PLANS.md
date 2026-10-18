# SDK 0.1.25 组件规划与计划管理

组件计划管理位于 Web 的「组件计划」工作区，也可从资产或新建扫描页面进入。源码必须先上传为不可变快照。可手工编辑组件表格或 JSON，保存草稿，执行本地路径校验，再确认冻结版本。确认冻结本身不会启动组件扫描。打开冻结版本后，可通过独立的 [组件审计执行](SDK_COMPONENT_SCANS.md) 配置并明确启动 `runComponentScans`。

## 两种操作

| Web 操作 | SDK 公开 API | 模型与费用 | 结果如何生效 |
|---|---|---|---|
| 规范化并检查覆盖 | `normalizeComponentPlan(repository, value, signal)` | 本地执行，不调用模型，完成结果费用为已知 $0 | 检查当前草稿；没有重叠、遗漏、空组件或重名后，可人工确认 |
| 生成组件建议 | `planComponents(repository, {config, environment, signal})`，再规范化 | 会调用所选模型；0.1.25 没有费用回调或费用上限，记录为未知 | 建议不会自动覆盖草稿。人工采用生成新修订，审阅并重新校验后才能确认 |

自动建议需要在 Web 选择模型并明确确认未知费用。模型连接与凭据沿用已有的冻结配置和 Agent 凭据投影。合成测试替换模型规划函数；模拟 Agent 不会调用真实规划函数，自动建议会明确失败并提示手工编辑。实际本地规范化测试使用锁定 SDK 的公开函数。

## 合同和版本

统一定义位于 `contracts/schemas/v1/component-planning.schema.json`，Go/TS 类型和运行时校验由原有生成器产出。核心对象为 `ComponentSource`、`ComponentPlanDocument`、`ComponentPlanCoverage`、`ComponentPlanningOperation`、`ComponentPlanningResult`、`ComponentPlanSnapshot`、`ComponentPlanVersion`。

新增业务输入版本 5 / ScannerJob 7 / ScannerDescription 8；控制 API 仍为 4，结果 1、进度 2、事件 2。使用原有 HTTP 租约、共享文件产物和 JSONL 事件，不引入新传输协议。缺少 Job 7 的节点不能领取计划操作，已有扫描任务仍按原合同运行、读取。

每个操作包含执行身份、计划 ID、草稿修订、快照 ID、完整源码 SHA-256，以及可选冻结 Git HEAD。普通源码与 Git 快照均可规划；Git 组件计划针对冻结 HEAD 的**完整源码**，不继承 base/head 的差异范围。

Server 新增迁移 `019_component_plans.sql`，保存计划、编辑日志、冻结版本和操作关联。旧迁移与历史合同保持不变。升级应同步发布 Server、Agent、Scanner、Web。按既有流程备份数据库和共享产物；使用旧 Server 镜像回退时保留新增表，不删除迁移或历史数据。

## 不可变绑定和确认规则

1. 创建计划时 Server 重新计算源码快照哈希，并绑定原快照及 Git HEAD。
2. Agent 为每次规划/规范化复制独立源码目录，拒绝链接或特殊文件，并校验完整哈希。SDK 不直接获得共享快照作为可写工作目录。
3. Scanner 在 SDK 前后核对完整哈希；原生输入、规范化文档、覆盖结果及错误保存为执行产物。
4. Server 接收时验证租约、执行与操作身份、成本语义和原始草稿；独立重算源码哈希及覆盖结果，不信任客户端提供的 `valid` 标记。
5. 确认要求当前修订的成功本地校验、指定执行及结果摘要一致。Server 在冻结前再次检查源码和覆盖结果。
6. 冻结版本保存规范化文档、源码身份、覆盖证据、原始编辑来源、确认人/时间、校验执行和结果/版本摘要。同一修订重复确认返回同一版本。
7. 后续编辑仅增加草稿修订；旧执行的迟到结果仍保留，但不能覆盖或冻结新修订。已有冻结版本始终可读取、下载。

## 路径、覆盖与容量

路径为规范相对路径，根目录写 `.`；禁止绝对路径、上级路径、反斜线、盘符及 `.git` 元数据。名称由 SDK 去除首尾空白，组件内部完全相同的路径由 SDK 去重，祖先/后代路径仍视为重叠。Go 名称处理与 JavaScript `trim` 的 Unicode 空白规则一致。

覆盖清单包含全部普通源码文件，包括隐藏文件、Git 忽略文件；排除 `.git` 元数据。目录匹配遵循路径段边界，`src` 不匹配 `src2`。空组件、名称重复、任何路径重叠和任何未覆盖文件都会阻止冻结。SDK 建议不能静默把遗漏源码视为已经覆盖。

每份计划限制 128 个组件、每组件 64 个路径，Web 请求最大 1 MiB；快照沿用 20 万文件、单文件 100 MiB、总量 10 GiB 的边界。覆盖结果保留完整遗漏清单，Web 每页 50 条；重叠展示前 200 处并保留总数。文件顺序按 UTF-8 字节排序，清单摘要在 Go/TS 一致；超限返回明确失败，不截断后声称完整。

## 中断、历史与存储

可在 Web 取消操作。节点租约失效后计划操作进入失败状态，禁止自动重新调用模型，也不能从扫描「重新执行」入口重放；用户应在计划页面创建新操作。自动建议没有可靠费用终值时始终保留未知，不转成零。已有人工费用核对流程不会改写 SDK 原始未知费用。

计划引用的源码和操作证据受到存储清理保护；已冻结版本依赖的验证结果不能通过直接删除扫描接口移除。计划操作不写漏洞，不生成扫描报告，也不混入审计数量和扫描比较。

## API

所有接口沿用会话认证与审计。查看者可读取计划和版本；操作权限才可创建、修改、规划、采用和确认。

| 方法与路径（均以 `/api/v1` 开始） | 作用 |
|---|---|
| `GET /component-plans?snapshotId=…&page=…` | 按源码分页读取计划 |
| `POST /component-plans` | 创建绑定快照的手工草稿 |
| `GET /component-plans/{id}` | 当前草稿、最近操作、原始结果及摘要 |
| `PUT /component-plans/{id}` | 以 `revision` 乐观锁保存新修订 |
| `POST /component-plans/{id}/operations` | 创建 `normalize` 或 `suggest` 操作 |
| `POST /component-plans/{id}/apply-suggestion` | 用修订、执行与结果摘要显式采用建议 |
| `POST /component-plans/{id}/freeze` | 用修订、执行与结果摘要确认冻结 |
| `GET /component-plans/{id}/versions?page=…` | 冻结版本分页 |
| `GET /component-plans/{id}/versions/{version}` | 校验摘要后读取指定冻结版本 |

## 测试入口

- `contracts`: `npm test`，包括新增计划合同的 Go/TS 双向往返、执行/源码混淆、费用、覆盖和新旧版本组合。
- `shared/sourceidentity`: `go test ./...`，覆盖路径边界、遗漏、重叠、隐藏/忽略文件、Unicode 排序及异常源码。
- Scanner: `npm test`，真实 SDK 本地规范化、哈希变化和模拟模式禁止真实规划。
- Agent: `go test ./...`，独立源码副本与描述版本兼容。
- Web: `npm test`，当前草稿冻结条件和原始合同保护。
- Server: 在专用 `axiom_test_input…` PostgreSQL 中运行 `go test ./internal/httpserver -run '^TestComponentPlansPipeline$' -count=1 -v`。
- 加 `AXIOM_TEST_WEB_COMPONENTS=1` 执行真实浏览器验收。需先构建 Scanner 和 Web；浏览器连接测试创建的回环 Server，自动规划使用合成 SDK，`normalizeComponentPlan` 使用锁定 SDK。CI `input-chain.yml` 已包含该入口。

本轮验收不使用现有业务数据库，不执行真实规划模型、真实审计、真实 SDK 登录或外部发布。浏览器记录与截图位于 `output/playwright/component-planning/`。
