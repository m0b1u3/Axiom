---
name: Axiom component audit execution
description: Bounded Operate extension for frozen-plan launches, component execution receipts, budget reservations and original native evidence.
mode: Operate
primary_target: "code-audit-web/src/components/ComponentExecutions.vue"
related_targets: ["code-audit-web/src/components/ComponentScanLaunch.vue", "code-audit-web/src/components/ExecutionResult.vue", "code-audit-web/src/utils/component-scans.ts", "code-audit-web/src/views/ComponentPlans.vue", "code-audit-web/src/views/ScanDetail.vue"]
---

# Surface: Component audit execution

## Overview

This is the implemented frozen-plan execution extension as of 2026-10-17. It serves security engineers launching and investigating component audits within the existing Operate workspace. It preserves the approved C / Cold Precision identity and the governing [design philosophy](../DESIGN_PHILOSOPHY.md), [component language](../COMPONENTS.md), [tokens](../DESIGN_TOKENS.css) and [DESIGN.md](../../DESIGN.md). It introduces no new visual world or global tokens. [PRODUCT.md](../../PRODUCT.md) supplies product context; [SDK_COMPONENT_SCANS.md](../../docs/SDK_COMPONENT_SCANS.md) defines the execution and cost contract.

The operator opens a frozen version in [Component plans](component-plans.md), selects a model and execution limits, acknowledges unknown native matching fees and explicitly starts a Standard component audit. Planning and freezing remain separate from execution. This entry uses the frozen complete source and component boundaries; it does not inherit Git base/head differential scope or automatically request planning suggestions. Later draft changes cannot change an existing task.

| Domain object | Surface role |
| --- | --- |
| Frozen plan version | Launch boundary: plan/version, document SHA-256, immutable source SHA-256 and optional HEAD. |
| Parent task / execution attempt | One task, cumulative budget and retained execution history; each attempt has its own fixed authorization and selected batch. |
| Component receipt | Original component index, state, phase, activity, reservation, observed cost, final cost, SDK Scan ID and result artifact. |
| Native matching result | Batch-scoped model inference with original counts, grouping and uncertain matching evidence. |
| Finding binding | Original execution/attempt, component, frozen plan/source and raw result identity. |

The Go Server + Go Agent + Node Scanner architecture, HTTP transport, artifacts, JSONL activity, existing authentication and historical result readers remain in place. Components are records within one parent execution rather than independent queue tasks that each receive the full parent allowance.

## Colors

Neutral semantic surfaces, text and fine dividers carry the form, grid and inspector. Existing accent roles identify the primary launch action, links, selection and focus. Errors and cost uncertainty remain readable text with explicit meaning; unknown is never rendered as zero. Component status, native matching status and vulnerability verification remain separate concepts. The extension adds no palette roles, gradients or decorative status fills.

## Typography

The existing UI family and compact hierarchy remain unchanged. Section headings use the small semibold role; labels, notes, status details and property lists use the extra-small role and body line height. Hashes, SDK identifiers, paths, amounts and raw JSON use functional monospace. Long values wrap within their own surfaces. The data and evidence remain more prominent than the quiet section headings.

## Layout

`ComponentScanLaunch.vue` is a divided section inside the inspected immutable version in `ComponentPlans.vue`. The selected model sits above a wrapping field grid for component concurrency, round time, component limit, round limit and cumulative task budget. The reservation preview, cost limitation and acknowledgement precede the primary action. This keeps the source boundary and explicit model call together.

`ComponentExecutions.vue` extends `ScanDetail.vue` beside the existing task budget, history, recovery, activity and result surfaces. Its header links the frozen plan and reports concurrency. A compact property strip shows round phase, reserved amount, known cost lower bound and native matching fee state. The data grid remains beside a persistent component inspector on desktop; contextual actions lead to the original execution and raw component result. Retry controls follow as a separate divided domain section.

The inspector column is bounded by (`minmax(200px, 280px)`). At (1000px) and below it stacks beneath the grid, with a two-column metadata layout; at (600px) and below its metadata becomes one column. Table overflow remains local, headers/actions wrap and long hashes break within the inspector. The launch fields reflow through an auto-fit grid with a (140px) minimum. These adaptations retain the same form and investigation order on mobile rather than introducing cards.

Spacing follows the existing relationship scale: nearby labels and values use `--space-1`/`--space-2`, section interiors and pane divisions use `--space-3`, and the launch section is separated from the frozen document by `--space-4`. No feature card grid or dashboard composition is added.

## Elevation & Depth

The launch form, component grid, inspector and retry section remain flat. Borders, selection tone and type establish hierarchy. Existing Select, Button and Input primitives retain their incumbent floating or control treatment. Hover changes row background; focus uses the existing visible outline. This extension adds no decorative shadow or ambient motion.

## Shapes

Main sections, panes and inspector remain rectangular. Shared controls retain their token geometry and compact sizing. Raw matching evidence is a simple tonal, scrollable text surface. No new radius, shadow or shape token is introduced.

## Components

### Frozen-version launch

The launch form requires `operate` permission, a model and an explicit acknowledgement that the audit calls the model and native matching can incur additional unknown fees outside scan limits. Concurrency is (1–8); time is (1–600) minutes. Budget fields accept integer-micro-dollar precision. The displayed allocation is a preview; the Server freezes the actual allocation when the parent attempt is claimed, using cumulative budget remaining. Creation errors remain inline and retain the field values. Busy state disables duplicate submission. A viewer sees the permission explanation instead of execution controls.

### Parent and component state

