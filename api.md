## Codex Security SDK 是做什么的

它是一个 **TypeScript/Node.js SDK**，用于把 Codex Security 的代码安全扫描能力嵌入你自己的平台、CI/CD、代码审查机器人或安全运营系统。

它和普通的 `@openai/codex-sdk` 不同：

* `codex-sdk`：偏向编程代理、修改代码、执行开发任务。
* `codex-security`：偏向漏洞发现、验证、覆盖度分析和安全报告。

官方包名是：

```bash
npm install @openai/codex-security
```

SDK 包公开可安装，但实际运行扫描仍需要获得 Codex Security 使用权限。([OpenAI Developers][1])

## 主要功能

### 1. 扫描整个代码仓库

最基础的功能是对本地仓库运行安全审计：

```ts
import { CodexSecurity } from "@openai/codex-security";

const security = new CodexSecurity();

try {
  const result = await security.run("/path/to/repository", {
    outputDir: "/private/security-results",
  });

  console.log("报告：", result.reportPath);
  console.log("漏洞数量：", result.findings.findings.length);
  console.log("覆盖状态：", result.coverage.completeness);
} finally {
  await security.close();
}
```

`run()` 会启动扫描、等待扫描完成、验证生成的安全产物，然后返回结构化的 `ScanResult`。([OpenAI Developers][1])

---

### 2. 指定目录或文件扫描

不一定要扫整个项目，可以限定范围：

```ts
const result = await security.run("/repo", {
  target: [
    "services/payment",
    "packages/auth",
    "src/api/admin.ts",
  ],
});
```

适合：

* 单独审计登录、权限、支付模块
* 大型 Monorepo 分模块扫描
* 只扫描外部暴露的 API
* 扫描高风险业务代码

路径只能指向仓库内部的文件或目录，SDK 会解析路径并去重。([OpenAI Developers][1])

---

### 3. 扫描两个 Git 版本之间的改动

可以扫描两个 Commit、Tag 或分支之间的代码差异：

```ts
import { CodexSecurity, DiffTarget } from "@openai/codex-security";

const target = DiffTarget.refs({
  base: "origin/main",
  head: "HEAD",
});

const result = await security.run("/repo", {
  target,
});
```

典型用途是：

* Pull Request 安全审查
* 合并前漏洞检查
* 发布版本与上个版本之间的安全审计
* 检查某次 Commit 是否引入安全问题

`head` 默认是 `HEAD`，扫描前需要确保相关 Git revision 已经在本地获取。([OpenAI Developers][1])

---

### 4. 扫描尚未提交的代码

它也能扫描工作区中 staged 和 unstaged 的改动：

```ts
const target = DiffTarget.workingTree({
  base: "HEAD",
});

const result = await security.run("/repo", {
  target,
});
```

这个模式很适合做本地提交前检查，例如集成到：

```bash
git commit
```

或：

```bash
pre-commit
```

流程中。([OpenAI Developers][1])

---

### 5. 深度扫描模式

普通扫描之外，还有 `deep` 模式：

```ts
const result = await security.run("/repo", {
  target: ["services/billing"],
  mode: "deep",

  workers: 2,
  subagents: 0,
  stopAfterNoNew: 3,
  maxDiscoveryRuns: 10,
  maxTimeHours: 1.5,
});
```

深度模式可以反复进行漏洞发现，使用多个独立 Worker 扫描并汇总结果。可以限制：

* 并行 Worker 数量
* 每个 Worker 的子代理数量
* 连续多少轮没有新漏洞后停止
* 最大扫描轮数
* 最长扫描时间

深度模式只支持整个仓库或指定路径；Git diff 和 working-tree 扫描应使用标准模式。达到时间限制后，SDK 会保留已完成 Worker 的结果，并把覆盖度标记为可能不完整。([OpenAI Developers][1])

---

### 6. 注入架构文档和威胁模型

可以把项目架构、威胁模型、安全规范等文档作为扫描上下文：

```ts
const result = await security.run("/repo", {
  knowledgeBasePaths: [
    "/docs/architecture.md",
    "/docs/threat-model.pdf",
    "/docs/security-policies",
  ],
});
```

支持：

* `.md`
* `.markdown`
* `.txt`
* `.pdf`
* `.docx`

目录会被递归读取。这样 Codex Security 不只是看代码，还能理解：

* 哪些接口是公网接口
* 哪些服务属于高信任区域
* 租户隔离规则
* 数据库和消息队列信任边界
* 哪些操作必须管理员授权
* 哪些数据属于敏感数据

这对发现越权、跨租户访问、业务逻辑漏洞通常比纯静态规则更有价值。([OpenAI Developers][1])

---

### 7. 自定义扫描重点

可以通过 `scanPrompt` 指定安全审计重点：

```ts
const result = await security.run("/repo", {
  scanPrompt: `
    重点检查：
    1. 多租户数据隔离
    2. 水平和垂直越权
    3. 管理员接口鉴权
    4. SSRF 与内部网络访问
    5. Webhook 签名验证
  `,
});
```

还可以通过 `postScanPrompt` 在扫描结束后执行后续处理：

```ts
const result = await security.run("/repo", {
  scanPrompt: "重点检查 tenant isolation 和 authorization。",
  postScanPrompt:
    "将已确认的高危漏洞整理到 post-scan-summary.md。",
});
```

