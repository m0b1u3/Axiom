# DESIGN_PHILOSOPHY.md

# Security Console Design Philosophy

## Cold Precision

本项目是面向专业安全人员的 Security Console。

主要用户包括：

- Security Researcher
- Application Security Engineer
- Red Team
- Penetration Tester
- SOC Analyst
- Incident Responder
- Threat Hunter
- Security Platform Engineer
- AI Security Agent Operator

本产品不是：

- 普通 SaaS 后台
- CMS
- CRM
- 营销网站
- ChatGPT Wrapper
- Cyberpunk Demo
- Dribbble Concept
- Admin Template

UI 的设计目标不是第一眼“炫”。

而是第一眼让用户感觉：

> 这是一个成熟、可信、专业、复杂但有秩序的安全工作平台。

---

# 1. Product Character

整个产品必须始终保持以下视觉性格：

- Precise
- Cold
- Calm
- Dense
- Controlled
- Technical
- Operational
- Evidence-driven
- Serious
- Mature

不要变成：

- Soft
- Cute
- Playful
- Futuristic
- Decorative
- Consumer-like
- Marketing-heavy

---

# 2. Core Principle

产品 UI 不是围绕页面设计。

而是围绕以下八种信息设计：

1. Object
2. State
3. Relationship
4. Evidence
5. Boundary
6. Action
7. Risk
8. Time

任何视觉元素如果不能帮助表达以上内容，都应考虑删除。

---

# 3. Security Console ≠ Dashboard

不要默认把产品首页设计成 Dashboard。

避免：

```text id="rt0d84"
Critical          12

High              37

Active Tasks      18

Assets            2814

[ Donut Chart ]

[ Line Chart ]
```

Dashboard 不是产品价值本身。

安全人员真正关心的是：

```text id="3yk9ta"
现在什么正在发生？

哪里需要处理？

发现了什么？

证据是什么？

哪些只是推测？

什么被阻止了？

哪些动作需要审批？

执行边界是什么？

发生了什么变化？
```

产品首页应该优先考虑：

- Investigations
- Operations
- Findings
- Executions

而不是 KPI 展板。

---

# 4. Operational Workspace

主要页面应表现为 Workspace，而不是 Website。

推荐结构：

```text id="kfnfzh"
┌─────────────┬────────────────────────────────────────┬──────────────────────┐
│             │                                        │                      │
│ Navigation  │           Main Workspace               │      Inspector       │
│             │                                        │                      │
│             │                                        │                      │
│             │                                        │                      │
└─────────────┴────────────────────────────────────────┴──────────────────────┘
```

必要时：

```text id="ldc3wr"
Navigation
    │
    ├── Workspace
    │      ├── Toolbar
    │      ├── Data Surface
    │      └── Bottom Activity
    │
    └── Inspector
```

Inspector 是一级设计模式，而不是临时补充组件。

---

# 5. Flat Structural Design

主界面应具有较强平面结构。

不要：

```text id="2uyh5w"
Card
  Card
    Card
      Badge
      Card
```

推荐：

```text id="j2xbdk"
Pane

────────────────────

Section

────────────────────

Data

────────────────────
```

通过：

- spacing
- alignment
- typography
- divider
- background tone

建立层级。

不要依赖：

- shadow
- giant radius
- gradient
- floating cards

建立层级。

---

# 6. Cold Precision Geometry

安全产品的几何语言应该略微偏硬。

核心原则：

```text id="0dk7am"
Small radius
Sharp alignment
Clear split panes
Straight dividers
Compact controls
```

不要让所有东西软化成 SaaS Bubble UI。

主 Workspace：

```text id="b4k7j4"
radius: 0
```

Sidebar：

```text id="q81a75"
radius: 0
```

Inspector：

```text id="07ih29"
radius: 0
```

内部小组件可以使用少量圆角。

---

# 7. Neutral First

90% 以上界面使用 Neutral Color。

品牌色不是背景装饰。

品牌色仅用于：

- Active
- Selected
- Primary action
- Focus
- Progress
- Key visualization

