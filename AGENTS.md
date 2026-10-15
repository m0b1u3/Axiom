# AGENTS.md

# Frontend Implementation Rules

本文件适用于：

- Codex
- Claude Code
- Cursor
- AI coding agents
- Human frontend contributors

任何 UI 开发前必须阅读：

```text id="9flmvi"
DESIGN_PHILOSOPHY.md
COMPONENTS.md
DESIGN_TOKENS.css
```

它们具有高于单页面临时设计的优先级。

---

# 1. Do Not Design From Scratch

不得因为任务描述简单就自行创造新视觉风格。

优先：

```text id="29o7lh"
Existing domain component
→ Existing UI primitive
→ Existing layout pattern
→ New pattern only if necessary
```

---

# 2. Product Object First

写 JSX 前先识别：

```text id="dvi6uc"
Primary object
Secondary objects
State
Relationship
Actions
Evidence
Boundary
Time
```

如果页面主要处理 Finding：

应使用 Finding 相关组件。

不要创建：

```text id="uw1ywa"
GenericDetailsCard
```

---

# 3. Never Default to Dashboard

用户说：

```text id="l9o23n"
做一个首页
做一个安全概览
```

不得自动生成：

```text id="n7c2bi"
4 KPI cards
2 charts
Recent activity
```

先根据真实工作流决定首页信息。

---

# 4. Never Default to Cards

Card 不是默认布局组件。

在创建 Card 前必须判断：

```text id="9v1vh7"
Is this content semantically self-contained?
```

如果否，使用：

- section
- pane
- list
- table
- property list
- inspector
- timeline

---

# 5. Prohibited Generic Patterns

除非明确要求，禁止：

```text id="201lcs"
Bento grid
Gradient hero
Gradient text
Glassmorphism
Glow
Floating orb
Decorative grid background
Large rounded card
Huge statistics card
Pill navigation
AI sparkle
Cybersecurity shield illustration
Hacker terminal aesthetic
```

---

# 6. No AI Decoration

以下图标不得因为“科技感”随意使用：

```text id="ycqxjq"
Sparkles
Bot
Brain
Wand
Rocket
Zap
ShieldCheck
Globe
Network
```

如果它确实代表业务对象则允许。

---

# 7. Security UI Is Neutral First

页面大面积使用 Neutral。

不要：

```text id="oi48of"
每个状态都是彩色背景
每个模块使用不同品牌色
每张卡使用渐变
```

颜色是信号，不是 decoration。

---

# 8. Use Tokens

禁止：

```css id="hqrhpt"
padding: 13px;
border-radius: 11px;
color: #64748b;
```

如果设计 Token 已经存在。

优先：

```css id="6c72cd"
padding: var(--space-3);
border-radius: var(--radius-sm);
color: var(--text-secondary);
```

---

# 9. Do Not Introduce Arbitrary Radius

禁止随意：

```text id="ygf1d8"
rounded-xl
rounded-2xl
rounded-3xl
```

必须遵循 Token。

主 Shell、Workspace、Sidebar、Inspector：

默认没有 radius。

---

# 10. Do Not Introduce Decorative Shadow

普通：

```text id="t3v8i6"
panel
table
sidebar
toolbar
```

不使用 Shadow。

只有：

```text id="b3c69z"
Popover
Dropdown
Dialog
Dragged object
```

允许 Shadow。

---

# 11. Density Is Intentional

默认桌面专业应用。

不要自动采用 Mobile SaaS Control Size。

推荐：

```text id="oxnyza"
Input        32px
Button       30–34px
Row          32–36px
Toolbar      36–40px
Sidebar row  30px
```

---

# 12. Do Not Add Padding to Fix Layout

如果页面看起来乱，不要一直：

```text id="i8ja15"
add padding
add gap
add margin
```

先检查：

```text id="fgigtn"
alignment
grouping
hierarchy
columns
container structure
```

---

# 13. Avoid Mechanical Spacing

AI 很容易：

```text id="vgfulf"
gap-6
p-6
space-y-6
```

整页重复。

必须根据关系决定间距。

紧密相关：

```text id="xudush"
4–8px
```

同组：

```text id="024yfp"
8–12px
```

不同 Section：

```text id="yf30wy"
16–24px
```

---

# 14. Typography Before Containers

如果层级不明显：

第一步不是添加 Card。

先调整：

```text id="jkeskb"
position
font weight
font size
text tone
spacing
divider
```

---

# 15. Tables Are Good

不要因为：

```text id="k7zf39"
table looks boring
```

把安全数据转换成 Card。

专业安全产品天然需要 Data Grid。

重点做好：

- selection
- hover
- keyboard
- filtering
- sorting
- column resize
- inspector

---

# 16. Preserve Context

从列表打开详情：

优先：

```text id="cft5ns"
Inspector
Split View
```

而不是：

```text id="90kouz"
route to another full page
modal
```

除非详情本身是独立 Workspace。

---

# 17. Domain State First

任何对象必须考虑完整状态。

Execution：

```text id="37d6tz"
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
```

Finding：

```text id="mkymk0"
Suspected
Observed
Verified
Reproduced
False Positive
Suppressed
Resolved
```

---

# 18. Security Semantics Matter

不得混淆：

```text id="mvu83f"
Error
Block
Deny
Warning
Approval
Failure
```

例如：

Scope Blocked ≠ System Failure。

---

# 19. Evidence Must Be Visible

安全结论旁边应该尽可能存在：

```text id="kjowhd"
Evidence
Confidence
Source
Timestamp
Execution
Artifact
```

不能只有 AI Summary。

---

# 20. AI Inference Must Be Marked

模型生成的内容不能伪装成已验证事实。

明确使用：