扫描后处理失败不会抹掉已经完成的扫描结果，而是通过 warning 回调报告。([OpenAI Developers][1])

---

### 8. 输出结构化漏洞信息

SDK 不只是生成一份自然语言报告，还会返回可编程处理的数据：

```ts
for (const finding of result.findings.findings) {
  const location = finding.locations[0];

  console.log({
    title: finding.title,
    severity: finding.severity.level,
    file: location?.path,
    line: location?.startLine,
  });
}
```

单个 Finding 还可能包含：

* `codeEvidence`：代码证据
* `rootCause`：根因
* `validation`：漏洞验证信息
* `attackPath`：攻击路径
* `remediationTests`：修复后的验证测试
* `preventiveControls`：预防性安全控制

因此可以把结果接入自己的漏洞平台，而不是只能人工看 Markdown。([OpenAI Developers][1])

---

### 9. 输出多种报告和产物

`ScanResult` 会提供：

| 字段              | 用途                   |
| --------------- | -------------------- |
| `reportPath`    | Markdown 安全报告        |
| `findingsPath`  | 结构化漏洞 JSON           |
| `coveragePath`  | 扫描覆盖度 JSON           |
| `manifestPath`  | 扫描清单和产物校验信息          |
| `sarifPath`     | SARIF 报告，存在时可接代码扫描平台 |
| `artifactsDir`  | 漏洞验证和支撑材料目录          |
| `scanDir`       | 本次扫描完整目录             |
| `threadId`      | 对应的 Codex 线程         |
| `cost`          | Token 和模型成本估算        |
| `pluginVersion` | 本次扫描使用的安全插件版本        |

还可以调用：

```ts
const json = result.toJSON();
```

把扫描结果整体转换成可序列化对象。([OpenAI Developers][1])

---

### 10. 返回扫描覆盖度

SDK 会告诉你它检查了什么、没检查什么，而不是只返回“发现了几个漏洞”。

```ts
console.log(result.coverage.completeness);

for (const item of result.coverage.deferred) {
  console.log(item.id, item.reason);
}
```

覆盖状态包括：

* `complete`
* `partial`
* `unknown`

Coverage 中还会记录：

* 已检查的攻击面
* 排除内容
* 延后检查的内容
* 未解决问题
* 扫描完整程度

因此不能只看“0 个漏洞”就认为系统绝对安全，还应检查 `coverage.completeness`、排除项和 deferred 内容。([OpenAI Developers][1])

---

### 11. 仓库历史漏洞跟踪

当存在扫描历史时，可以读取整个仓库仍处于 Open 状态的漏洞：

```ts
for (const finding of result.repositoryFindings ?? []) {
  console.log(
    finding.title,
    finding.confirmedInLatestScan,
  );
}
```

`confirmedInLatestScan` 可以区分：

* 本次扫描仍能确认的漏洞
* 历史扫描发现、但本次未重新确认的漏洞

适合构建漏洞生命周期管理、回归验证和历史趋势面板。([OpenAI Developers][1])

---

### 12. 扫描前预检查

`preflight()` 可以在真正消耗模型资源前验证配置：

```ts
const plan = await security.preflight("/repo", {
  target: ["services/billing", "packages/auth"],
  knowledgeBasePaths: ["/docs/architecture.md"],
  outputDir: "/private/security-results",
});

console.log(plan.repository);
console.log(plan.target.kind);
console.log(plan.mode);
console.log(plan.outputDir);
```

它会检查：

* 仓库路径
* 扫描 Target
* 扫描模式
* 知识库文档
* 输出目录
* Codex 配置

`preflight()` 本身不会启动 Codex Runtime，也不会使用扫描凭证，适合在 Web 后台或任务队列提交长任务前校验用户输入。([OpenAI Developers][1])

---

### 13. 成本限制与成本监控

可以设置扫描预算：

```ts
const result = await security.run("/repo", {
  maxCostUsd: 5,

  onCost(cost) {
    console.log("预估费用：", cost.estimatedUsd);
  },
});
```

需要注意，`maxCostUsd` 是估算限制，并非绝对硬上限，已经在执行中的请求可能让最终成本略微超出限制。

深度扫描达到预算后，如果已经有足够结果，SDK 可能返回部分结果并将 Coverage 标记为 `partial`；无法生成有效结果时，会抛出 `ScanCostLimitExceededError`。([OpenAI Developers][1])

---

### 14. 实时进度和生命周期事件

SDK 提供了较完整的回调：

```ts
const result = await security.run("/repo", {
  onScanStarted() {
    console.log("扫描开始");
  },

  onProgress(progress) {
    console.log(
      progress.phase,
      progress.filesCompleted,
      progress.filesTotal,
    );
  },

  onWorkerStatus(status) {
    console.log(status.kind);
  },

  onWarning(warning) {
    console.warn(warning);
  },

  onReconnect(attempt, maxAttempts) {
    console.log(`重新连接 ${attempt}/${maxAttempts}`);
  },

  onObserverError(observer, error) {
    console.error(observer, error);
  },
});
```

主要生命周期回调包括：

* 身份验证方式选择
* 输出目录准备完成
* 历史结果归档
* Trusted Access 状态
* 扫描开始
* 文件扫描进度
* Worker 状态
* Agent Session 事件
* 成本变化
* 连接重试
* 警告和监听器异常

