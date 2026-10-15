# COMPONENTS.md

# Security Console Component Language

本文件定义产品的领域级 UI 组件。

开发页面时，应首先判断内容属于什么业务对象，再选择组件。

不要首先考虑：

> 用 Card 还是 Accordion？

应该首先考虑：

> 这是 Finding、Evidence、Execution，还是 Policy Decision？

---

# 1. ApplicationShell

标准结构：

```text id="c537i4"
ApplicationShell
├── GlobalSidebar
├── Workspace
│   ├── WorkspaceHeader
│   ├── WorkspaceToolbar
│   └── PrimaryDataSurface
└── Inspector
```

主 Shell 不使用：

- rounded container
- shadow
- floating card

不同区域通过 divider 分割。

---

# 2. GlobalSidebar

用于产品级导航。

结构：

```text id="2puk0g"
Product
Workspace

Investigations
Executions
Findings
Targets
Artifacts

Infrastructure
Workers
Runtime
Policies

Settings
```

规则：

- 宽度约 236px
- row 30px
- icon 14–16px
- 不强制每项 icon
- Active 状态使用 subtle background
- 不使用 pill navigation
- 不使用大 Radius
- 不使用彩色 icon

---

# 3. WorkspaceHeader

承担：

- Current object
- Scope
- Key state
- Main action

示例：

```text id="4snab6"
Investigation #TF-1842                            Running

api.example.com
Production · external-web

                                     Pause   ···
```

禁止：

```text id="ddq3jz"
Welcome back!
Here's what's happening with your security today.
```

这种 SaaS 模板文案。

---

# 4. WorkspaceToolbar

统一处理：

- Search
- Filters
- Sort
- View
- Bulk action

示例：

```text id="orvxo7"
[ Search findings... ]

Severity: Critical, High
Status: Open
Environment: Production

                                            38 results
```

Toolbar 高度：

```text id="3z5cyt"
36–40px
```

---

# 5. SecurityDataGrid

用于：

- Findings
- Executions
- Assets
- Workers
- Targets
- Artifacts
- Policies

必须支持：

- column resize
- sorting
- filters
- selected row
- keyboard navigation
- contextual actions
- loading
- empty
- partial loading

推荐：

```text id="c7ih4n"
Severity │ Finding          │ Target       │ State      │ Confidence │ Last Seen
```

不要把天然表格数据转换成 Card。

---

# 6. FindingRow

FindingRow 至少表达：

```text id="q917qk"
Severity
Title
Target
Verification state
Evidence count
Confidence
Age
```

默认：

```text id="cag06s"
● HIGH   SQL injection
         POST /api/user?id=

         Verified · 4 evidence · 94%              7m
```

Hover：

出现：

```text id="a8c37s"
Validate
Assign
Suppress
···
```

不要让 Row Height 因 Hover 发生变化。

---

# 7. FindingInspector

结构推荐：

```text id="q1b166"
SQL Injection

HIGH
Verified

Target
POST /api/user?id=

Confidence
94%

Evidence
4

────────────────────

Summary

────────────────────

Evidence

────────────────────

Reproduction

────────────────────

Timeline
```

主要操作：

```text id="0nfi8p"
Validate again
Open investigation
Assign
Suppress
```

Inspector 不使用整体 Card。

---

# 8. SeverityIndicator

Severity 只使用少量视觉信号。

推荐：

```text id="hjyi0b"
● Critical
● High
● Medium
```

或者：

```text id="1omj21"
│ Critical
```

禁止整行 Severity Background。

---

# 9. EvidenceViewer

EvidenceViewer 是核心组件。

支持类型：

```text id="k747gl"
HTTP Exchange
Code Location
Runtime Observation
Log
Network Observation
File
Screenshot
Artifact
Diff
Trace
```

统一 Header：

```text id="vch9iu"
E-481

HTTP differential
Observed

worker-04 · 14:32:18
```

主体按照 Evidence 类型渲染。

---

# 10. EvidenceState

必须统一：

```text id="vyjb5x"
Observed
Verified
Inferred
Contradicted
Unknown
```

Evidence State 与 Finding Severity 不是同一个维度。

---

# 11. EvidenceChain

用于表达：

```text id="74uuf6"
Observation
   ↓
Evidence
   ↓
Hypothesis
   ↓
Validation
   ↓
Finding
```

展示重点：

- source
- timestamp
- relationship
- confidence
- contradiction

