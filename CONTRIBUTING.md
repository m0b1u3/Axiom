# 参与 Axiom

Axiom 当前准备首个自托管 Beta，支持 Linux amd64 与可信用户共享工作空间。所有扫描操作保留在 Web，Go Server、Go Agent、Node Scanner / SDK 的边界保持明确。

## 报告问题与提出修改

普通缺陷和功能建议使用仓库的 Issue 模板。提供版本、部署平台、复现步骤、预期与实际行为。安全问题通过 [私密漏洞报告](https://github.com/m0b1u3/Axiom/security/advisories/new) 提交，详见 [SECURITY.md](SECURITY.md)。

日志只提交脱敏后的最小片段。不要上传 API Key、会话令牌、环境配置、源码快照、数据库、备份或含客户代码的审计报告。

在 Pull Request 中说明问题、改变后的行为、验证结果与已知限制。涉及合同、迁移或恢复能力时，说明历史数据的兼容方式。测试使用明确标记的 Fixture；真实模型审计需另行明确授权，不纳入 CI。

## 本地开发

生产用户按 [部署指南](deploy/README.md) 安装；以下命令用于贡献者。需要 Go 1.24.3 或兼容的更新版本、Node.js 22.13+ / 24、npm、Python 3.10+，以及运行整栈验收所需的 Docker Engine 与 Compose v2。正式发布与 CI 的具体工具版本见工作流、Dockerfile 和 SDK 清单。

```bash
cd contracts
npm ci --ignore-scripts --no-audit --no-fund
npm run build
cd ../code-audit-scanner
npm ci --no-audit --no-fund
npm run verify:sdk
npm run build
cd ../code-audit-web
npm ci --no-audit --no-fund
npm run build
cd ..
bash deploy/dev-up.sh
```

开发启动默认使用 SDK 替身。生产配置拒绝示例密钥和默认弱密码。浏览器端模型配置、上传、启动、取消与结果查看不需要用户操作 Scanner 命令行。

## 验证要求

按修改影响选择检查；发布候选必须通过全部发布门槛。

```bash
python3 deploy/release/check.py
python3 -m unittest discover -s deploy/release -p '*_test.py' -v
python3 -m unittest discover -s deploy/ops -p '*_test.py' -v
python3 deploy/check_go_boundaries.py
npm --prefix contracts test
npm --prefix code-audit-scanner test
npm --prefix code-audit-web test
go -C code-audit-agent test -count=1 ./...
go -C code-audit-server test -p 1 -count=1 ./...
```

部分 Server 集成测试需要专用 PostgreSQL；使用 CI 的临时数据库配置，禁止指向实际业务库。浏览器与整栈测试入口见 [成熟化记录](docs/MATURITY_BASELINE.md) 和 [发布流程](docs/RELEASE_PROCESS.md)。CI 覆盖合同生成、跨语言兼容、依赖边界、Go/TS 回归、浏览器、正式镜像和备份恢复。

## 保持模块边界

- 数据库、迁移、凭据解密与业务调度归 Server；Agent 通过现有 HTTP 合同工作。
- SDK 依赖只进入 Scanner。修改 SDK 文件必须更新 `deploy/sdk` 中的固定补丁、哈希及重建验证。
- 修改 Schema 时重新生成 Go/TS 类型并补充共同的合法、非法和历史样例。不得重写已经发布的迁移或破坏历史读取。
- UI 开发前阅读 [AGENTS.md](AGENTS.md) 及其中指定的设计规则，复用已有领域组件；较大改动需真实浏览器截图复核。

## 许可

项目采用 [Apache-2.0](LICENSE)。贡献必须有权以项目许可证提交，并保留第三方来源与许可声明。新增依赖时同步锁文件，说明用途和许可证；不要提交 node_modules、构建产物、私人配置或运行数据。
