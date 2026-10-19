# Axiom — AI 源代码安全审计一体机

自托管的 Web 源代码安全审计平台。用户在浏览器配置模型、上传源码、控制扫描和查看证据，后台使用 Go Server + Go Agent + Node Scanner / Codex Security SDK。

当前版本：`0.1.0-beta.1` 发布候选。首版支持 **Linux amd64、可信用户、自托管**。本轮自动验收使用 SDK Fixture 及官方 Mock，真实审计、供应商费用及长期运行验收尚未完成；详见 [CHANGELOG](CHANGELOG.md)。

[安装、升级与回滚](docs/BETA_INSTALLATION.md) · [部署与备份](deploy/README.md) · [参与贡献](CONTRIBUTING.md) · [私密报告安全问题](SECURITY.md) · [发布流程](docs/RELEASE_PROCESS.md)

当前阶段：Web → Go Server → Go Agent → Node Scanner → Codex Security SDK 的输入、结果和事件链路已打通。支持不可变源码快照、模型配置、预算和范围、结构化进度、完整结果与证据、历史尝试及产物下载。自托管的可靠性与部署工作见 [成熟化验收记录](docs/MATURITY_BASELINE.md)。模型配置、上传、启动、取消、重试和查看结果均在 Web 完成。

高级 SDK 参数与知识文档的固定版本、归档/恢复、任务冻结和父扫描状态已接入 Web，范围与测试见 [扫描输入与知识库基线](docs/SDK_INPUT_KNOWLEDGE.md)。这批完成项不表示 SDK 全部能力已集成；后续范围由 [能力清单](deploy/sdk/integration.json) 记录。

原生报告、仓库历史、结构化诊断与用量估算见 [SDK 原生结果基线](docs/SDK_NATIVE_RESULTS.md)。

漏洞详情支持[单个漏洞证据链图谱](docs/FINDING_EVIDENCE_GRAPH.md)：关联 SDK 证据引用、代码、推断路径和复核依据，区分源码观察与漏洞验证，并保留历史报告和原始对象。

已提交的 [Git base/head 差异审计](docs/SDK_GIT_DIFF.md) 已接入：Web 上传完整仓库归档，冻结提交并预览差异，使用 Standard 调用 SDK refs 目标，证据绑定 HEAD；未提交工作区差异仍待后续接入。

[组件规划与计划管理](docs/SDK_COMPONENT_PLANS.md) 已接入：Web 编辑、SDK 自动建议、本地规范化与全量路径检查、人工确认冻结和历史版本下载。固定 SDK 0.1.25；计划确认不会启动组件扫描。

## 仓库结构

```text
Axiom/
├── code-audit-scanner/   # Scanner 合同 + Codex Security 适配（TS，产物落盘）
├── code-audit-server/    # Go HTTP API、业务数据库、迁移、调度与结果索引
├── code-audit-agent/     # Go 扫描运行时（合同领取/续租/上报，无数据库依赖）
├── code-audit-web/       # Vue 3 Web 工作区（项目/扫描/知识/漏洞）
├── contracts/           # 统一 Schema、生成 Go/TS 类型、Go 控制信封及跨语言测试
├── shared/sourceidentity/ # 两端共用的源码树哈希，仅依赖 Go 标准库
├── deploy/              # 开发/正式 Compose、SDK 基线及维护工具
├── test-fixtures/        # M0 漏洞样本
└── docs/                 # M0 清单 + 兼容性矩阵 + Scanner 合同
```

## 快速启动

自托管使用 [Beta 安装指南](docs/BETA_INSTALLATION.md)。扫描操作全部在 Web 完成；安装、备份与恢复命令供管理员维护。下面是贡献者的开发与合成数据演示入口，默认账号仅适用于开发模式：

```bash
# 一键起三件套（server + agent[默认mock] + web），Ctrl-C 一起退出
bash deploy/dev-up.sh

# 全链路演示（独立 demo 库：上传 fixtures → 快照 → 扫描 → mock 完成 → 打印报告地址）
bash deploy/demo.sh
```

手动方式：

```bash
cd code-audit-server && go run ./cmd/server          # 127.0.0.1:8080
cd code-audit-agent && AXIOM_SCAN_MOCK=1 go run ./cmd/agent
cd code-audit-web && npm install && npm run dev      # 127.0.0.1:5173
```

默认账号 `admin` / `admin`。日志位于 `code-audit-agent/data/logs/`。

## Go 模块边界检查

```bash
python3 deploy/check_go_boundaries.py
python3 -m unittest discover -s deploy -p 'check_go_boundaries_test.py' -v
```

检查每个模块的独立构建/测试依赖和完整 module 图。Server 不引用 Agent；Agent 和 contracts 不包含数据库驱动；业务数据、Provider 预设和密钥解密归 Server。旧 SQLite 路径及 `AXIOM_AGENT_DB` 仅作为 Server 的兼容配置保留，不代表 Agent 访问数据库。

## 硬约束

- Codex Security 只允许出现在 `code-audit-scanner`
- `ScanTask` 不携带 API Key；密钥只走环境变量注入
- 扫描必须绑定不可变 Source Snapshot，执行前核对内容哈希
- 生产依赖必须钉版本，禁止 `latest` / `^0.x`

## 开源许可

Axiom 使用 [Apache-2.0](LICENSE)。第三方代码、SDK 补丁、字体与标识保留各自许可和声明，见 [NOTICE](NOTICE)。

2026-10-16：SDK 原生预检、独立复核、完整会话事件及未知费用的 Web 接入见 [SDK 操作基线](docs/SDK_OPERATIONS.md)。保留 SDK 0.1.25、历史读取与原有传输；验收不执行真实模型、SDK 登录或外部发布。

2026-10-16：已核实 SDK 0.1.25 没有跨进程续扫入口。Web 新增过期中断登记、完整提交结果恢复入库与明确确认后的重新执行，保持原执行、费用和历史证据。迁移 017 仅保存恢复来源；既有合同、SDK 锁与七处补丁不变。详情见 [扫描中断恢复](docs/SCAN_RECOVERY.md)。验证只使用本地 Mock / Fixture，业务 Docker 未升级。

2026-10-18：纯 Web SDK 执行身份已接入 API Key、ChatGPT 设备码、账户查询、退出及撤销；任务冻结身份、Agent 和凭据版本，刷新会话私有回存。详见 [SDK 执行身份与会话管理](docs/SDK_EXECUTION_IDENTITIES.md)。SDK 固定 0.1.25，现有 Docker 已同步更新为 `sdk-identities-20261018-01`；备份恢复与部署后浏览器验收见 [部署说明](docs/SDK_IDENTITIES_DEPLOYMENT.md)。
