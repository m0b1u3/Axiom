# 原生安装阶段 0 运行验证

本目录提供 Linux x64 与 Windows x64 的依赖锁定和原生运行探针，具体改造阶段见[原生安装包方案](../../docs/NATIVE_INSTALLATION_PLAN.md)。探针使用未修改的 SDK 0.5.0、原生 Codex 0.163.0、合成源码和仅监听 loopback 的 Responses Fixture，不执行真实模型扫描或登录。

## 锁定内容

`dependencies.lock.json` 记录 16 个固定上游文件的 URL、大小和 SHA-256，覆盖 Node、Python、Git、PostgreSQL、Chromium、中文字体与 SDK。Node 校验值来自官方 SHASUMS，Git for Windows 校验值来自官方 Release，npm 归档核对现有锁文件的 SHA-512；其余 SHA-256 记录从明确版本的发布来源实际下载的文件，不声称额外完成了发布者签名验证。

Linux Python、Git 和 PostgreSQL 从锁定的源码构建。Windows 使用锁定的嵌入式 Python、MinGit 和 PostgreSQL 二进制包。平台二进制清单位于 `../sdk/manifest.json`，增加 Windows 条目不会将 Windows 自动标记为已验收，也不会改变 `official-v2` 与 `legacy-v1` 的隔离方式。

初始工作树源码快照和选定依赖输入分别记录。远端探针基于专用验证分支，只执行其中锁定的输入及探针；初始工作树哈希不表示整个未提交工作树都已上传或通过发行认证。

## 运行方式

推荐运行 `.github/workflows/native-stage0.yml`。它在 Ubuntu 24.04 和 Windows Server 2022 的原生 GitHub runner 上执行，不挂载业务目录或导入用户凭据。Windows 先以管理员安装测试负载，再由专用普通账户执行探针；Linux 由普通 runner 账户执行。准备步骤只适用于可销毁的验证主机。

独立运行需要 Python 3.12 的准备环境，以及 Linux 源码构建工具和开发库。先在全新目录准备运行依赖，再安装已有锁文件中的 SDK：

```text
python deploy/native/prepare.py --destination <new-runtime-directory> --output <report-directory>
python deploy/native/install_probe_sdk.py <report-directory>/runtime.json
<locked-node-executable> deploy/native/probe.mjs --out <report-directory> --runtime <report-directory>/runtime.json --work <new-fixture-working-directory>
```

Windows 的探针必须由普通账户执行，不能将管理员账户的成功结果当作用户态运行证据。现有包装脚本 `windows-probe.ps1` 为一次性 GitHub runner 管理账户和 ACL，不是供生产系统使用的安装器。

## 检查范围

- 原生操作系统、x64 架构、普通账户、精确 Node 版本和输入文件哈希。
- Python、Python SQLite、Node SQLite、Git、SDK、Codex 与 Windows 原生插件模块。
- 原版 SDK 的 Linux 沙箱预检，以及真实原生工具调用中的源码读取、源码写入拒绝、目录外写入拒绝和执行专用临时目录。
- Linux sh、bash 和 Windows PowerShell 的空格与中文路径。
- PostgreSQL 专用临时实例的启动、SCRAM 身份验证与 UTF-8 查询。
- 锁定浏览器和中文字体的 PDF 导出。浏览器参数对齐现有 PDF 实现；SDK 沙箱单独验证，不因 PDF 的既有 `--no-sandbox` 参数而关闭。

Fixture 的有限两轮交互与诊断超时只用于结束探针，不写入 Axiom 任务配置，不改变 SDK 原生重试、扫描时限、读取量、输出或并发限制。

`report.json` 逐项保留通过和失败，`sdk-*.json` 保留本地 Fixture 的工具输出及终止原因，构建日志和 `runtime.json` 记录负载准备结果。平台任一必要检查失败时，探针返回非零退出码并标记 `partial`，不能仅凭某一步成功声明平台可交付。Windows Server 2022 的结果不代替 Windows 11 验收。

## 本地检查

```text
python -m unittest discover -s deploy/native -p '*_test.py' -v
node --test deploy/sdk/apply.test.mjs deploy/sdk/sdk050-patches.test.mjs
node --check deploy/native/probe.mjs
```

依赖升级必须重新下载并独立核对上游来源、npm integrity、完整平台文件清单、`sourceInputs` 和原生验收结果。`fetch.py` 只消费已有锁文件，拒绝未经锁定或校验失败的输入，不执行下载内容。