这让你可以做 WebSocket 实时进度、任务队列状态、扫描控制台和审计日志。`onSessionEvent` 可能包含未脱敏的源码或凭据，因此不应直接写入公共日志系统。([OpenAI Developers][1])

---

### 15. 中途取消扫描

支持标准 `AbortSignal`：

```ts
import {
  CodexSecurity,
  ScanInterruptedError,
} from "@openai/codex-security";

const controller = new AbortController();

try {
  const scan = security.run("/repo", {
    signal: controller.signal,
  });

  // 由用户操作、任务超时或服务关闭触发
  controller.abort();

  await scan;
} catch (error) {
  if (error instanceof ScanInterruptedError) {
    console.log("残留扫描目录：", error.scanDir);
  } else {
    throw error;
  }
}
```

被取消的扫描可能仍在 `scanDir` 中留下部分产物，便于排查或保留证据。([OpenAI Developers][1])

---

### 16. 身份验证管理

SDK 支持：

```ts
await security.loginApiKey(apiKey);
await security.loginChatGPT();
await security.loginChatGPTDeviceCode();

const account = await security.account();

await security.logout();
```

支持的认证来源包括：

* `OPENAI_API_KEY`
* `CODEX_API_KEY`
* 已保存的 Codex 登录
* ChatGPT 浏览器登录
* ChatGPT Device Code 登录

在扫描时还可以强制选择认证方式：

```ts
await security.run("/repo", {
  auth: "chatgpt",
});
```

或：

```ts
await security.run("/repo", {
  auth: "api-key",
});
```

同时存在 API Key 和本地登录状态时，SDK 默认优先使用 API Key。([OpenAI Developers][1])

---

### 17. 自定义模型和模型提供商

可以覆盖 Codex Runtime 配置：

```ts
const security = new CodexSecurity({
  pythonPath: "/usr/bin/python3",

  codexOverrides: {
    model: "your-model",
    model_reasoning_effort: "high",
  },
});
```

SDK 还支持配置其他模型提供商，包括：

* Amazon Bedrock
* OpenRouter
* Fireworks

不过更换模型后，漏洞发现、验证能力和稳定性可能不同；官方默认配置通常更适合作为基准。([OpenAI Developers][1])

---

### 18. 细粒度错误处理

SDK 导出了专门的错误类型，例如：

```ts
AuthenticationRequiredError
ConfigurationError
InvalidTargetError
OutputDirectoryError
OutputInsideProtectedRootError
PluginPythonUnavailableError
PluginBootstrapError
ScanCostLimitExceededError
IncompleteScanError
ContractValidationError
ScanInterruptedError
```

这方便在生产系统中区分：

* 用户没有登录
* 仓库路径不正确
* Git Target 不存在
* 输出目录权限不足
* Python 环境缺失
* 扫描超过预算
* 扫描被用户取消
* 返回结果不符合结构化契约

官方列出了这些错误类型及其对应场景。([OpenAI Developers][1])

## 最适合拿它做什么

Codex Security SDK 比较适合下面几类场景：

1. **PR 安全机器人**
   每次 Pull Request 创建后，扫描 `base...head` 差异，把高危漏洞写入 PR 评论或 Checks。

2. **企业内部 SRC 平台**
   用户选择仓库和范围，后台调度扫描，前端展示 Findings、攻击路径、证据和修复建议。

3. **CI/CD 安全门禁**
   根据严重等级、验证状态和扫描覆盖度决定是否阻止构建。

4. **定期全仓库深度审计**
   每周或每月执行 Deep Scan，把结果同步到漏洞管理平台。

5. **敏感模块专项审计**
   对认证、支付、租户隔离、文件上传、Webhook、反序列化等模块应用定制 `scanPrompt`。

6. **漏洞修复回归验证**
   利用仓库历史 Findings 和 `confirmedInLatestScan` 检查旧漏洞是否仍然存在。

## 运行环境和安全注意事项

当前 SDK 使用 ESM，运行在服务端，要求 Node.js 22.13.0 以上的 22 系列，或 Node.js 24、26；实际扫描还需要 Python 3.10 以上。([OpenAI Developers][1])

更重要的是，SDK 扫描进程使用运行它的本地操作系统权限，而且不会暂停等待人工批准。因此应当：

* 只扫描自己拥有或明确获得授权的仓库
* 使用隔离容器或专用低权限用户运行
* 扫描前清理无关的云密钥、生产数据库密码和 SSH 凭据
* 把 `outputDir` 放在仓库之外
* 限制报告目录权限，因为报告中可能包含源码片段和漏洞细节

官方明确说明 SDK 会使用本地权限运行，扫描过程也可能继承当前环境变量。([OpenAI Developers][1])

一句话概括：**Codex Security SDK 的价值不是“调用一次 AI 帮我看看代码”，而是把全仓审计、Diff 审查、漏洞验证、覆盖度、成本控制、实时进度和结构化报告变成可编程能力。**

[1]: https://developers.openai.com/codex/security/sdk "TypeScript SDK – Codex Security | ChatGPT Learn"


不。上一条**没有把 SDK 全部功能分析完**。刚才我重新按当前源码的公开 API 面逐项核对了一遍，确认上一条漏掉了一批相当重要的能力，而且还需要严格区分 **npm 包里的 CLI 功能** 和 **TypeScript SDK 真正公开的 API**。