The current parent overview collects each component's most recent receipt while retaining its original attempt. Historical selection displays the selected attempt's component set; components not selected in that attempt remain visibly unexecuted. Findings, native matching and report exports cover the selected execution and its batch. Historical findings and their disposition are read-only. Completing one round does not imply that all components in the parent plan are complete.

| Component state | Visible meaning |
| --- | --- |
| No receipt | Not yet executed. |
| `pending` | Waiting to execute. |
| `running` | Running, with reported phase and current activity. |
| `completed` | Completed; excluded from component reexecution. |
| `partial`, `failed`, `canceled` | Partial completion, failure or cancellation, retaining available results. |
| Pending/running receipt after the parent stops | Interrupted and incomplete. |

Parent round phases separately name source preparation, component scanning, cross-component matching and finished. The grid shows reservation, known fee lower bound, final fee or explicit unknown, attempt and finding count. The inspector preserves scope, source/plan hashes, optional HEAD, SDK identity, current activity and domain error code/message. Component loading, read failure with refresh, artifact download failure and absent activity/identity each have explicit states.

### Reservations and reexecution

One parent allowance funds the selected batch. Integer-micro-dollar allocations are reserved before dispatch; the total cannot exceed that attempt's allowance, and unused allocations are not repeatedly reused between concurrent components. Effective concurrency is the smaller of the configured limit and selected component count, subject to existing global/model/project admission; the Agent slot still belongs to the parent. The UI distinguishes reservation from observed lower bound and final cost. Engine estimates do not guarantee the provider's bill. Matching has no public fee callback, cap or disable switch in SDK (0.1.25); only a phase that has not been called may show known ($0). A called phase with unavailable cost remains unknown.

The existing Web task-budget reconciliation flow gates reexecution when fees are unknown. After the parent stops, operators may select failed or unfinished components, explicitly confirm another model call and create a new attempt. Completed components have no retry checkbox and the Server rejects their replay. The new attempt retains the task, source, model and frozen plan, uses cumulative remaining budget and preserves old results and fee evidence in their original attempts. Matching failure does not turn a completed component into a failed component. The new batch's matching does not claim deduplication against all earlier successful components.

Cancellation uses the existing parent action and retains available work and cost evidence. It is not a pause or cross-process resume. Existing interrupted-execution recovery can validate and import an already submitted result without another model call; lease expiry cannot silently replay a model.

### Native matching and original evidence

`ExecutionResult.vue` names cross-component matching “模型推断”, displays original finding/group/uncertain-pair counts when available and exposes the native summary and matching evidence as JSON. It states that matching covers this attempt's selected components, uncertain matches remain independent findings and matching does not alter verification state. Independent findings remain available when matching fails.

The component inspector downloads the original component JSON from its originating attempt. The existing archive preserves complete result JSON, raw SDK extensions, artifact sizes/hashes and artifact downloads. `ScanDetail.vue` extends the finding inspector with the original component binding and source-plan link. The binding includes parent execution/attempt, original component index, SDK Scan ID, plan version/digest, source digest/optional HEAD and raw result size/digest. Client contract readers reject invalid component contracts or mismatched task/version/source identity rather than treating them as current evidence.

### Keyboard and permissions

Named form fields and acknowledgement controls use native semantics. The component table has headers; each component selector has selected-state semantics and visible focus. Arrow Up/Down and J/K move selection and focus between component buttons; Enter activates the focused native button. The scrollable grid itself is focusable. Loading, errors and reported matching reasons use existing status/alert semantics. Viewer accounts retain inspection, execution history and downloads while launch and retry actions require operation permission. No full accessibility certification is implied.

## Do's and Don'ts

- Do keep the frozen boundary, original attempt, cost state and evidence identity adjacent to the operator's action.
- Do distinguish parent completion, round completion, component failure and matching failure.
- Do preserve unknown fees, original component findings, raw matching evidence and historical artifacts.
- Do label native matching as model inference and scope its claims to the selected batch.
- Don't equate a reservation or known lower bound with a complete final bill.
- Don't replay completed components or overwrite successful evidence during failed-only reexecution.
- Don't add decorative cards, new global visual rules or synthetic product data to fill this workspace.

The supplied [direction contract](../../output/playwright/component-scans/direction-contract.md) and [finish review](../../output/playwright/component-scans/finish-review.md) record this bounded extension. The review disposition is **ship**, with no material fixes within its supplied boundary. The single [detector record](../../output/playwright/component-scans/detector.json) is empty (`[]`). No new visual decisions remain open.

The retained captures show [desktop launch](../../output/playwright/component-scans/component-launch-desktop.png), [mobile launch](../../output/playwright/component-scans/component-launch-mobile.png), [desktop running components](../../output/playwright/component-scans/component-running-desktop.png) and [mobile running components](../../output/playwright/component-scans/component-running-mobile.png). They cover those component surfaces only, not terminal/error layouts or the full surrounding workspace. Synthetic source/components are labelled “Fixtures / Mock”.

[browser.json](../../output/playwright/component-scans/browser.json) records (15) passing fixture checks for launch acknowledgement/error retention, scheduler progress/reservations, keyboard inspection, native result download, matching/bindings, read retry, partial/cancelled work, unknown-fee reconciliation, failed-only reexecution, historical evidence, distinct read-only archived finding rows, current/historical report scope, rejection of unavailable attempts and viewer permissions. Terminal and error behavior is supported by that record and source inspection, not terminal-state screenshots. The record reports zero real planning-model calls, real audits, real SDK logins and external publications. This documentation pass reads supplied source and artifacts only; it does not run a browser, model or implementation tests, certify backend security or accessibility, or claim deployment/real-model audit quality.
