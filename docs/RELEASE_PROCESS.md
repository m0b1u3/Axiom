# Beta 发布流程

首个发布候选为 `0.1.0-beta.1`，唯一发布版本来源为根目录 `VERSION`；`deploy/release/release.json` 必须匹配。npm 私有包版本、Schema 版本及兼容矩阵分别管理，不能用发布版本替代合同协商。

## 支持边界

Linux amd64、可信用户、自托管。Web 是用户操作入口；Go Server、Go Agent、Node Scanner / SDK 和现有传输方式保持。当前真实 SDK 验收为 `not-run`，CI 禁止使用真实模型凭据。候选不能宣称真实审计、供应商账单或长期稳定性已经通过。

## 候选检查与源码交付

```bash
python3 deploy/release/check.py
python3 -m unittest discover -s deploy/release -p '*_test.py' -v
python3 deploy/release/source.py export --output /absolute/private/candidate-source --archive /absolute/private/Axiom-source.tar.gz
python3 deploy/release/source.py verify /absolute/private/candidate-source
```

导出只选择源码目录，排除本地 `release/` 安装副本、node_modules、构建产物、运行数据、备份、配置、密钥及 SDK 登录状态；保留 `contracts/data` 和受控 SDK override。遇到源码链接或特殊文件则拒绝。清单逐文件记录路径、大小、权限及 SHA-256，额外或缺失文件也会使校验失败；清单 SHA 不能代替独立确认的发布来源。

归档固定顺序、权限和时间戳；相同源码与 revision 产生相同归档。不要把备份当作发布包。公开前检查导出文件与变更，第三方来源见 `NOTICE`、`deploy/sdk/UPSTREAM_LICENSE`、锁文件和固定补丁清单。源码导出检查不能代替人工检查文件内容中是否有私人数据。

## 远端 CI

五个工作流均可由 push、Pull Request 或手动运行触发；仅使用只读仓库权限及固定 Action SHA，不注入模型或业务密钥。

| 工作流 | 发布证据 |
| --- | --- |
| `contracts.yml` | Go/TS 生成结果、共同样例和 Node 版本组合 |
| `go-boundaries.yml` | 独立 Go 模块构建、数据库依赖边界 |
| `input-chain.yml` | PostgreSQL、Go/TS、真实浏览器输入/结果/进度/权限/预算/存储回归 |
| `production-stack.yml` | 从源码导出目录构建正式镜像，固定内部基线镜像升级与回滚、大源码与并行长任务、备份恢复、冻结迁移升级 |
| `release-candidate.yml` | 许可证和发布边界、源码包与校验清单、源码交付拒绝保护 |

发布前对**同一个完整提交 SHA** 检查全部工作流。失败的工作流修复后重新运行，不能用另一提交的成功记录替代。准备好 `gh` 认证后执行：

```bash
python3 deploy/release/check.py --remote --revision FULL_COMMIT_SHA --report /absolute/private/beta-readiness.json
```

检查同时确认 GitHub 私密漏洞报告已启用，并将本地交付源码的 Git blob 与权限逐项对应到该提交，拒绝借用其他源码的 CI 成功记录。报告仅含版本、平台、源码树哈希、提交、工作流链接和状态；CI 只保留明确列出的源码包及公共验收摘要，不上传 env、备份、数据库、运行日志、浏览器存储状态或任意整个工作目录。CI Artifact 有保留期限，不能作为永久发布下载地址。

## 安装、升级与回滚证据

新安装、历史读取、容器替换与备份恢复由正式镜像验收覆盖。冻结迁移验收从原始 001–012 SQL 的固定哈希建立 PostgreSQL 样例，验证后续追加迁移、历史结果/HTML 报告、未知费用与重复启动；不启动 Agent。CI 另行从固定提交 `02515c1` 及源码树哈希构建独立基线镜像，执行基线安装、候选升级和原镜像备份回滚；合成私有 SDK 状态文件的内容、属主和权限必须保留。稳定性规模与运行命令见 [无模型稳定性与发布验收](STABILITY_RELEASE_BASELINE.md)。

有可用的内部基线镜像时，可以追加实际镜像升级：先生成两个仅 `AXIOM_RELEASE` / `AXIOM_IMAGE_PREFIX` 不同的私有配置，分别选中基线和候选镜像，再运行：

```bash
python3 deploy/ops/acceptance.py --env /absolute/private/baseline.env --candidate-env /absolute/private/candidate.env --work /absolute/private/upgrade-acceptance
```

两份配置必须选择相同的全新 `axiom-accept-*` 项目。工具先验证未使用，再执行基线安装、费用与清理证据写入、升级前备份、按顺序切换候选镜像、新 Fixture 任务及原镜像恢复。回滚后验证历史数据保留、候选新增任务不在旧恢复点、旧会话撤销。测试 Agent 网络无外部出口，模型调用为 0。

首版没有前一公开 Release。同版本替换、冻结 Schema 升级和内部镜像升级必须在报告中分别标识；后续版本应增加前一公开版本的实际镜像升级认证。

## 发布决定

开源入口、报告渠道、同一提交的五项远端 CI 和安装/恢复证据齐备后，维护者才能创建对应 `vVERSION` 标签与标记为 prerelease 的 GitHub Release。发布准备不自动创建标签、GitHub Release、容器仓库发布或升级业务环境。发布说明必须保留未执行真实 SDK 验收的限制。

用户和管理员的安装步骤见 [Beta 安装指南](BETA_INSTALLATION.md)。发布后的合同或 SDK 变更继续遵循 [兼容矩阵](../contracts/COMPATIBILITY.md) 与 [SDK 重建基线](SDK_REPRODUCIBILITY.md)。
