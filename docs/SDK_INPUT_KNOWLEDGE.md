# 扫描输入与知识库接入基线

2026-10-15。锁定 `@openai/codex-security 0.1.25` 和现有受管理补丁。保留 Go Server + Go Agent + Node Scanner、HTTP、共享产物与 JSONL；所有业务操作由 Web 完成。本批不执行真实审计、账户登录或外部发布。

## Web 配置与 SDK 对应

| Web 配置 | SDK 公开参数 | 产品约束 |
| --- | --- | --- |
| 每个 Worker 的子代理数量 | `subagents` | 仅 Deep，0–64；留空沿用 SDK，显式 0 保留。与 `workers` 分开。 |
| 补充扫描指令 | `scanPrompt` | 追加到既有范围、重点与执行边界指令，最多 16000 字符。 |
| 自定义验证指令 | `validationPrompt` | 仅 Standard/default；与加强可利用性模板互斥，最多 16000 字符。 |
| 扫描后处理指令 | `postScanPrompt` | 在同一次 SDK 执行中处理，受该次时限和费用上限约束，最多 16000 字符。 |
| SDK 工作流 ID | `workflowId` | 最多 128 个 ASCII 标识字符；父任务已完成的同名工作流不能复用为新扫描。 |
| 隐私标识 | `safetyIdentifier` | 最多 64 个 ASCII 标识字符，页面提示使用稳定匿名标识。 |
| 父 Web 扫描任务 | `parentScanId` | Server 从有效父执行解析原生 ID；浏览器不能只提交任意原生 ID。 |
| 预期插件版本 | `expectedPluginVersion` | 明确版本号；不匹配由 SDK 拒绝，不静默忽略。 |
| SDK 严重程度阈值 | `failureSeverity` | `critical/high/medium/low/informational`；不改写漏洞人工状态。 |
| 固定版本知识文档 | `knowledgeBasePaths` | Server 冻结内容，Agent 私有物化，Scanner 核验后传入受控路径。 |

既有模型、扫描范围、Worker、轮次、停止条件、时限、单次费用上限和任务总预算继续使用原链路。重试保留冻结输入及知识版本，有限总预算仍按剩余额度下发。父扫描关联是新任务，使用自己的预算，不表示恢复原会话。

## 知识文档与历史保护

知识文档页面采用列表与 Inspector。支持新建、上传 `.md/.txt`、搜索、分页、原始正文、版本历史、编辑为新版本、归档与恢复；viewer 可读、operator/admin 可操作。保存失败和版本冲突保留编辑内容。

每份文档必须是非空 UTF-8 文本，不含 NUL，最多 65536 字节。每次扫描最多引用 8 个不重复的版本记录。历史版本也可选择；从历史版本编辑会以读取时的最新版本号做并发检查，生成新的最新版本，不覆盖旧正文。

扫描创建请求只接受 `knowledgeVersionIds`，不接受文档正文或宿主机路径。Server 在插入任务的同一事务中锁定并核对文档，冻结标题、版本记录 ID、正文与 SHA-256。编辑或归档不改变已有任务；归档阻止新的引用，已有任务执行、重试和原始输入读取继续可用。默认不删除知识历史。

Agent 使用 `runtime/knowledge/<versionId>.md` 和私有权限物化，禁止覆盖。Scanner 检查目录/文件不是符号链接、文件类型、UTF-8 内容、字节上限和哈希后才调用 SDK。执行详情展示冻结参数、原始文档、哈希和原始合同，并链接回文档版本。

## 父扫描状态

SDK 0.1.25 的 `parentScanId` 要求工作台中存在对应扫描。每次 Axiom 执行使用独立状态目录，因此不能只把 ID 转发给空的工作台。

Scanner 在 SDK 关闭、结果及提交标记写入后，通过 SQLite `VACUUM INTO` 生成一致性工作台副本，包含已提交的 WAL 数据。只复制 `workbench.sqlite3`，不读取或修改 SDK 表，不复制 `codex-home`、登录文件或 API Key。该版本适配局限于 Scanner。

私有 `normalized/parent-state.json` 绑定 SDK 0.1.25、父 execution、原生 Scan ID、源码哈希、结果哈希及最多 8 MiB 的不透明 SQLite。公开产物清单及任务响应不包含该副本。父任务选择接口仅返回任务/尝试、源码快照、SDK 版本和原生 ID。

Server 要求父任务已完成或部分完成、有完整归档结果、同一不可变源码快照，且私有状态、结果与提交标记均通过身份和哈希校验。创建时将不透明副本冻结到新任务，随后不依赖父任务文件是否仍存在。Agent 透传生成合同；Scanner 只恢复到新的隔离工作台，凭据目录仍独立。缺失、过大、损坏、版本/源码不匹配的状态在模型启动前拒绝。

## 合同、迁移与部署

