# 当前状态与验收范围

更新日期：2026-10-27。源码发布候选 **0.1.0-beta.2**；SDK **0.5.0**，Scanner description **14**，SDK result adapter **2**，数据库迁移截至 **028_model_token_pricing.sql**。Linux amd64、单主机、可信用户自托管仍为支持范围。

本页是当前状态入口。其他带日期的基线保留其当时版本与验收事实；其中的 SDK 0.1.25、未部署或未真实验收描述，不代表当前状态。Schema、npm 包和应用发布版本分别管理。

## 已完成并部署

- 项目、不可变源码上传、扫描配置、任务进度、历史结果、原生报告、漏洞与证据图谱。
- SDK Git refs、知识文档、组件计划与执行、身份管理、独立复核、结果导出，以及中断候选、复核和正式归档链路。各项的真实模型覆盖范围分别记录，不能由“已接入”推断全部通过真实模型验收。
- Responses 输入规范化与发送前字段校验、namespace 工具身份往返、报告正文及源码引用校验；连接成功只证明连通性，不能将模型标记为全面 supported。
- 通用模型单价、实际 Token 费用估算、每次执行价格快照与历史保留。缺少单价或所需用量时完整费用未知，0 仅表示明确的免费单价或已知零用量。
- 私有配置和身份保护、健康检查、一致性备份、隔离恢复与数据库迁移验证。

当前参考部署保持 **DeepSeek / deepseek-flash + Responses + 原生 Schema**。独立复核使用 `text.format=json_schema`、`strict=true`；原生扫描通过 Responses 工具 Schema 工作，不把每个扫描请求都描述成最终报告 Schema。没有 Chat 协议适配，不自动切换 JSON 模式或重放失败模型请求。`AXIOM_VALIDATION_OUTPUT_MODE` 的部署值为 `native_schema`。

## 验收事实

| 范围 | 已有证据 | 尚未证明 |
| --- | --- | --- |
| DeepSeek 小项目 | 一个可运行的 Python 小项目完成扫描、完整复核、源码校验、正式归档和交互证据图谱；57 次扫描请求、2 次复核请求，失败及自动重试均为 0 | 所有语言、项目规模、模型及 SDK 工作流的质量与稳定性 |
| 计价功能 | 498 项跨语言合同、290 项 Scanner、94 项 Web 测试；Server Store / HTTP、Agent、离线完整流程、桌面和 390px 浏览器通过 | 配置实际采购价后的真实费用估算及供应商账单核对 |
| 计价部署 | `vps-model-pricing-20261027-01` 三个应用镜像已部署；备份在断网副本恢复，Store 打开两次迁移幂等；39 张既有业务表、21 条费用记录及 8008 个文件内容保留 | 新源码候选已重新执行真实模型验收，或已经公开发布 |
| GLM | 使用者转述提供方确认 Responses 处理问题 | 提供方修复后的兼容性；当前未重新测试 |

真实验收是计价功能上线前的范围证据；本次候选整理没有再调用模型。参考部署的两个模型价格仍未配置，历史未知费用未被追溯补价、重算或改成零。推断证据继续标为 inferred；归档不表示运行时漏洞复现。

可随源码交付的脱敏摘要：[DeepSeek 真实验收](acceptance/deepseek-20261027.json)、[计价部署与隔离验收](acceptance/pricing-deployment-20261027.json)。原始响应、运行日志、数据库和私有备份不随源码交付。摘要保留原证据范围，不能替代新的真实验收。

## 当前部署与源码候选

VPS 的 Server / Agent 保留不可变标签 `vps-model-pricing-20261027-01`；Web 后续进行了仅静态资源与加载恢复的兼容修复，见 [静态 Web 兼容流程](STATIC_WEB_BASELINE.md#旧标签页与静态资源兼容)。计价部署的脱敏摘要继续保留其当时镜像与验收事实。本次文档、版本和发布检查整理不会重新部署或重标这些镜像。计价基线实现源码已整理为提交 `c654b6ff16db1ecf0a3a99e2da37181f76cc916b`；三个应用镜像的精确 ID 见脱敏部署摘要。

`VERSION` 和 `deploy/release/release.json` 标识本地源码候选。可复现源码包记录完整 Git SHA、逐文件清单及校验值；公开发布还须满足同一提交的五项远端 CI 和私密漏洞报告渠道检查。当前整理不创建远端标签、GitHub Release 或容器仓库发布。

## 使用与维护

- 安装、升级、备份与回滚：[Beta 安装指南](BETA_INSTALLATION.md)、[部署指南](../deploy/README.md)。
- 填写采购价与读取费用来源：[模型单价与 Token 费用](MODEL_PRICING.md)。
- SDK 和合同边界：[SDK 重建基线](SDK_REPRODUCIBILITY.md)、[兼容矩阵](../contracts/COMPATIBILITY.md)。
- 源码交付、版本与远端检查：[发布流程](RELEASE_PROCESS.md)、[本候选说明](releases/0.1.0-beta.2.md)。

仍需在明确授权后开展新计价的真实用量核对、更多真实项目与长期稳定性验收。组件规划、组件匹配、深度续扫、真实身份等各自保留独立验收范围；SDK 能力清单中的 planned / partial / adapter_required 不是已完成能力。
