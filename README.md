# Axiom — AI 源代码安全审计一体机

当前使用 **SDK 0.5.0 / Scanner description 14**。参考 VPS 已部署核心工作流和模型计价，保持 **DeepSeek + Responses + 原生 Schema**。当前功能、部署与验收范围统一见 [当前状态](docs/CURRENT_STATUS.md)；旧 SDK 和早期部署记录保留历史语义。

自托管的 Web 源代码安全审计平台。用户在浏览器配置模型、上传源码、控制扫描和查看证据，后台使用 Go Server + Go Agent + Node Scanner / Codex Security SDK。

源码候选版本：`0.1.0-beta.2`。支持 **Linux amd64、可信用户、自托管**。DeepSeek 小项目已真实跑通扫描、复核、归档和证据图谱；新计价完成隔离测试和部署，真实计费、更多项目和长期稳定性仍待验收。候选整理不调用模型、不代表已公开发布；详见 [候选说明](docs/releases/0.1.0-beta.2.md) 与 [CHANGELOG](CHANGELOG.md)。

[安装、升级与回滚](docs/BETA_INSTALLATION.md) · [部署与备份](deploy/README.md) · [参与贡献](CONTRIBUTING.md) · [私密报告安全问题](SECURITY.md) · [发布流程](docs/RELEASE_PROCESS.md)

当前阶段：Web → Go Server → Go Agent → Node Scanner → Codex Security SDK 的输入、结果和事件链路已打通。支持不可变源码快照、模型配置、预算和范围、结构化进度、完整结果与证据、历史尝试及产物下载。自托管的可靠性与部署工作见 [成熟化验收记录](docs/MATURITY_BASELINE.md)。模型配置、上传、启动、取消、重试和查看结果均在 Web 完成。

高级 SDK 参数与知识文档的固定版本、归档/恢复、任务冻结和父扫描状态已接入 Web，范围与测试见 [扫描输入与知识库基线](docs/SDK_INPUT_KNOWLEDGE.md)。这批完成项不表示 SDK 全部能力已集成；后续范围由 [能力清单](deploy/sdk/integration.json) 记录。

原生报告、仓库历史、结构化诊断与用量估算见 [SDK 原生结果基线](docs/SDK_NATIVE_RESULTS.md)。

漏洞详情支持[单个漏洞证据链图谱](docs/FINDING_EVIDENCE_GRAPH.md)：关联 SDK 证据引用、代码、推断路径和复核依据，区分源码观察与漏洞验证，并保留历史报告和原始对象。

已提交的 [Git base/head 差异审计](docs/SDK_GIT_DIFF.md) 已接入：Web 上传完整仓库归档，冻结提交并预览差异，使用 Standard 调用 SDK refs 目标，证据绑定 HEAD；未提交工作区差异仍待后续接入。

[组件规划与计划管理](docs/SDK_COMPONENT_PLANS.md) 已接入：Web 编辑、SDK 自动建议、本地规范化与全量路径检查、人工确认冻结和历史版本下载。SDK 0.5.0；计划确认不会启动组件扫描，真实规划模型仍未验收。

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

## 模型单价与 Token 费用估算

在 Web「模型与授权」中新建或编辑模型，在「用量计价」启用「按配置单价自动估算费用」。计价可选，DeepSeek 等供应商按具体模型配置手工填写；系统不抓取官网价格。保存单价无需调用真实模型，也不验证所选接口的 Responses / Schema 兼容性，可先使用 Fixture / Mock 隔离验证。

- 币种填写三位大写代码，例如 `USD`、`CNY`。输入、输出与缓存单价均按**百万 Token**填写非负十进制值，最多六位小数；`0` 表示免费，留空表示未知。可选「价格依据」用于记录合同价、折扣或适用时段，不填写凭据。
- 缓存规则可选「全部输入按同一单价」「缓存读取单独计价，写入按普通输入」「缓存读取、写入分别计价」。选择分别计价时，需要对应单价及用量；缺少任一所需项，金额保持未知。

每次执行被领取开始时，冻结与执行的供应商、模型及接口匹配的单价和版本。改价只影响之后开始的执行；删除模型配置也保留已有快照，历史执行不会追溯补价。估算使用实际上报 Token：SDK 累计回调按累计值更新，不重复相加；Responses 复核从实际响应的用量元数据提取。缺失、无效或中断的用量保留为未知或部分费用下界，不按零处理。计算使用十进制有理数，合计后仅向上舍入一次至该币种的百万分之一；推理 Token 已包含在输出用量中，估算时不重复计费。

在执行详情「任务预算与费用」展开各次「按用量估算」，可查看单价快照、实际 Token、来源及完整性，并访问「原始费用快照」。USD 配置估算进入现有 USD 预算记账，其他币种独立展示且不自动换汇；SDK 原始费用另行标注并保留。所有金额均为估算，不代表供应商账单。

填写步骤、公式、用量完整性、单价快照与回退限制见 [模型单价与 Token 费用](docs/MODEL_PRICING.md)。当前参考部署单价未填写，不会追溯重算已有费用。

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

## 历史增量记录

以下条目按当时版本保留；当前运行和验收状态见 [当前状态](docs/CURRENT_STATUS.md)。

2026-10-16：SDK 原生预检、独立复核、完整会话事件及未知费用的 Web 接入见 [SDK 操作基线](docs/SDK_OPERATIONS.md)。保留 SDK 0.1.25、历史读取与原有传输；验收不执行真实模型、SDK 登录或外部发布。

2026-10-16：已核实 SDK 0.1.25 没有跨进程续扫入口。Web 新增过期中断登记、完整提交结果恢复入库与明确确认后的重新执行，保持原执行、费用和历史证据。迁移 017 仅保存恢复来源；既有合同、SDK 锁与七处补丁不变。详情见 [扫描中断恢复](docs/SCAN_RECOVERY.md)。验证只使用本地 Mock / Fixture，业务 Docker 未升级。

2026-10-18：纯 Web SDK 执行身份已接入 API Key、ChatGPT 设备码、账户查询、退出及撤销；任务冻结身份、Agent 和凭据版本，刷新会话私有回存。详见 [SDK 执行身份与会话管理](docs/SDK_EXECUTION_IDENTITIES.md)。SDK 固定 0.1.25，现有 Docker 已同步更新为 `sdk-identities-20261018-01`；备份恢复与部署后浏览器验收见 [部署说明](docs/SDK_IDENTITIES_DEPLOYMENT.md)。