截至 **2026 年 9 月 8 日**，npm 页面显示 `@openai/codex-security` 最新为 **0.1.25**。它仍处于 1.0 之前，公开 API 可能继续变化。([npm][1])

## 现在按“SDK 公共 API”重新梳理

当前 GitHub `main` 的 `sdk/typescript/src/index.ts` 有 **161 行公开导出定义**。从这个入口来审计，比只看使用文档准确得多。([GitHub][2])

| 能力类别             | SDK 能力                             | 代表 API                                             |
| ---------------- | ---------------------------------- | -------------------------------------------------- |
| 核心扫描             | 全仓、路径、Git Diff、Working Tree        | `CodexSecurity.run()`、`DiffTarget`                 |
| 扫描模式             | Standard / Deep                    | `mode`, `workers`, `subagents` 等                   |
| Mock 扫描          | 不调用 LLM 生成模拟扫描数据                   | `run(...,{mock:true})`                             |
| 自定义发现            | 给发现阶段额外安全指令                        | `scanPrompt`                                       |
| 自定义验证            | 完全替换最终漏洞验证步骤                       | `validationPrompt`                                 |
| 扫描后处理            | 扫描结束后运行 follow-up                  | `postScanPrompt`                                   |
| 知识库              | 架构、威胁模型、安全策略等                      | `knowledgeBasePaths`                               |
| 独立漏洞验证           | 不重新扫描仓库，单独验证一个 finding             | `security.validate()`                              |
| 扫描预检             | 不启动完整扫描即验证配置                       | `security.preflight()`                             |
| 漏洞匹配             | 判断两次扫描中的漏洞是否同一根因                   | `matchScanFindings()`                              |
| 漏洞去重             | 对接 findings service 后做跨 finding 去重 | `deduplicateScan()` / `deduplicateScanDirectory()` |
| 严重度重新分类          | 根据自定义安全 rubric 重新评估严重度             | `classifySeverity()` 等                             |
| 组件扫描             | 把 monorepo 拆成多个组件分别扫描              | `runComponentScans()`                              |
| 自动组件规划           | AI/逻辑生成组件划分                        | `planComponents()`                                 |
| GitHub 导入        | 导入 GitHub Code Scanning / SARIF 告警 | `importGitHubCodeScanningAlerts()`                 |
| Linear 发布        | 把 finding 发布成 Linear issue         | `publishScan()`                                    |
| 发布状态检查           | 检查 Linear 发布结果                     | `checkScanPublication()`                           |
| 自定义 Findings API | 把扫描结果发送到自己的漏洞后端                    | `publishScanToCustom()`                            |
| Workflow         | scan → publish → dedupe 可恢复工作流     | `workflowId`                                       |
| 成本估算             | 估算扫描成本                             | `estimateScanCost()`                               |
| 成本限制             | 限制/动态提高预算                          | `maxCostUsd`, `onBudgetApproaching`                |
| 生命周期事件           | 进度、worker、session、cost、warning 等   | 多种 callbacks                                       |
| 认证               | API Key / ChatGPT / Device Code    | `loginApiKey()` 等                                  |
| 账户管理             | 查询登录状态、退出                          | `account()`, `logout()`                            |
| 资源管理             | Runtime 清理                         | `close()`, `Symbol.asyncDispose`                   |
| Codex 配置         | 合并/生成 Codex Runtime 配置             | `mergedCodexConfig()` 等                            |
| Runtime 控制       | Plugin/Python/Codex 可执行程序等         | 多个 runtime helpers                                 |
| Contract 校验      | 加载和验证扫描产物                          | `loadContract()`, `requireScanFile()`              |
| Target 工具        | 仓库/Git target 标准化                  | `normalizeTarget()` 等                              |
| 扫描结果模型           | Findings、Coverage、Manifest、SARIF 等 | `ScanResult`                                       |
| 错误体系             | Auth、预算、中断、产物、插件等错误                | 多个 Error class                                     |
| 版本信息             | SDK/Plugin 版本                      | `VERSION`, `BUNDLED_PLUGIN_VERSION`                |

这才接近当前 SDK 的完整功能面。公开入口本身明确导出了 classification、component planning/scanning、comparison、deduplication、publication、GitHub import、runtime、contracts、targets 等模块。([GitHub][2])

---

## 我上一条最重要的遗漏：独立漏洞验证

这个功能其实非常重要：

```ts
const result = await security.validate({
  repositoryPath: "/repo",

  finding: {
    title: "Possible SQL injection",
    location: "src/query.ts:42",
  },
});

console.log(result.disposition);
console.log(result.report);
```

它不是：

> “再跑一遍安全扫描看看有没有这个漏洞”

而是：

> **给它一个现有漏洞报告，让 Codex Security 专门验证这个漏洞。**

结果有：

```text
reportable
suppressed
not_applicable
deferred
```

因此你可以把：

* Semgrep finding
* CodeQL finding
* Burp 人工发现
* SRC 报告
* GitHub Code Scanning alert
* 其他 SAST finding

喂给 Codex Security，让它进行二次验证。

而且 standalone validation **不会修改仓库，也不会在 scan history 里创建一次普通扫描记录**。([GitHub][3])

这个能力我上一条完全漏掉了。

---

# 另一个非常重要的能力：导入 GitHub Code Scanning

SDK 可以：