不要做成聊天记录。

---

# 12. HttpExchangeViewer

必须是专业安全产品级组件。

Tabs：

```text id="zysr4g"
Request
Response
Timing
Evidence
Raw
```

Request：

```text id="7byyd3"
POST /api/user?id=1 HTTP/1.1
Host: api.example.com
Authorization: ••••••••
```

支持：

- syntax highlighting
- wrap
- raw
- copy
- search
- sensitive value masking
- byte size
- timing

---

# 13. HttpDiffViewer

用于漏洞验证特别重要。

示例：

```text id="c6ct75"
Control                       Payload

status  200                   200
length  841                   842
time    182ms                 5.03s
```

下面展示：

```text id="487k7c"
+4.85 s latency differential
```

不要只给 AI 总结结果。

---

# 14. CodeEvidenceViewer

结构：

```text id="gls03m"
src/controllers/UserController.ts

118    const id = req.query.id;
119
120    db.query(
121      `SELECT * FROM users WHERE id = ${id}`
122    );
```

支持：

- file path
- lines
- highlight
- blame/source metadata
- open full file
- related findings

---

# 15. ExecutionWorkspace

Execution 是一级工作区。

推荐：

```text id="5hqvij"
Execution #EX-10293

Running · 18m 42s

Target
api.example.com

Scope
external-production

Worker
worker-07
```

主体：

```text id="1bfdvs"
Execution Tree / Timeline
```

右侧：

```text id="5xkwrl"
Selected Step Inspector
```

底部可选：

```text id="xc5vyz"
Activity / Artifacts
```

---

# 16. ExecutionStep

字段：

```text id="xj0hfq"
Timestamp
Step
Tool
State
Duration
Worker
Result
```

例如：

```text id="u0gh35"
14:32:04   scope.validate        allowed        18ms
14:32:05   http.send             completed     481ms
14:32:06   analyze.diff          completed     92ms
14:32:06   validate.sql          running        8.2s
```

---

# 17. ExecutionTimeline

Timeline 不使用巨大的圆形节点。

推荐小 Marker：

```text id="ygr19o"
│
● target.resolve
│
● scope.validate
│
● http.send
│
● evidence.store
│
```

尺寸保持紧凑。

---

# 18. ActivityStream

结构：

```text id="t59i2h"
TIME       TYPE       MESSAGE
```

示例：

```text id="da093v"
14:32:01   SCOPE      target allowed
14:32:02   DNS        api.example.com → 18.2.4.8
14:32:04   HTTP       POST /api/users
14:32:09   FINDING    SQL injection suspected
```

可点击 Event 查看详情。

---

# 19. TargetIdentity

统一展示：

```text id="el94cq"
api.example.com

Resolved
18.24.2.11

Environment
Production

Scope
external-web

Authorization
Allowed
```

Target 不只是一个字符串。

---

# 20. ScopeBoundary

ScopeBoundary 应始终清晰。

例如：

```text id="q3k4fi"
Authorized

api.example.com
*.example.com

Network
External only

Mutation
Read-only
```

如果阻止：

```text id="9z49bl"
Blocked by scope

10.0.14.8 is not covered by
external-production.
```

---

# 21. PolicyDecision

结构：

```text id="9pyon7"
Allowed

Policy
network.external.readonly

Rule
target.in.authorized_scope

Evaluated
14:32:01.482
```

Denied 时需要：

```text id="7im2eh"
Reason
Rule
Policy
Requested action
```

---

# 22. ApprovalGate

用于：

- Destructive action
- Write operation
- Privilege escalation
- Credential action
- Lateral movement
- High-risk network action

结构：

```text id="ls8s73"
Approval required

Action
POST /admin/user/delete

Risk
State-changing operation

Target
admin.example.com

Policy
production-write-protected

Requested by
agent-04

Reject                      Approve once
```

Approval Gate 必须独立于普通 Dialog。

---

# 23. WorkerPresence

显示：

```text id="adwdci"
worker-07

Active
node-ap-northeast-1

Current
validate.sql

Runtime
sandbox-142

Last heartbeat
2s ago
```

不要做成头像 Team Member。

---

# 24. AgentStatus

Agent 不是聊天机器人。

展示：

```text id="pygtif"
Agent

Goal
Validate authentication boundaries

Current action
Testing session reuse

State
Waiting for HTTP response

Worker
worker-03
```

---

# 25. HypothesisList