| 对象 | 版本 |
| --- | --- |
| 普通任务输入/结果/观察 | 1 / 1 / 1，保持原语义 |
| 高级输入、知识文档或父扫描任务 | 2 / 1 / 1 |
| 领取合同 | base `ExecutionAssignment` v1 + `ScanInputAssignment` v2，绑定同一 execution |
| 独立数据 | `ScanInput` v1、`SDKParentState` v1 |
| Scanner 进程输入/描述 | Job v4 / Description v5 |
| 传输 | 控制 API v4、现有共享文件与 JSONL |

`contracts/data/scan-input-compatibility.json` 生成 Go/TS 的版本与大小常量；执行兼容矩阵追加 `scan_input` profile，原四项保持原值。旧能力节点不能领取输入版本 2；矩阵摘要不一致会阻止执行，需同步配套组件。禁止降级丢弃参数。

PostgreSQL 迁移 `014_scan_input_knowledge.sql` 追加知识文档、不可变版本、任务冻结输入及私有父状态表，并允许新输入版本。001–013、历史 Schema 和原 SQLite schema 不改写；SQLite 历史导入继续有效。新增数据由现有数据库备份覆盖，父快照也保留在产物备份内。Server/迁移 → 配套 Agent/Scanner → Web 的顺序不变；旧组件回滚不能执行新输入任务，不做有损向下迁移。

## 自动验收入口

合同：`contracts/fixtures/v1/` 及 Go/TS 双向 codec；覆盖缺失/零值、未知字段、未来版本、非法模式、知识内容和父状态。

服务端与跨进程：`knowledge_test.go`、`knowledge_request_test.go`、`knowledge_pipeline_test.go`。使用独立 PostgreSQL 和实际 Server/Agent/Scanner，在 SDK 实例边界替换合成实现。检查旧节点拒绝、完整参数映射、冻结文档/父状态、父证据损坏、权限、并发更新及剩余预算重试。

Scanner 另以已安装的 SDK 0.1.25 官方 `mock:true` 调用公开 `run()`，验证知识文档、父扫描副本和工作流可执行；确认认证文件未被复制，篡改被拒绝。没有模型、真实账户或供应商调用。

浏览器：`knowledge-web-acceptance.mjs` 使用静态生产构建、隔离 Server/数据库和 Chromium，拒绝站外请求。覆盖上传/历史版本、保存故障保留草稿、归档/恢复、viewer、历史引用、高级输入校验、父选择竞态、冻结输入与桌面/窄屏截图。

开发者在构建 Scanner/Web 后，可在 Server 目录使用专用测试库运行：

```sh
AXIOM_TEST_DATABASE_URL='postgres://postgres:fixture-password@127.0.0.1:5432/axiom_test_input_knowledge?sslmode=disable' \
AXIOM_TEST_WEB_INPUT=1 \
AXIOM_TEST_KNOWLEDGE_REPORT_DIR="$PWD/../output/playwright/knowledge" \
go test -p 1 -count=1 -timeout=12m ./...
```

测试会清空该本机 `axiom_test_input*` 专用库，不得指向业务库。CI 已包含合成流水线与报告上传；本批远端 CI 未执行，现有业务 Docker 栈未升级。真实模型效果、供应商费用、大仓库和长期运行继续待验收。

本批没有完成 SDK 全部能力：独立预检/复核、Git/组件审计、ChatGPT 身份、语义去重与外部集成仍按 [SDK 能力清单](../deploy/sdk/integration.json) 继续实施。


## 本批本地验收记录

- 186 项跨语言共同合同/回归（181 个样本及 5 个回归）；Go 另检查 320 个已知任务/能力/预算组合与未知组合拒绝。
- 126 项 Scanner 测试、34 项 Web 单元测试、6 项 SDK 能力基线检查通过；受管理的 7 处 SDK 补丁只读核验通过。
- Server、Agent 全回归通过，包含 PostgreSQL、历史 SQLite、故障与现有浏览器场景；父证据四类篡改拒绝、冻结副本独立性和 $3 总预算下剩余 $1.875 重试通过。
- 本批新增 14 组浏览器检查与 6 张桌面/窄屏截图通过，已人工查看；执行详情包含原始冻结输入和原文。
- 4 个 Go module 依赖图及 7 项边界拒绝测试通过，Agent 没有新增数据库依赖；工作流 actionlint 通过。
- 42 份受保护基线中 41 份哈希保持原值；唯一预期改动是兼容矩阵追加新 profile，原四项及其他矩阵字段保持原值。

本地证据位于 `output/playwright/knowledge/verification.json`，记录生产构建、测试源码、截图及受保护文件的 SHA-256。上述结论适用于本批合成验收，不表示已经运行真实审计或通过远端 CI。

后续原生结果/导出/诊断与用量估算已在独立批次完成合成接入，见 [原生结果基线](SDK_NATIVE_RESULTS.md)。