```ts
import {
  importGitHubCodeScanningAlerts,
  CodexSecurity
} from "@openai/codex-security";

const findings =
  await importGitHubCodeScanningAlerts({
    repository: "owner/repo",
    githubToken: process.env.GH_TOKEN,
  });

for (const finding of findings) {
  const result = await security.validate({
    repositoryPath: "/repo",
    finding,
  });

  console.log(result.disposition);
}
```

支持 GitHub Code Scanning 中的告警，包括**第三方上传的 SARIF**。

所以它实际上可以形成：

```text
CodeQL / Semgrep / SARIF
          ↓
GitHub Code Scanning
          ↓
Codex Security SDK
          ↓
AI 二次漏洞验证
          ↓
reportable / suppressed
```

Import 本身是只读操作，不启动 Codex，也不会替你 checkout 代码。([GitHub][3])

这个能力对安全平台特别有价值。

---

# 还有 Mock Scan

```ts
const result = await security.run(repo, {
  mock: true
});
```

这个模式：

* 不需要 Codex 身份认证
* 不调用 LLM
* Token 使用量为 0
* 生成完整模拟扫描结构
* 生成 Finding / Artifact / Report / History
* 默认产生 12 个 synthetic findings
* 可以测试 dedup 流程
* 可以测试你的 Web UI
* 可以测试数据库导入
* 可以测试 CI/CD

支持：

```text
repository
path
diff
```

但只支持 Standard，不支持：

```text
Deep
custom validation
post-scan prompt
patch
```

([GitHub][3])

这对于你自己开发一个 Codex Security 管理平台其实非常实用。

---

# 还有我上一条完全没讲的 Component Scan

它不只是：

```ts
target: [
  "apps/api",
  "apps/web"
]
```

这种“多个路径放进同一次扫描”。

SDK 有专门的：

```ts
runComponentScans()
```

例如一个 monorepo：

```text
repo
├── apps/
│   ├── api
│   ├── web
│   └── admin
├── services/
│   ├── payment
│   └── auth
└── packages/
    └── shared
```

可以把它真正拆成多个独立扫描：

```text
component-1 → API
component-2 → Web
component-3 → Admin
component-4 → Payment
component-5 → Auth
```

并支持并发：

```ts
await runComponentScans({
  repository: "/repo",
  outputDir: "/scan-results",

  components: [
    {
      name: "API",
      paths: ["apps/api"]
    },
    {
      name: "Auth",
      paths: ["services/auth"]
    },
  ],

  workers: 4,
});
```

而且还有：

```ts
planComponents()
normalizeComponentPlan()
```

以及：

```ts
auto: true
planOnly: true
```

也就是说 SDK 能**自动分析仓库结构并提出组件划分方案**，之后逐组件并行扫描。扫描结束后还会合并结果，对跨组件的同根因 finding 做匹配/去重。([GitHub][4])

这对于大型 monorepo 很关键。

---

# Finding Matching：比较两次扫描是不是同一个漏洞

另一个非常值得注意的 API：

```ts
matchScanFindings()
```

例如：

```text
scan A

F-001
SQL Injection in query.ts
```

代码修改后：

```text
scan B

F-945
Unsafe SQL construction in database.ts
```

ID、标题、位置可能全变了。

但 Codex Security 可以判断：

```text
F-001 ↔ F-945

same root cause
```

SDK：

```ts
const comparison = await matchScanFindings(
  {
    before: oldFindings,
    after: newFindings,
  },
  {
    workingDirectory: "/repo",
  }
);

console.log(comparison.matches);
console.log(comparison.uncertain);
console.log(comparison.related);
```

结果区分：

```text
matches
uncertain
related
```

还支持把已经确认过的：

```ts
knownFindingGroups
```

传进去复用判断结果。([GitHub][3])

这意味着你可以做真正的：

```text
漏洞生命周期跟踪
```

而不是简单比较 finding ID。

---

# Severity Classification：重新给漏洞定级

这也是上一条完全漏掉的重要模块。

SDK 当前公开：

```ts
classifySeverity()
classifyScanSeverity()
classifyScanDirectorySeverity()
```

例如公司自己的定级规范：

```text
Critical:
能够跨租户获取敏感数据

High:
能执行管理员操作但需要普通账户

Medium:
需要复杂前置条件
```

然后：

```ts
const classification = await classifySeverity(
  findings,
  {
    rubricPath: "/security/severity-policy.md",
    knowledgeBasePaths: [
      "/security/architecture.md"
    ]
  }
);
```

它可以基于：

```text
代码证据
+
漏洞影响
+
企业架构
+
企业自己的定级 Rubric
```

重新分类 Severity。

更重要的是：

**不会修改扫描发现本身的原始 severity。**

Classification 是一层独立评估结果。([GitHub][3])

---

# Findings 去重能力

当前 SDK 还有：

```ts
deduplicateScan()
deduplicateScanDirectory()
```

它跟 `matchScanFindings()` 又不是完全一回事。

`matchScanFindings()` 更像：

```text
Scan A ↔ Scan B
```

判断漏洞身份关系。

而 deduplication 可以：

```text
Current Finding
      ↓
Findings Service
      ↓
搜索相似 Finding
      ↓
Codex Review
      ↓
确认是不是同一根因
      ↓
建立 duplicate group
```

例如：

```text
F1 SSRF in request.ts
F2 URL validation bypass
F3 Internal metadata endpoint reachable
```