安全状态颜色用于：

- Risk
- Execution state
- Policy result
- Evidence state

不要让整个页面成为：

```text id="t6ex0h"
Red
Orange
Green
Blue
Purple
```

组成的交通灯。

---

# 8. Security Color Is Signal

颜色必须是 Signal。

例如：

```text id="6vypzd"
Critical      red
High          orange-red
Medium        amber
Low           neutral-blue
Info          gray-blue
```

但是颜色应该只占一个组件很小比例。

推荐：

```text id="olgrft"
● HIGH
```

或者：

```text id="zatzch"
│ HIGH
```

而不是：

```text id="441jg5"
██████████████████████████
█    HIGH SEVERITY       █
██████████████████████████
```

---

# 9. Information Density

专业用户需要信息。

不要害怕密度。

应该害怕的是：

> 没有结构的密度。

推荐数据密度：

```text id="tvzrio"
Sidebar row      30–32px
Data row         32–36px
Tree row         28–30px
Toolbar          36–40px
Input            30–34px
Button           30–34px
Tab              32px
Inspector row    28–32px
```

目标是在 1440px 桌面环境下一屏承载足够多的工作信息。

---

# 10. Typography Creates Hierarchy

层级优先通过 Typography 建立。

顺序：

1. Position
2. Typography
3. Spacing
4. Divider
5. Background
6. Border
7. Shadow

Shadow 是最后手段。

---

# 11. Titles Should Be Quiet

不要：

```text id="7chvj4"
48px

Security Operations
```

产品 Console 中的 Page Title：

```text id="cgf47n"
20–24px
```

已经足够。

安全产品的重要内容应该是 Data Surface，而不是标题。

---

# 12. Monospace Is Functional

Monospace 用于：

- IP
- Port
- Hash
- URL
- HTTP
- Payload
- Code
- UUID
- Artifact ID
- Command
- Timestamp when necessary

不要全页面 monospace。

否则会变成：

> Hacker Theme。

---

# 13. Core Security Objects

整个产品的视觉语言围绕真实对象建立。

包括：

```text id="72412a"
Workspace
Investigation
Execution
Task
Agent
Worker
Target
Asset
Service
Finding
Evidence
Artifact
Hypothesis
Observation
Policy
Scope
Approval
Action
Runtime
Session
Event
```

这些词比：

```text id="cx1po7"
Card
Widget
Tile
Box
Section
```

更重要。

---

# 14. Evidence Is First-Class

安全产品最大的视觉差异之一：

Evidence 必须是一等公民。

Finding 不等于一个风险名称。

Finding：

```text id="29gxzi"
Finding
 ├─ Claim
 ├─ Evidence
 ├─ Reproduction
 ├─ Confidence
 ├─ Source
 ├─ Observation
 ├─ Artifact
 └─ Timeline
```

用户必须能够判断：

```text id="jy43q9"
Why should I believe this?
```

---

# 15. Evidence ≠ Inference

UI 必须清晰区分：

```text id="wgi8v5"
Observed
Inferred
Suspected
Verified
Reproduced
Contradicted
Unknown
```

绝不能让 AI 推断看起来像确定事实。

例如：

```text id="txlvt7"
Observed

Response delay +4.82 seconds
```

和：

```text id="ubc8t4"
Hypothesis

Possible time-based SQL injection
Confidence 71%
```

必须具有不同视觉语义。

---

# 16. Findings Are Not Cards

Finding 列表应优先：

```text id="pcr01j"
Severity │ Finding             │ Target             │ Evidence │ Confidence │ Age
─────────┼─────────────────────┼────────────────────┼──────────┼────────────┼─────
Critical │ Auth bypass         │ admin.internal     │ 7        │ 98%        │ 2m
High     │ SQL injection       │ /api/user?id=      │ 4        │ 94%        │ 8m
Medium   │ Weak CORS policy    │ api.example.com    │ 2        │ 81%        │ 1h
```

点击后打开 Inspector。

---

# 17. Inspector Is Persistent Context

Inspector 负责：

