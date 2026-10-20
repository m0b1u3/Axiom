# 无模型稳定性与发布验收

此验收面向 SDK 0.1.25、Linux amd64、可信用户自托管，保持 Go Server、Go Agent、Node Scanner、静态 Web 与 HTTP / 共享产物 / JSONL 传输。报告只覆盖实际运行的规模与时长，真实模型、真实账户、供应商账单和外部发布继续标记为未验收。

## 隔离范围

验收从全新 `axiom-accept-*` 私有配置创建随机命名的独立项目、数据库、数据卷和临时端口。已有容器与卷会使目标检查失败；稳定性项目使用独立 UUID。脚本拒绝以业务项目配置运行，并检查原有非验收容器的镜像与启动时间未变。

Agent 仅连接内部 Docker 网络，测试注册器将 SDK 模块替换为显式 Fixtures / Mock 实现。镜像仍包含并核对 SDK 0.1.25，Fixture 通过只读挂载注入，正常发布镜像不包含测试注册器。模型配置中的 Key 与控制台账户均为本轮隔离生成或合成，不使用任何已有业务账户、SDK 登录态或模型凭据。`AXIOM_SCAN_MOCK=0` 用于覆盖实际 Scanner 路径，模型调用由模块替换和网络隔离阻断。

主应用、历史演示、报告、私有 `.env` 和 SDK 凭据不挂载到测试环境。结束只清理通过未使用检查的本轮项目；备份、浏览器日志和私有配置保留在权限受限的验收目录，不进入 Git 或公共 CI Artifact。

## 默认稳定性规模

| 场景 | 默认规模与断言 |
| --- | --- |
| 源码归档 | 12,000 个合成源码文件、48 MiB 内容，另加显式 Fixture 清单；ZIP 使用 stored 模式，上传不依赖高压缩比 |
| 不可变源码 | 独立计算每个文件及整个源码树的 SHA-256，Scanner 读取后再次核对 |
| 多任务 | 6 个同模型、同项目任务同时进入 SDK 替身；一个使用大快照，其余使用小快照 |
| 持续执行 | 所有 SDK 替身启动后继续至少 180 秒，跨过 3 个默认 60 秒租约周期；取消其中一个后其余继续 |
| 事件与结果 | 每任务至少 1,000 个原生 SessionEvent，加结构化活动突发与持续事件；归档后逐页检查身份、连续序列、计数、脱敏与费用 |
| 故障 | 实际重启 Server、短暂停止 PostgreSQL、SIGKILL Agent 并等待真实租约自然过期；已展开的 Web 恢复面板自动只读核验并显示过期入口，用户明确登记中断；SDK 调用记录检查每次尝试只执行一次 |
| 费用与恢复 | 中断费用保持未知；Web 核对估算、明确确认重新执行，再创建第 2 次尝试，保留旧结果与费用依据 |
| 资源 | 记录隔离 Server/Agent 的 CPU、内存样本与 API 批次延迟，检查 OOM 和意外重复执行；采样数据不代表容量承诺 |
| Web | 真实静态页面和后端检查并行状态、活动、刷新、取消、报告下载、窄屏，以及未知费用与恢复操作 |

命令在仓库根目录执行，使用全新的路径与明确的候选镜像标签：

```bash
python3 deploy/ops/stack.py init /absolute/private/acceptance.env --project axiom-accept-local --port 0 --release candidate-tag
docker compose --env-file /absolute/private/acceptance.env -f deploy/docker/compose.production.yml build
python3 deploy/ops/stability_acceptance.py --env /absolute/private/acceptance.env --work /absolute/private/stability-run
```

可显式扩大本轮验收规模，例如 `--hold-seconds 600 --files 20000 --source-mib 128 --parallel 12 --session-burst 3000`。报告保留参数与实测时长；测试脚本对自身资源使用设置边界，不影响产品并发或 SDK 参数。未运行的更长时间或更大规模不计为通过。

## 固定基线安装、升级与回滚

`deploy/release/stability-baseline.json` 固定基线提交、源码树 SHA-256、SDK 与镜像标签。目前基线为 `02515c1`。构建工具从 Git 导出该提交，按源码交付规则过滤并核对整个源码树，再构建 Linux amd64 Server、Agent/Scanner、Web 镜像。工作目录须尚不存在；不会复制现有安装或备份作为源码。

```bash
python3 deploy/ops/build_stability_baseline.py --work /absolute/private/baseline-build
python3 deploy/ops/stack.py retarget /absolute/private/acceptance.env /absolute/private/baseline.env --release baseline-02515c1
python3 deploy/ops/acceptance.py --env /absolute/private/baseline.env --candidate-env /absolute/private/acceptance.env --work /absolute/private/upgrade-run
```

测试先安装固定基线，写入合成任务、历史结果、未知费用、清理意图及权限为 0700/0600 的私有 SDK 状态标记。队列空闲后一致备份，按 Server、Agent、Web 的顺序切换候选镜像，核对历史数据并执行新的合成任务。回滚将升级前备份恢复到另一个新项目，核对原镜像 ID、历史数据、模型凭据与 SDK 私有文件的字节、属主、权限，确认新版本专有任务不出现在旧恢复点，旧会话已撤销。

该项是固定内部提交之间的实际镜像升级。首版仍没有前一公开 Release；它与冻结迁移 001–012 到当前 Schema 的测试分别记录，不能称为前一正式版本的升级认证。

## CI 与通过边界

`Production deployment baseline` 在原生 Linux amd64 runner 上从本提交的源码交付目录构建候选，重新检出固定基线，并运行静态代理、内部镜像升级回滚、默认稳定性规模和冻结迁移验收。其他合同、Go 边界、Web 综合回归、源码交付工作流继续保留，发布检查要求同一个完整提交全部通过。

公共 Artifact 只包含源码清单、通过摘要、资源样本与模拟界面截图。私有环境、数据库、备份、浏览器存储与运行日志不上传。`report.json` 的稳定性通过状态在项目清理和原部署保护检查成功后写入。

本验收提供有明确边界的工程证据。多日持续运行、真实模型行为与费用、真实身份授权及其他主机环境继续单独记录为待验证；测试通过不自动发布 Release、标签、镜像或更新现有业务部署。