最终可能认为：

```text
Root Cause RC-12
├── F1
├── F2
└── F3
```

SDK 对保存的 scan 和外部 sealed scan directory 都提供入口。([GitHub][5])

---

# 自定义 Findings Backend

这是另一个很有意思的地方。

SDK 有：

```ts
publishScanToCustom()
```

因此你不必把结果只留在 Codex Security 本地。

可以：

```ts
await publishScanToCustom(
  result.scanDir,
  {
    findingsUrl:
      "https://security.example.com/api",
  }
);
```

它会把完成且 sealed 的 findings 发布到你自己的 Findings API。

返回类似：

```ts
{
  scanId,
  repositoryId,
  findingIds,
  findingCount
}
```

还有：

```ts
dryRun
expectedScanId
workflowId
AbortSignal
```

支持。([GitHub][6])

于是可以建立：

```text
                         ┌→ Linear
Codex Security Scan ─────┼→ 自建漏洞平台
                         ├→ Findings Service
                         └→ Dedup Pipeline
```

---

# Linear 集成也是 SDK API

不是只有 CLI。

```ts
await publishScan(scanDir, {
  destination: "linear",
  teamId: "TEAM_ID",
  projectId: "PROJECT_ID",
  skipExisting: true,
});
```

还有：

```ts
checkScanPublication()
```

可以在真正 publish 前/后做只读状态检查。([GitHub][3])

---

# Custom Validation 比 scanPrompt 更强

我上一条虽然讲了 `scanPrompt`，但是漏掉了：

```ts
validationPrompt
```

这两个完全不是一回事。

```text
scanPrompt
    ↓
影响“怎么找漏洞”

validationPrompt
    ↓
替换“怎么验证漏洞”
```

例如：

```ts
await security.run(repo, {
  validationPrompt: `
    启动本地测试环境。

    对每个候选漏洞：
    1. 构造 HTTP 请求
    2. 进行真实权限测试
    3. 保存请求与响应
    4. 验证是否可以未授权访问
    5. 测试结束关闭环境
  `
});
```

自定义验证还能重新给 candidate 设置：

```text
severity
impact
disposition
confidence
evidence
```

Deep Scan 不支持这个选项。([GitHub][3])

这实际上非常接近一个：

**AI 驱动的漏洞验证 Agent Framework。**

---

# Workflow 机制

当前 `ScanOptions` 里还有：

```ts
workflowId
```

源码注释直接写的是：

> durable scan → custom publication → dedupe workflow

也就是说能让：

```text
scan
 ↓
publish
 ↓
deduplicate
```

组成一个**可恢复的持久工作流**。

扫描中断后，不一定要从第一步重新做。

`run()` 检测到 `workflowId` 后会进入专门的 workflow 路径并保存 scan/artifact 状态。

这也是做 SaaS / SRC 平台很有用的能力。

---

# Budget 其实也比我上一条讲得更完整

不仅有：

```ts
maxCostUsd
```

还有：

```ts
onBudgetApproaching
```

这个 callback 可以返回一个新的 budget：

```ts
onBudgetApproaching: async ({ cost, maxCostUsd }) => {

  if (shouldContinue) {
    return maxCostUsd + 5;
  }

  return undefined;
}
```

所以可以实现：

```text
初始预算 $3
      ↓
发现 Critical 候选
      ↓
自动批准继续
      ↓
预算提高到 $8
```

而不是只能“一开始设死一个价格”。`ScanOptions` 还同时公开 `onCost`。

---

# Scan 生命周期事件也比上一条列得更多

当前 `ScanOptions` 明确公开：

```ts
onCost
onBudgetApproaching

onOutputArchived
onOutputDirReady

onAuthentication
onTrustedAccessStatus

onScanStarted
onReconnect

onActivity
onSessionEvent

onProgress
onDeepProgress
onWorkerStatus

onWarning
onObserverError

signal
```

并且还有：

```ts
safetyIdentifier
workflowId
parentScanId
expectedPluginVersion
failureSeverity
```

这些都是当前 `ScanOptions` 的正式字段。

---

# `CodexSecurity` 本身的完整高层方法

目前核心 client 至少是：

```ts
const security = new CodexSecurity();
```

然后：

```ts
security.run()
security.validate()
security.preflight()

security.loginApiKey()
security.loginChatGPT()
security.loginChatGPTDeviceCode()

security.account()
security.logout()

security.close()
security[Symbol.asyncDispose]()
```

此外还有便利构造器：

```ts
createSecurity()
```

`close()` 会取消正在执行的 operation/login 并清理 Runtime；也支持 JavaScript Explicit Resource Management 的 `Symbol.asyncDispose`。

还有个开发时很容易踩的点：

**同一个 `CodexSecurity` 实例一次只能执行一个 tracked operation。**

如果同时跑两个：

```ts
security.run(...)
security.validate(...)
```

源码会抛：

```text
A Codex Security operation is already in progress.
```

想并发跑多个 scan，一般应该创建多个实例，或者使用专门的 component scan orchestration。

---

# 还有一层“底层 SDK API”

如果严格说“全部公开 API”，它甚至不仅是安全扫描。

当前 `index.ts` 还导出了 Runtime 工具：

