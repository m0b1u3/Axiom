# 已提交 Git 差异审计基线

固定 SDK：`@openai/codex-security 0.1.25`。用户在 Web 导入、预览、预检、创建和查看扫描；后台保留 Go Server → Go Agent → Node Scanner，以及 HTTP、共享产物和 JSONL。

## Web 工作流

1. 在「新建扫描 → 代码快照 → 导入 Git 提交差异」上传包含完整 `.git` 目录的 `.zip`、`.tar.gz` 或 `.tgz`。仓库可以位于归档根目录，也可以有一层外部目录。
2. 填写 base/head 提交、分支或标签。默认是 `HEAD~1` / `HEAD`。导入时立即解析成完整提交 SHA，以后引用移动不会改变任务目标。
3. 导入会重建干净的、分离 HEAD 的快照并展示差异。此操作不调用模型。上传工作区中的未提交修改不参与扫描。
4. 在文件列表检查增删、修改和重命名，支持分页、方向键、Home/End、原始 Git 补丁和原始目标合同。每页 50 个文件；单个补丁预览最多 256 KiB，截断有明确提示；二进制文件展示元信息。
5. 选择模型、单次上限和可选任务总预算，使用 Standard 创建任务，也可以只执行 SDK 原生预检。Git 目标不叠加路径白名单，不支持 Deep。
6. 执行详情保留同一份差异预览。漏洞证据链显示冻结的 base/head；代码观察只对照 HEAD，旧行与已删除文件不能成为 HEAD 的代码观察。

预检只检查本地目标和配置，费用为已知零，认证始终标记为未验证。独立漏洞复核继续沿用冻结目标与来源证据；SDK 0.1.25 没有该操作的公开费用回调和预算选项，费用保持未知，需在 Web 明确知悉后发起。

## 快照与执行边界

Server 不信任上传的工作区、Git 配置或 hooks。导入先按既有解压规则检查路径、文件数、单文件大小、总容量及压缩比，然后只读取 Git 对象和引用，在新仓库中解析选定提交并检查对象完整性。最终只保留 base/head 可达的历史对象、受控引用、重建的 HEAD 文件及新生成的仓库元数据；原配置、远端凭据、hooks、无关分支和悬空对象不进入冻结快照，也不执行 checkout filters。

冻结身份包括 base/head commit、两棵 tree、完整原始差异 SHA-256、HEAD 源码内容哈希及整个快照内容哈希。已有 `TreeHash` 算法保持原样；新增 `WorktreeHash` 单独绑定 HEAD 源码，不把 `.git` 当作提交源码。原上传包摘要保存在 Git 元数据侧表；资产的 `archiveSha256` 是重建快照的确定性 TAR 摘要，不冒充原上传包摘要。

Agent 在领取时核对执行、快照 ID、内容哈希和目标，在每次执行产物目录内创建独立仓库副本，检查副本后才交给 Scanner。SDK 不直接使用共享冻结目录。Scanner 再核对 commit/tree、HEAD、差异摘要和源码哈希，调用公开 `DiffTarget.refs({base:完整SHA, head:完整SHA})`。原生预检也使用该目标。所有 Git 子进程都隔离环境、配置、hooks 与替换对象；Server 在 Linux 上取消请求时终止 Git 及其帮助进程组。

SDK 原生结果的 `manifest.scan.target` 必须报告相同的 `git_diff` base/head。结果的 `extensions.gitDiff`、每个漏洞的 `extensions.axiomGitTarget` 都保存统一 `GitDiffAssignment`，绑定 task/attempt、快照和 HEAD。Server 入库及图谱查询复核这些绑定；Web 同时拒绝不匹配的预览、文件和证据响应，不回退为全量扫描。

代码片段能在冻结 HEAD 的对应行匹配，才标为「已观察到代码」。这不代表漏洞已验证；SDK 复核结论、模型推断和实际验证仍有独立语义。base 片段不匹配或文件在 HEAD 已删除时，保留原始片段和差异上下文，不能自动切换源码版本制造观察依据。

## 合同和版本

- 新增 `GitDiffTarget`、`GitDiffAssignment`、`GitDiffPreview` 和 `GitDiffContent` 独立 Schema，生成 Go/TS 类型与校验器。
- Git 任务使用 input v4、Job v6、ScannerDescription v7；基础 Assignment、完整结果、进度/事件和控制接口版本保持不变。租约通过现有 HTTP 信封携带 Git 侧带，结果继续使用已有扩展入口。
- 兼容矩阵区分 Git 目标与可选 SDK 操作能力。旧 Scanner 不能领取新 Git 任务，Web 展示等待和缺失能力，不能静默降级。
- 新增迁移 `018_git_diff.sql` 保存快照和任务目标侧表；旧迁移与旧 Schema 不改写，普通任务和历史结果无需回填。
- Server 与 Agent Docker 镜像均包含 Git 运行时，供导入、执行副本与 Scanner 的提交身份校验使用。部署必须同步更新 Server、Agent/Scanner 和 Web，并先备份数据库与共享产物。回滚使用一致备份恢复到独立环境，不把 Git 任务改成普通任务，不让旧组件继续写新 Git 资产。

## 首版范围

仅支持完整 SHA-1 仓库中两个不同的已提交 commit，且 base/head 树都由普通文件组成。浅克隆、部分克隆、外部对象 alternates、外部 worktree、符号链接、子模块、SHA-256 仓库及违反现有安全路径规则的树均拒绝。文件路径需为有效 UTF-8，不能在物化时被规范化成另一条路径。压缩包和重建后的 Git 对象加源码均受现有容量限制；完整差异摘要输入上限为 64 MiB。

两个不同提交可以具有相同源码树，此时展示真实空差异。对非 UTF-8 文件补丁的文字预览会提示替代字符，冻结摘要仍基于原始字节。`DiffTarget.workingTree`、远端拉取、组件审计和 SDK 升级尚未包含；能力清单中的 Git 能力组为 `partial`。

## 验收

共同 Go/TS 样本验证身份、版本、模式、路径、分页、状态与错误组合；Git 仓库测试覆盖未提交内容、packed objects、无关配置/hooks、缺失与损坏历史、链接/子模块、容量、取消和篡改。Agent/Scanner 测试核对私有副本、真实 `DiffTarget.refs`、环境隔离和锁定 SDK 的本地原生预检。

端到端验收使用临时 PostgreSQL、真实 Server/Agent/Scanner 适配器与明确的合成 SDK 替身。浏览器覆盖上传、失败保留、全量分页、键盘、增删/重命名、二进制、大补丁、乱序响应、重试、旧 Server、Web 预检和创建、HEAD/base 证据区分及窄屏布局。既有历史报告、预算、恢复和普通源码输入也纳入回归。

这些验收不调用真实模型，不执行真实审计、真实 SDK 登录或外部发布，不写入现有业务数据库。开发验收产物位于 `output/playwright/git-diff/`；测试入口是 `TestGitDiffPipeline` 和 `code-audit-web/tests/git-diff-web-acceptance.mjs`，已纳入 CI 工作流。