标准结构：

```text id="oi0tic"
Confidence │ Hypothesis                       │ Support │ Against
───────────┼──────────────────────────────────┼─────────┼────────
82%        │ SQL injection exists             │ 4       │ 1
61%        │ Endpoint protected by WAF        │ 2       │ 0
24%        │ Backend queue causes delay       │ 1       │ 3
```

---

# 26. HypothesisInspector

显示：

```text id="fyhuah"
Hypothesis H-014

Possible time-based SQL injection

Confidence
82%

Supporting evidence
E-481
E-482
E-491

Contradicting evidence
E-489

Next validation
Boolean differential
```

---

# 27. SecurityGraph

只应用于真实关系。

Node 类型可包括：

```text id="2g9adj"
Target
Service
Identity
Credential
Finding
Evidence
Artifact
Worker
```

交互：

```text id="rqyp04"
Select
Expand
Hide
Trace path
Inspect
Pin
Filter neighbors
```

禁止默认添加：

- Glow
- Animated particles
- Neon lines

---

# 28. AttackPath

AttackPath 强调 Sequence。

例如：

```text id="d1h7q0"
Internet
   ↓
Public API
   ↓
SSRF
   ↓
Metadata Service
   ↓
IAM Role
   ↓
Object Storage
```

每一步必须可以查看 Evidence。

---

# 29. ArtifactViewer

统一处理：

```text id="19omey"
Request
Response
File
Log
Screenshot
Binary metadata
JSON
Trace
Code
```

Artifact 应带：

```text id="9o60dh"
Artifact ID
Type
Source
Created
Execution
Hash
Size
```

---

# 30. InspectorSection

Inspector 内禁止大量 Card。

标准：

```text id="eiwkqp"
Target
────────────

Method        POST
Path          /api/user
Host          api.example.com
```

Section 之间使用：

- whitespace
- divider

而不是 Card。

---

# 31. PropertyList

所有 metadata 优先：

```text id="wwu1i8"
Label            Value
```

Label 固定宽度。

Value 可选择、复制、点击。

---

# 32. StatusIndicator

状态必须使用统一组件。

支持：

```text id="qx813m"
Running
Queued
Waiting
Paused
Blocked
Approval
Completed
Partial
Failed
Cancelled
```

不要每个功能自己画 Badge。

---

# 33. ContextMenu

领域级操作应进入 ContextMenu。

例如 Finding：

```text id="vg1s0c"
Open
Open in investigation
Validate again
Assign
Copy link

────────────

Suppress
Mark false positive
```

危险操作位于底部。

---

# 34. CommandPalette

命令可以作用于领域对象：

```text id="26lnsd"
> Validate selected finding
> Open evidence E-481
> Pause execution
> Filter critical findings
> Open target api.example.com
```

---

# 35. EmptyState

标准结构：

```text id="6cdq9z"
No active executions

Executions will appear when a task is
dispatched to a worker.

New execution
```

不使用 Illustration。

---

# 36. LoadingState

执行类组件不得只有 Spinner。

示例：

```text id="2ne23y"
Resolving target...

Validating authorized scope...

Waiting for worker...
```

具体阶段比百分比更有价值。

---

# 37. Toast

Toast 只用于短暂反馈。

适合：

```text id="2u6hba"
Copied
Filter saved
Finding assigned
```

不适合：

```text id="8ovy6h"
Scope violation
Execution failure
Approval request
```

这些必须进入主要 UI。

---

# 38. Dialog

Dialog 只用于：

- Confirmation
- Create
- Destructive action
- Important settings

普通详情不进入 Dialog。

---

# 39. Charts

只在问题明确时使用。

允许：

```text id="16k5hk"
Finding trend
Policy block trend
Worker utilization
Execution latency
Severity change
```

不要：

```text id="1hfoej"
为了 Dashboard 完整而加入图表。
```

---

# 40. Product Signature Components

为了避免产品变成 Generic Admin，至少应建立三个高度领域化的视觉组件。

推荐：

```text id="mo8vym"
ExecutionWorkspace

EvidenceChain

HypothesisBlackboard
```

或者：

```text id="y8mkus"
SecurityGraph

ScopeBoundary

FindingEvidence
```

这些组件应该成为产品视觉识别度来源。

---

# Final Rule

组件体系最终不应该让开发者思考：

> 我需要什么 UI 元素？

而应该思考：

> 用户正在处理什么安全对象？