```ts
bootstrapPlugin
bundledPluginRoot
cleanupSdkDirectory
createIsolatedHome
createMarketplace
extractPluginZip
importAmbientAuth

pluginExecutionEnvironment
pluginMetadata

prepareOutputDir
resolveCodexCommand
resolvePluginPath
resolvePluginPython
validateOutputDir
```

配置：

```ts
DEFAULT_CODEX_CONFIG
mergedCodexConfig
writeCodexConfig
```

Contract：

```ts
loadContract
requireScanFile
```

Targets：

```ts
DiffTarget
normalizeRepository
normalizeTarget
repositoryRevision
validateMode
```

以及：

```ts
estimateScanCost

VERSION
BUNDLED_PLUGIN_VERSION
```

再加上一整套 TypeScript 类型和错误类。([GitHub][2])

所以它其实已经不是一个简单：

```text
scan(repo)
```

的包装库。

---

## 还有一个需要纠正的地方：哪些是 CLI，不要算进 SDK

当前 npm 包同时包含：

```text
Codex Security CLI
+
Codex Security TypeScript SDK
```

README 中还有很多 CLI 能力，比如：

```text
bulk-scan
export
patch
verify-fix
scan history commands
interactive patching
install-hook
Cloud publish CLI workflow
```

这些**不能因为 npm 包支持，就自动说成 TypeScript SDK 有对应公开函数**。

例如当前公开的 `index.ts` 中，没有把：

```ts
patch()
verifyFix()
bulkScan()
exportScan()
```

作为同名顶层 SDK API 暴露出来。

反过来，SDK 当前明确公开了：

```text
classifySeverity
matchScanFindings
deduplicateScan
runComponentScans
publishScanToCustom
importGitHubCodeScanningAlerts
```

这些才应该算真正的 SDK 公共能力。([GitHub][2])

---

## 所以我现在对它的定位会改成

Codex Security SDK 并不只是：

> **AI SAST 扫描 SDK**

更准确应该是：

```text
                 Codex Security SDK

┌─────────────────────────────────────────┐
│              Discovery                  │
│ Repo / Path / Diff / Deep / Component   │
└────────────────┬────────────────────────┘
                 ↓
┌─────────────────────────────────────────┐
│              Validation                 │
│ Default / Custom / Standalone           │
└────────────────┬────────────────────────┘
                 ↓
┌─────────────────────────────────────────┐
│        Finding Intelligence             │
│ Match / Dedupe / Severity Classification│
└────────────────┬────────────────────────┘
                 ↓
┌─────────────────────────────────────────┐
│            Lifecycle                    │
│ History / Coverage / Cost / Workflow    │
└────────────────┬────────────────────────┘
                 ↓
┌─────────────────────────────────────────┐
│            Integrations                 │
│ GitHub / Linear / Custom Findings API   │
└─────────────────────────────────────────┘
```

也就是一个比较完整的 **AI AppSec / Vulnerability Management SDK**，而不只是“让 Codex 帮你找几个漏洞”。

**所以你的质疑是对的：上一条我只覆盖了核心扫描层，大约遗漏了 validation、classification、matching、dedupe、component orchestration、GitHub import、publication/workflow 和一批底层 public API。**

如果严格以 **当前 v0.1.25 / `index.ts` 公共 API** 为标准，上面这一版才接近完整的 SDK 功能地图。([npm][1])

[1]: https://www.npmjs.com/package/%40openai/codex-security?utm_source=chatgpt.com "@openai/codex-security - npm"
[2]: https://github.com/openai/codex-security/blob/main/sdk/typescript/src/index.ts "codex-security/sdk/typescript/src/index.ts at main · openai/codex-security · GitHub"
[3]: https://github.com/openai/codex-security/tree/main/sdk/typescript "codex-security/sdk/typescript at main · openai/codex-security · GitHub"
[4]: https://github.com/openai/codex-security/blob/main/sdk/typescript/src/component-scan.ts "codex-security/sdk/typescript/src/component-scan.ts at main · openai/codex-security · GitHub"
[5]: https://github.com/openai/codex-security/blob/main/sdk/typescript/src/deduplication/scan.ts "codex-security/sdk/typescript/src/deduplication/scan.ts at main · openai/codex-security · GitHub"
[6]: https://github.com/openai/codex-security/blob/main/sdk/typescript/src/custom-publish.ts "codex-security/sdk/typescript/src/custom-publish.ts at main · openai/codex-security · GitHub"


## 统一进度与事件（步骤 4）

用户仍通过 `/api/v1/scans` 创建任务，详情返回 `observationContractVersion`、`progress`（生成的 ProgressSnapshotV2 对象）和可选 `progressDiagnostic`。`progressJson` 留作旧客户端兼容。执行历史返回相同观察合同对象，progress 中的阶段不能代替任务 `status`。

`GET /api/v1/scans/{id}/logs?after={sequence}&attempt={attempt}` 保留接口与轮询方式。新版返回 `format: scan_event_v2`、`execution`、`events[].event`（完整生成 ScanEventV2）、`nextAfter`、`total`（最大有效序号）、`recordCount`（有效唯一记录数）、`hasMore`、`invalidRecords` 和可选 `diagnostic`。兼容字段 n/timestamp/level/kind/message 是展示投影，n=sequence；message 不决定语义。老执行返回 `legacy_jsonl`，after 仍按原行号处理。JSONL 未结束的半行不推进游标，重复事件不重复展示，无效或异执行的记录不进入有效事件集合。

