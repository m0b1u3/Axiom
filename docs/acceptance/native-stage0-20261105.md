# Linux／Windows 原生安装阶段 0 验证记录

日期：2026-11-05。阶段范围是依赖冻结和原生运行条件验证。安装器、原生服务、数据迁移与完整业务验收仍按[改造方案](../NATIVE_INSTALLATION_PLAN.md)后续阶段实施。

依赖锁定和双平台诊断已完成，阶段 0 的双平台运行门禁为 **部分通过**：Linux 配置宿主策略后 11/11 项通过，Windows 8/9 项通过。Windows 私有源码的原生沙箱读取仍被阻断，不能将 Windows 原生链路标记为可交付。

## 实测结果

| 检查 | Ubuntu 24.04 x64 | Windows Server 2022 x64 |
| --- | --- | --- |
| 普通账户与精确依赖输入 | 通过，UID 1001，非容器 | 通过，Administrator=false |
| Node／Python／SQLite／Git | 通过；Python SQLite 3.45.1、Node SQLite 3.53.4 | 通过；Python SQLite 3.49.1、Node SQLite 3.53.4 |
| 原版 SDK／Codex／文件哈希 | 通过，Linux 平台 45 文件 | 通过，Windows 平台 47 文件，`windows.node` 原生模块加载成功 |
| Python 插件 | 通过，随包 Python 执行原插件脚本 | 通过，随包 Python 执行原插件脚本 |
| 沙箱准备 | 默认宿主失败；加载固定 bwrap 路径的诊断 AppArmor 配置后通过 | 管理员预置 elevated 沙箱成功，实际扫描仍为普通账户 |
| shell、源码读取与边界 | sh、bash 均通过：读取合成源码、拒绝源码和目录外写入、临时文件落在本次执行内 | PowerShell 与随包 Python 已启动；读取私有源码脚本时 EACCES。源码读取及之后的写入边界、临时目录检查未通过 |
| PostgreSQL 16.15 | 临时实例启动、SCRAM、UTF-8 查询与停止通过 | 临时实例启动、SCRAM、UTF-8 查询与停止通过 |
| 浏览器与 PDF | 浏览器版本、字体哈希、PDF 文件头通过 | 浏览器版本、字体哈希、PDF 文件头通过 |
| 总计 | 默认 8/11；宿主策略配置后 11/11 | 8/9；没有遗留本次探针账户的进程 |

