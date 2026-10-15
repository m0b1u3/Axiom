# SDK 修改盘点与重建基线

本基线从原开发 VPS 的私有安装目录 的实际安装文件提取，逐文件对比同版本官方 npm 发布包，并校验发布包的 SHA-512 integrity。范围是 Linux x64 扫描运行环境；没有将未经验证的 Windows/macOS 行为标为已验收。

## 已确认的修改

| 位置 | 修改 | 原先保存位置 |
| --- | --- | --- |
| SDK `dist/config.js` | `features.multi_agent_v2` 改为 `multi_agent: true`，保留当前第三方 Responses 模型的 V1 兼容策略 | node_modules |
| 插件 `references/core-scan.md` | baseline/investigator 使用 `fork_context: false` | 用户插件缓存 |
| 插件 `references/threat-model.md` | 独立架构评审使用 V1 参数 | 用户插件缓存 |
| 插件 `skills/fix-finding/SKILL.md` | 调查和补丁评审使用 V1 参数 | 用户插件缓存 |
| 插件 `skills/security-scan/references/scan-artifacts-and-ledger.md` | 工作列表的调度说明改为 V1 参数 | 用户插件缓存 |

应用层 `src/provider/model-provider.ts` 的 `multi_agent_version: "v1"` 是配套配置，不属于 npm SDK 文件修改。源码还包含第三方 Provider 接入、Chat/Responses 转换、日志处理等业务适配；这些与 SDK 补丁一起纳入源码基线。

四个插件文档原本只修改了缓存，自带 `_bundled_plugin` 仍为原版。新扫描采用独立 SDK 状态目录时会从自带插件建立新缓存，因此单纯备份旧缓存不足以还原行为。安装流程现在把相同修改应用到自带插件，供新运行目录复制。

`deploy/patch-codex-runtime.mjs` 是已有的 Windows spawn 补丁脚本，但盘点时 VPS 的 `dist/runtime.js` 与原版一致。Linux 重建不自动应用该 Windows 补丁；Windows 发布需要单独验收。

## 固定版本与实际解析

- 验证环境：Ubuntu 24.04，Python 3.12.3，Git 2.43.0。
- Node.js：22.23.2；npm：10.9.8。
- Chat 接口适配使用的 `undici`：8.10.2。干净安装发现原项目遗漏了这一直接依赖；现已按开发机实际版本补入 package.json 与锁文件，并与官方包逐文件比对。
- `@openai/codex-security`：0.1.25；内置插件：0.1.94。
- `@openai/codex-sdk` 与 CLI 包：0.149.1。
- 根目录额外安装的 Linux Codex：0.152.1-linux-x64，与官方发布包一致。
- **SDK 实际默认解析到嵌套的 0.149.1-linux-x64**，不是根目录额外安装的 0.152.1。嵌套二进制也与官方发布包一致。本次保留此行为，不混入运行时升级。
- 深度扫描不传 worker 覆盖，沿用插件默认 4；V1 子代理协议、深度扫描 discovery worker 数、Axiom 多任务并发上限是不同参数。

`deploy/sdk/manifest.json` 保存包版本、官方下载地址、integrity、文件哈希和补丁前后哈希。`inventory.json` 保存原始差异清单；`patches/` 供代码评审；`overrides/` 是经过哈希验证的替换文件。

## 安装与验证

必须保留仓库的 `deploy/sdk` 与 `code-audit-scanner` 相对位置。不要只复制 scanner 的 package.json。

```sh
cd code-audit-scanner
npm ci --no-audit --no-fund
npm run verify:sdk
npm run test:sdk-patches
npm run typecheck
npm test
npm run build
```

`postinstall` 自动应用补丁。它先校验所有目标文件，再写入；只接受原版或预期已修改版本。未知修改、缺少文件、多出未管理文件、补丁校验失败均会中止。`verify:sdk` 为只读检查。重复应用不会改变已正确应用的文件。

任何新 SDK 修改都应进入补丁清单并重新验证，不再把直接编辑 node_modules 当作交付方式。

## 干净环境重建

```sh
node deploy/sdk/rebuild.mjs --output /absolute/path/to/new-directory
```

输出目录必须不存在。脚本复制源码、锁文件、补丁和测试样例，使用空 HOME、空 npm 缓存与隔离临时目录；不继承 API Key、数据库连接和 Agent 凭据，也不复用现有 node_modules、SDK 工作台或插件缓存。

依次执行锁文件安装、全文件校验、补丁测试、TypeScript 检查、扫描器测试、构建和 SDK 模拟扫描。运行验证还检查 V1 配置、四份插件文档、默认深度 worker 数、实际 Codex 版本，以及新缓存中真正安装的插件内容。成功后写入 `REBUILD_OK.json`。重复验证可显式传 `--cache /path/to/npm-cache` 复用官方包下载缓存，仍使用全新 HOME 和 node_modules，npm integrity 与完整文件哈希继续校验；验收记录会说明是否使用缓存。

此验证不调用付费模型，不证明第三方模型在真实服务上的完整审计质量，也不代替 Windows/macOS 验收。当前仍需要网络获取锁定的 npm 包；离线依赖仓库和客户安装包属于后续发布阶段。

## 备份与升级

本次原始依赖与旧插件缓存已备份到 原开发环境的私有备份目录（不随源码发布），包含修改前原样安装树、缓存、官方包与差异报告。不要把密钥、auth.json、业务数据库或运行日志写入源码仓库。

升级 SDK 时应另建分支：比较上游变化、重新移植仍有必要的补丁、更新 manifest 与锁文件、完成独立重建后再切换。不要静默跳过补丁失败，也不要用更高版本覆盖当前可用基线。

## 2026-09-10：深度扫描凭据转发修复

真实并发演练发现，第 27 个隔离任务的主协调器可以使用 DeepSeek，而 4 个内部 worker 连续报 `Missing environment variable: DEEPSEEK_API_KEY`。原因是 bundled plugin 的 `.mcp.json` 只转发上游已有供应商凭据名，没有转发 Axiom 使用的第三方凭据名。

新增第 6 处固定补丁：`_bundled_plugin/.mcp.json` 的 `env_vars` 加入 `DEEPSEEK_API_KEY`、`DASHSCOPE_API_KEY`、`MOONSHOT_API_KEY`、`ZHIPU_API_KEY`、`CUSTOM_API_KEY`。只写变量名，不写任何密钥值。已有 V1 配置与 4 处文档补丁保留，内部 worker 数仍为默认 4。

`verify-runtime.mjs` 同时检查 bundled 配置和一次全新插件安装后的配置。完整隔离重建路径为 `/opt/Axiom-rebuild-task-management`，扫描器回归测试增加 worker 失败日志检查。SDK 原始审计报告仍是历史基线，新增变更由 manifest、override 与第 6 个 diff 单独记录。

应用层同时修复 `task_complete.error` 被会话过滤器丢弃的问题：错误以 error 级别进入活动流，保留线程/worker 标识并脱敏，即使关闭推理日志也会保留错误。此次问题不会再只表现为反复“会话开始”。