后台内部 v4 presence/claim 协商输入、结果和 ObservationCapabilities；v4 heartbeat 接收 ExecutionHeartbeat v1（execution、agentId、progress 对象或 null、可选 diagnostic），回包包括 status、revision 和 accepted。它继续采用 HTTP，不改变用户入口。lease_expired 表示所有者、执行编号、租约或终态不允许再续租；protocol_version_unsupported 表示合同版本不支持；进度校验失败不会通过解析 message 被认定为扫描结论。


## Web 扫描输入与知识文档（SDK 0.1.25）

`POST /api/v1/scans` 可附加 `scanInput`（`ScanInput` v1，浏览器的 `knowledge` 必须为空）、`knowledgeVersionIds` 和 `parentTaskId`。Server 解析并冻结文档及父扫描，选择输入合同版本 2；普通请求保留输入版本 1。调用仍由 Web 表单发起。

| 路径 | 行为 |
| --- | --- |
| `GET /api/v1/knowledge?q=&archived=false&page=1` | 固定每页 50 条，返回 items/total/page/pageSize；仅最新版本元数据。 |
| `GET /api/v1/knowledge?documentId=...&page=1` | 分页读取指定文档版本历史，归档后仍可读。 |
| `GET /api/v1/knowledge/versions/{id}` | 原始正文、版本、最新版本号、SHA-256 和时间。 |
| `POST /api/v1/knowledge` | title/content/baseVersion=0 创建文档。 |
| `POST /api/v1/knowledge/{id}/versions` | title/content/baseVersion 进行 CAS，生成新版本；冲突 409。 |
| `PUT /api/v1/knowledge/{id}/archive` | 必须明确 archived:true/false；归档与恢复均保留历史。 |
| `GET /api/v1/scans/{id}/parent-state` | 只返回经证据核验的父任务元数据，不返回私有 SDK 数据库。 |

读取沿用 read 权限，文档写入沿用 operate 与操作记录。正文最多 64 KiB，每次最多 8 个版本，浏览器不提交任意路径。完整参数/模式约束、私有父状态和版本协商见 [扫描输入与知识库基线](docs/SDK_INPUT_KNOWLEDGE.md)。


## SDK 原生结果、诊断和用量估算

归档结果 extensions.sdkNative 为独立 SDKNativeResult v1，含 Markdown/SARIF 引用、仓库历史、冻结用量估算和诊断。进度 extensions.sdkDiagnostics 为 SDKDiagnostics v1，完整日志以原产物下载接口读取。新增只读 sdk-cost-estimate 与 sdk-history-links 接口，不触发模型或外部写入；具体路径、历史与身份约束见 [原生结果基线](docs/SDK_NATIVE_RESULTS.md)。

## SDK 独立操作与原生会话事件

- `POST /api/v1/scans`：可传 `operation:"preflight"`，从本次 Web 配置新建原生预检，需要不可变源码快照。
- `POST /api/v1/scans/{id}/sdk-operations`：`kind:"preflight"`；或 `kind:"validate", findingId:"123", originAttempt:1, acknowledgeUnknownCost:true`。后者必须选择本来源已结束执行的现存漏洞，费用未知且不支持费用上限。
- `GET /api/v1/scans/{id}/sdk-operations?page=1`：关联操作每页 50 项，含总数与冻结来源。
- `GET /api/v1/scans/{id}/attempts/{attempt}/sdk-sessions?after=0`：最多 50 条/8 MiB；正式归档校验完整文件的身份、序号、数量、大小与哈希。没有归档结果时返回 uncommitted 快照，完整性未知；`download=1` 下载已保存的 partial JSONL。正式完整日志沿用 artifacts 路由下载。

读者可查看，operator/admin 可创建或取消；通用 retry 与预算设置不能绕过独立复核的未知费用确认。完整操作结果位于归档 ScanResult 的 `extensions.sdkOperation`，完整会话元数据位于 `extensions.sdkSession`，均有独立 Schema；基础结果版本不变。原生操作不生成扫描对比或 HTML/PDF 扫描报告（409），可下载其原生报告/完整 JSON。


## 扫描中断恢复（SDK 0.1.25）

`GET /api/v1/scans/{id}/recovery` 返回 `ScanRecoverySnapshot` v1，包含当前执行、断点续扫不支持的事实、提交结果核验、过期中断登记与重新执行资格。旧 Server 无此接口时，新 Web 保留历史读取并停用恢复写操作。

`POST /api/v1/scans/{id}/recovery` 接收 `ScanRecoveryAction` v1：`schemaVersion:1`、`execution:{taskId:string,attempt:number}`、`expectedStatus`、`action`、`resultSha256` 和 `acknowledgeReexecution`。`restore_result` 要求当前提交结果 SHA-256 且确认字段为 false；`restart` 要求确认 true、哈希 null；`interrupt_expired` 两者分别为 null / false。动作绑定原执行及状态，冲突返回 409；不接受 resume 或文件路径。恢复结果不调用 SDK、不增加 attempt，取消/超时保持原状态；重新执行继续执行费用/预算准入。

恢复记录通过快照及执行历史的 `resultRecovery` 读取，保留原错误、原完成信封、操作者、时间与提交哈希。权限、校验和测试边界见 [扫描中断恢复](docs/SCAN_RECOVERY.md)。旧 `/retry` 的兼容语义保留。