- Details
- Metadata
- Evidence
- Actions
- Relationships
- History

避免频繁：

```text id="anrfai"
List
→ Details page
→ Back
→ List
```

专业用户需要保持当前位置。

---

# 18. Progressive Disclosure

不要一次展示所有信息。

第一层：

```text id="lcsjnj"
Finding
Severity
Target
Verification
Evidence count
```

第二层：

```text id="dim7bp"
Inspector
```

第三层：

```text id="84ickr"
Deep investigation
Raw request
Raw response
Artifact
Graph
```

这使高密度 UI 仍然能够保持秩序。

---

# 19. Execution Is Temporal

Execution 不只是：

```text id="v8cm39"
Running
```

而应该表现为时间结构：

```text id="vrrf8w"
14:32:01  target.resolve       complete
14:32:02  scope.validate       allowed
14:32:02  broker.resolve       complete
14:32:03  http.request         complete
14:32:04  hypothesis.update    generated
14:32:06  validate.sql         running
```

执行过程本身就是产品。

---

# 20. Security Boundary Is Visible

Scope、Policy、Authorization 不能只存在后台。

任何危险或外部执行都必须可以追溯：

```text id="nw8kzj"
Requested Target
Resolved Target
Authorized Scope
Network Boundary
Policy
Approval
Worker
Runtime
```

用户必须知道：

> 为什么这次动作被允许？

或者：

> 为什么被拒绝？

---

# 21. Policy Block Is Not Error

以下是不同概念：

```text id="5e5gtx"
Execution Failed

Scope Blocked

Policy Denied

Approval Required
```

不要全部用红色 Error Toast。

Scope Blocked 甚至说明系统正常工作。

---

# 22. Approval Is Product Interaction

高风险动作应该具有专门的 Approval Gate。

例如：

```text id="cxbw6f"
Action requires approval

POST /admin/delete-user

Target
admin.example.com

Reason
State-changing request

Policy
production-write-protected

Requested by
agent-worker-04

[Reject]                     [Approve once]
```

它不应该只是浏览器 confirm()。

---

# 23. Security Graph

Graph 只在表达关系时存在。

适合：

```text id="hj1g94"
Asset
Identity
Service
Vulnerability
Credential
Finding
Evidence
Attack Path
```

不适合为了“高级”而随便画节点。

Graph 必须允许：

- inspect
- filter
- isolate
- expand
- trace
- navigate

---

# 24. AI Should Be Embedded

避免独立：

```text id="dfj2ki"
Ask AI ✨
```

AI 应存在于工作流中：

```text id="p9a4fl"
Hypothesis generated

Evidence correlated

Next validation proposed

Finding summarized

Execution plan changed
```

智能体不是客服机器人。

智能体是系统参与者。

---

# 25. Agent Visibility

如果 Agent 参与安全动作，必须可以看到：

```text id="emc3ou"
Agent
Current goal
Current action
Current state
Tool
Worker
Policy
Evidence produced
Decision
```

不要只显示：

```text id="5zrpvp"
AI is thinking...
```

---

# 26. Blackboard Design

Blackboard 不应该设计成聊天记录。

应该设计成结构化工作区。

例如：

```text id="riivzr"
Hypotheses

H-014   SQL injection exists                       82%
H-015   Endpoint protected by WAF                  61%
H-016   Delay caused by backend queue              24%

Evidence

E-481   latency differential                       supports H-014
E-482   WAF fingerprint                            supports H-015
E-483   control request normal                     contradicts H-016
```

重点是：

- Relationship
- Confidence
- Evidence
- Contradiction

而不是消息气泡。

---

# 27. Activity ≠ Terminal

Activity Stream 应有结构。

例如：

```text id="dmx3i7"
14:32:01   SCOPE    Target validated
14:32:02   DNS      api.example.com → 10.2.4.18
14:32:03   HTTP     POST /api/user
14:32:04   FINDING  SQL injection suspected
```

而不是全绿色终端。

Terminal 只用于真正 shell。

---

# 28. No Fake Security Aesthetic