Linux 证据：[Actions 42007511683](https://github.com/m0b1u3/Axiom/actions/runs/42007511683/job/127177904361)，commit `bf523f3f99efa80c7c3e36305cbd02b34dd2f64f`。Windows 最终证据：[Actions 42009284628](https://github.com/m0b1u3/Axiom/actions/runs/42009284628/job/127183873386)，commit `af1d25dde632438e39623bc650de557f1e9ed9ae`。两个运行使用相同的依赖锁；Windows 后续提交只增加 Windows 路径、权限诊断和说明。

### Windows 阻断的具体范围

1. **凭据状态祖先权限**：共享临时目录以及 runner 的 `C:\ProgramData` 被 SDK 拒绝。SDK 指出祖先允许其他身份替换目录；原始 SDDL 中 ProgramData 的 Users 规则包含 `DC`。这说明只对末级目录设置私有 ACL 不足以安装 SDK 状态。独立的管理员所有、受保护的卷根子目录通过了这项检查，没有修改公共祖先 ACL。
2. **私有源码的读取授权**：修正祖先条件后，原版 SDK 发起了 3 次本地模型目录请求和 2 次 Responses Fixture 请求，原生 `exec_command` 启动 PowerShell，再启动固定 Python。Python 打开 `源码 with spaces\probe.py` 时返回 `[Errno 13] Permission denied`。为父目录增加不继承的只读／穿越权限后仍复现。
3. **宿主 ACL 证据**：源码目录、`probe.py` 和 `example.py` 的 SDDL 只有 fixture 普通账户、SYSTEM、Administrators 的完整控制；执行输出目录出现了沙箱身份的写入 ACE。已观测到私有源码没有相应沙箱身份的读取 ACE。是否还涉及 SDK 的 Windows 根只读映射需要进一步定位，不能仅凭错误消息认定为路径编码或 SDK 缺陷。

下一步应在 Windows 平台适配中实现并验证“只给 SDK 沙箱身份授权读取本次已授权快照”的流程，覆盖创建、复用、撤销、私有凭据隔离和源码写入拒绝；保留 SDK 的完整祖先校验与默认沙箱。此项解决前，不进入 Windows 安装包发行验收。原生 shell 已启动不能代替源码与边界检查通过。

## 冻结基线

| 项目 | 固定值 |
| --- | --- |
| 源码版本 | `0.1.0-beta.2`，初始未提交工作树的独立导出 |
| 源码树 SHA-256 | `4b1050169c74755716ebeb19ac4ee28386aced06f6b8b30cd5c87b071896ffc9` |
| 导出文件数 | 1,714 |
| SDK／Codex | `@openai/codex-security 0.5.0`／`codex-cli 0.163.0` |
| Node／随附 npm | `24.21.0`／`11.19.0` |
| Python | `3.12.10`；Linux 源码构建，Windows 嵌入式发行包 |
| Git | Linux `2.56.0`；Windows MinGit `2.56.0.windows.4` |
| PostgreSQL | `16.15`；Windows 二进制发行版本 `16.15-1` |
| PDF 浏览器 | Chrome for Testing headless shell `153.0.8010.12` |
| 中文字体 | Noto Sans CJK `Sans2.004`，固定 commit `523d033d6cb47f4a80c58a35753646f5c3608a78` |
| Windows C++ 运行库候选 | Microsoft v14 x64 `14.51.36247.0` |
| 合同与数据库 | 12 个合同数据基线文件，28 个迁移；最新 `028_model_token_pricing.sql` |
| Go | 两个应用模块的最低版本均为 `1.24.0`；发行编译器与应用二进制构建属于阶段 1 |

[依赖锁](../../deploy/native/dependencies.lock.json)固定 16 个上游文件；[Windows 系统依赖锁](../../deploy/native/system-prerequisites.lock.json)额外固定 C++ 运行库安装器。17 个文件均实际下载并核对大小与 SHA-256，合计 1,115,618,045 字节。这是两个平台的验证输入总量，不能作为单平台安装包大小。

Node 的校验值核对官方 SHASUMS，Git for Windows 核对发布摘要，4 个 npm 归档核对现有 npm 锁中的 SHA-512。其他文件记录明确版本发布来源的实际下载哈希。C++ 安装器另在 Windows 核对有效的 Microsoft Corporation Authenticode 签名和精确文件版本；其余输入不声称完成额外的发布者签名认证。

源码导出使用现有 `deploy/release/source.py`，保留在本机独立验证目录。归档 SHA-256 为 `91e302e8a2b979fdf4af0fcda5d917b7f6292ed0c02246d96ad8b75ead2c3cc7`。[基线记录](native-stage0-baseline-20261105.json)保存合同、迁移和 Go 依赖文件的逐文件哈希。

远端专用分支只带入 10 个逐字节锁定的依赖输入和探针。初始工作树的树哈希不表示全部未提交改动已上传、构建或通过验收。根目录 `.gitattributes` 防止 Windows 自动转换这些输入的换行符。SDK 原有清单头部的 Node 22.23.3／npm 10.9.8 保留为历史元数据；本次原生组合由新锁和独立运行记录定义。

## 已交付的改动

| 位置 | 改动 |
| --- | --- |
| `deploy/native/*lock.json` | 精确版本、固定 URL、文件大小、SHA-256 与选定源码输入身份 |
| `deploy/native/fetch.py`、`platform_manifest.py` | 拒绝校验失败的缓存或下载，先校验 npm integrity 再派生完整文件清单 |
| `deploy/sdk/manifest.json` | 新增 Windows x64 Codex 全部 47 个文件的哈希；Linux 45 文件和兼容运行时隔离规则保留 |
| `deploy/native/prepare.py`、`install_probe_sdk.py` | 在一次性主机组装负载，用随包 Node／npm 安装已有锁文件，校验原版 SDK |
| `deploy/native/probe.mjs` | 普通账户、原生模块、真实工具调用、沙箱边界、临时数据库和 PDF 的独立检查与增量证据 |
| `deploy/native/linux_profile.py`、`linux-bwrap.profile.in` | 仅供 Actions 诊断的 bwrap 路径和哈希限定 AppArmor 配置 |
| `deploy/native/windows-*.ps1`、`windows-home.mjs` | 一次性普通账户、受保护目录、沙箱管理员预置、系统运行库签名与 ACL 证据 |
| `.github/workflows/native-stage0.yml` | 原生 Ubuntu 24.04／Windows Server 2022 runner，保留失败项与证据，不依赖 Docker 或 WSL |

SDK `official-v2` 的源代码、默认 V2 和 Windows `elevated` 沙箱没有改动。诊断使用合成源码、虚拟密钥和仅监听 loopback 的模型接口 Fixture，真实模型调用和真实登录均为 0。Fixture 的两轮交互和诊断超时用于收尾检查，没有写入产品扫描、复核、读取、输出或并发配置。

## 仍需完成的后续验收

| 项目 | 已有证据与需要解决的工作 | 对应阶段 |
| --- | --- | --- |
| Linux 宿主策略 | 默认 Ubuntu runner 的 bwrap 被阻断；路径限定的 AppArmor 诊断配置通过。安装器需要正式的配置生成、加载、升级、卸载及失败提示，不能关闭全局 AppArmor 或沿用容器配置 | 1／2 |
| Windows 安装路径和 ACL（阻断） | 私有源码读取失败；需要面向 SDK 沙箱身份的快照只读授权及回收。SDK 状态必须放在祖先链可信的位置，原定 ProgramData 需要独立处理或调整路径 | 1／3 |
| 干净 Windows 系统依赖 | runner 已有 `msvcp140.dll`、`vcruntime140.dll`、`vcruntime140_1.dll`，版本均为 `14.51.36247.0`。PostgreSQL ICU DLL 存在 MSVCP／VCRUNTIME 导入；最终包需验证运行库缺失时的安装流程，阶段 0 只验证安装器来源和签名 | 1／3／5 |
| Windows 11 与服务账户 | Server 2022 runner 不代替 Windows 11、SCM Session 0、用户注销、系统重启与非交互账户验收 | 3／6 |
| 系统库与独立负载 | Linux 源码构建依赖宿主开发库，精确已安装版本保存在 `host-packages.log`。需要锁定发行构建镜像和工具链、核对共享库闭包，并验证脱离源码仓库和 npm 缓存的运行负载 | 1／2 |
| 应用进程生命周期 | Server／Agent 服务宿主、Windows Job Object、取消、崩溃、续扫归属与清理仍需按原方案实现；一次性探针账户清理不能代替产品实现 | 1／2／3 |
| 完整业务与发行 | 本次没有启动完整原生应用、执行真实模型扫描、迁移业务数据库或生成安装器。标准／Deep／差异／组件任务、复核、身份、历史结果、升级恢复和签名仍按后续门禁验收 | 2—6 |

AppArmor 前提依据 [Ubuntu 用户命名空间限制说明](https://discourse.ubuntu.com/t/understanding-apparmor-user-namespace-restriction/58007)。Windows 的“管理员预置，普通账户执行”依据 [OpenAI Windows 沙箱说明](https://learn.chatgpt.com/docs/windows/windows-sandbox)，锁定的 0.163.0 已实测支持该预置命令。C++ 运行库来源依据 [Microsoft 官方发行页](https://learn.microsoft.com/en-us/cpp/windows/latest-supported-vc-redist)。

当前 VPS 只做只读前提检查：普通 UID 的用户命名空间创建被拒绝。没有给 VPS 安装原生运行依赖、加载上述 AppArmor 配置、迁移数据库或切换现有 Compose 服务；Actions 的配置通过结果不能直接视为 VPS 原生部署通过。

## 复核与证据

本机 6 项输入完整性测试通过，SDK 9 项隔离与补丁测试通过，`apply.mjs --check` 通过。Windows runner 的 6 项输入测试中 5 项通过，1 项 Linux AppArmor 专用测试按平台跳过。Git 自动换行场景也复核了选定输入的逐字节身份。

共用配置与维护工具可以继续推进；Windows 原生工具链门禁仍未关闭。Windows 11、真实服务账户和完整业务链路不能从本次 Server 2022 的部分通过结果中推定。

机器可读结果保存在 [native-stage0-20261105.json](native-stage0-20261105.json)，包含依赖锁身份、实际平台结果、CI commit／run／artifact 及下载摘要。完整构建与诊断日志保留在 CI artifact 和本机 `output/native-stage0-20261105-01/`；不上传 SDK 私有凭据状态。