```text id="gscx1h"
Inferred
Hypothesis
Suggested
Confidence
```

验证后才变成：

```text id="786925"
Observed
Verified
Reproduced
```

---

# 21. Avoid Chat UI

除非产品功能本身就是聊天，否则不要：

```text id="xz0jgr"
Message bubble
Assistant avatar
Ask AI
AI sparkle
```

Agent 交互优先表达：

```text id="qr708s"
Goal
Action
Evidence
Decision
Next step
```

---

# 22. Execution UI Must Show Work

禁止：

```text id="tot0c5"
AI is thinking...
```

如果系统知道阶段，必须显示：

```text id="9eio73"
Resolving target
Validating scope
Dispatching worker
Executing request
Collecting evidence
Evaluating hypothesis
```

---

# 23. Long Running Work

长任务必须支持：

```text id="49orxo"
Current step
Elapsed time
Worker
Pause/Cancel if supported
Partial output
Errors
```

---

# 24. Error Messages Must Be Domain Specific

禁止默认：

```text id="sdw9j5"
Something went wrong.
```

优先：

```text id="0xooku"
Request blocked by Scope Guard

Resolved IP 10.0.12.8 is outside
authorized range 203.0.113.0/24.
```

---

# 25. No Fake Data

不得为了填充界面生成：

```text id="ef2po4"
fake chart
fake vulnerability
fake percentage
fake activity
fake AI insight
```

开发 Preview 数据除外，但必须明确属于 Fixtures / Mock。

---

# 26. Empty State Is Valid UI

真实没有数据时展示 Empty State。

不要为了“高级”填入模拟内容。

---

# 27. Hover Does Not Move Things

禁止：

```text id="yqvb4u"
hover:scale
hover:-translate-y
hover:shadow-xl
```

使用：

```text id="4u9k00"
background
border
text
```

变化。

---

# 28. Motion Must Have Meaning

动画只表达：

```text id="vpox7p"
state change
open/close
selection
progress
navigation
```

禁止：

```text id="hsek42"
floating
breathing
infinite glow
ambient animation
```

---

# 29. Contextual Actions

不要把所有操作永久展示。

Row Hover：

```text id="eh7fg7"
primary contextual actions
```

ContextMenu：

```text id="ib3rkj"
secondary actions
```

Inspector：

```text id="bhdse0"
full action set
```

---

# 30. Keyboard Navigation

任何高频表格和列表应评估：

```text id="nl183s"
↑ ↓
J K
Enter
Esc
```

全局：

```text id="b0ke1s"
⌘K
/
```

---

# 31. Accessibility Is Required

必须处理：

- focus
- keyboard
- aria
- contrast
- semantic markup
- disabled state

不能只做 Mouse UI。

---

# 32. Do Not Overuse Badges

Badge 仅用于：

```text id="n9pa93"
Status
Severity
Compact category
```

普通属性使用 Text。

---

# 33. Do Not Overuse Icons

如果删除 Icon 后依然完全可理解，应判断 Icon 是否必要。

尤其避免：

```text id="v7jrvj"
icon + label
```

在每一行重复出现。

---

# 34. Do Not Overuse Tooltips

基础信息不应该依靠 Tooltip 才能理解。

Tooltip 用于：

- abbreviation
- secondary explanation
- shortcut
- uncommon icon

---

# 35. Inspector Has Fixed Logic

Inspector 一般包含：

```text id="qav41v"
Object header
Primary state
Primary metadata
Domain sections
Actions
History
```

不要在 Inspector 内再堆 Card。

---

# 36. Raw Security Data Must Be Accessible

任何经过格式化或 AI 总结的信息，如果存在 Raw 数据，应提供访问入口。

例如：

```text id="f1o79m"
Formatted
Raw
```

专业用户必须能看到原始事实。

---

# 37. Design For Investigation

相关对象应该能够自然跳转：

```text id="8ouvc1"
Finding
→ Evidence
→ Execution
→ Target
→ Artifact
```

不要让每个模块成为信息孤岛。

---

# 38. Product Signature

设计新功能时问：

```text id="251fvx"
Does this strengthen our product-specific UI language?
```

优先复用：

- Evidence
- Execution
- Hypothesis
- Boundary
- Investigation

这些产品核心概念。

---

# 39. Screenshot Review Required

完成较大 UI 修改后，应在真实浏览器环境检查。

截图后主动审查：

```text id="342e5w"
Does this look AI-generated?

Does this look like shadcn?

Does this look like generic SaaS?

Are there unnecessary cards?

Are controls oversized?

Is spacing mechanically uniform?

Are there too many rounded surfaces?

Is information density appropriate?

Are state and hierarchy obvious?

Can a security professional scan this quickly?
```

---

# 40. AI Smell Removal Pass

如果 UI 看起来 AI 味明显，按以下顺序处理：

```text id="u1v39t"
1. Remove decorative cards
2. Remove excessive radius
3. Remove decorative icons
4. Remove gradients
5. Reduce oversized padding
6. Increase useful information density
7. Improve typography hierarchy
8. Improve alignment
9. Replace generic components with domain components
10. Add missing states and contextual interactions
```

不要通过增加装饰解决 AI 味。

---

# 41. Final Review Questions

提交前必须判断：

```text id="sfkwdo"
Can users understand the current state immediately?

Can users identify what requires action?

Can users trace a conclusion back to evidence?

Can users distinguish observed facts from AI inference?

Can users see execution boundaries?

Can users preserve context while inspecting details?

Does the UI support professional keyboard/mouse workflows?

Does the interface look designed for security work?

Would this still look like our product without the logo?
```

---

# 42. Ultimate Constraint

Never build:

> A beautiful dashboard with security data.

Build:

> A professional security working environment.

The product should feel engineered, not generated.