禁止使用：

- Matrix code
- Hacker green
- Skull icon
- Giant shield
- Padlock illustration
- World map network animation
- Blue glowing grid
- Radar scan
- Hexagon sci-fi panels
- Fake terminal background
- Neon border
- Pulsing cybersecurity globe

这些不是专业感。

---

# 29. Microstates Create Premium Feel

商业产品高级感主要来自状态完整性。

每个异步对象至少考虑：

```text id="nekfit"
Idle
Queued
Preparing
Running
Waiting
Retrying
Paused
Blocked
Needs Approval
Completed
Partial
Failed
Cancelled
Expired
Unknown
```

还包括：

```text id="7p6g3u"
Hover
Selected
Focused
Disabled
Stale
Offline
Unavailable
```

---

# 30. Contextual Actions

不要把每个操作都永久显示。

默认只显示核心 Action。

更多操作通过：

- Hover
- Context menu
- Command palette
- Inspector
- Keyboard shortcut

暴露。

这既提高密度，又降低视觉噪音。

---

# 31. Keyboard-First

专业用户需要：

```text id="kcbfkd"
↑ ↓
J K
Enter
Esc
⌘K
⌘P
/
```

支持：

- Navigate
- Open
- Search
- Filter
- Command
- Close inspector

键盘能力本身也是商业成熟度的一部分。

---

# 32. Command Palette

Command Palette 应支持领域操作：

```text id="8xp9yd"
Open investigation
Run validation
Inspect evidence
Filter high severity
Assign finding
Open artifact
Switch workspace
Pause execution
```

而不是只有页面跳转。

---

# 33. Empty States

专业安全产品的 Empty State 应解释工作状态。

例如：

```text id="bs2gja"
No verified findings

Potential findings will appear here after
evidence has been validated.

Run validation
```

不要放火箭和插画。

---

# 34. Errors Must Be Operational

错误消息尽量包含：

```text id="stze4b"
What failed
Where
Why
Whether retry is safe
What user can do
```

例如：

```text id="12ua9a"
Network request blocked

Resolved address 10.0.24.18 is outside the
authorized target scope.

Scope: external-production
Policy: network.scope.enforce
```

而不是：

```text id="srtjdb"
Request failed.
```

---

# 35. Dark Mode

Dark Mode 不是 Hacker Mode。

它应该是：

```text id="atza0c"
charcoal
graphite
cool gray
restrained signal colors
```

不是：

```text id="wq94rq"
pure black
neon green
glow
```

---

# 36. Premium Means Deliberate

“高级”不是：

```text id="wa139o"
blur
gradient
shadow
animation
radius
```

“高级”来自：

```text id="q1gqf8"
alignment
density
state completeness
microinteraction
data formatting
empty state
loading state
error semantics
keyboard interaction
context preservation
domain-specific components
```

---

# 37. Recognition Test

每完成一个页面，问：

> 把 Logo 去掉之后，这个页面还能看出来是 Security Product 吗？

如果不能：

设计不够领域化。

再问：

> 把文案换掉以后，它是不是可以直接变成 CRM / Project Management / AI SaaS？

如果是：

设计仍然太通用。

---

# 38. AI Smell Test

重点检查：

- 是否出现不必要 Card
- 是否出现 Bento
- 是否所有东西都有圆角
- 是否大量 Gradient
- 是否所有 Section spacing 相同
- 是否每个标题都配 Icon
- 是否图标来自同一套默认 Lucide 组合
- 是否首页由 KPI Card 组成
- 是否信息密度异常低
- 是否存在大量无意义辅助文案
- 是否使用 “AI-powered”
- 是否存在 Sparkle
- 是否看起来像 shadcn starter

只要明显存在三个以上，应重新审视页面结构。

---

# 39. Final Standard

最终产品应该给用户这样的感觉：

> 这里的每一个像素都有工作意义。

不是：

> 这里的设计效果很多。

真正优秀的 Security Console 应该让专业用户在复杂数据中依然感觉：

**稳定、可控、可信、清晰。**