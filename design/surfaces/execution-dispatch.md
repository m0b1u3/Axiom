---
name: Axiom scan execution dispatch
description: Existing execution and diagnostics surfaces after retirement of platform task quotas, fixed Agent slots and extra SDK quantity ceilings.
mode: Operate
primary_target: "code-audit-web/src/components/ExecutionSettings.vue"
related_targets: ["code-audit-web/src/components/QueueCapacity.vue", "code-audit-web/src/components/ExecutionNodes.vue", "code-audit-web/src/components/ExecutionCompatibility.vue", "code-audit-web/src/components/AdvancedDiagnostics.vue", "code-audit-web/src/views/Settings.vue", "code-audit-web/src/views/NewScan.vue", "code-audit-web/src/components/AdvancedScanConfiguration.vue", "code-audit-web/src/components/ComponentScanLaunch.vue"]
---

# Surface: Scan execution dispatch

## Overview

This is the implemented execution-state extension as of 2026-10-19. It serves operators checking current work and administrators investigating node readiness or historical configuration. It preserves the approved C / Cold Precision environment and the governing [design philosophy](../DESIGN_PHILOSOPHY.md), [component language](../COMPONENTS.md), [tokens](../DESIGN_TOKENS.css) and [DESIGN.md](../../DESIGN.md). [PRODUCT.md](../../PRODUCT.md) supplies product context; [TASK_MANAGEMENT.md](../../docs/TASK_MANAGEMENT.md) owns the dispatch and upgrade contract. This extension introduces no visual world, global token or sidecar change.

The surface thesis is truthful execution state after quota controls are removed. Settings now names the section “扫描执行”. The first view orders its heading and reload action, a short explanation locating per-scan Worker/budget/timeout configuration, active/pending/online counts, the declared dispatch policy and a link to diagnostics. It uses the existing settings workspace rather than a dashboard or capacity editor. Advanced diagnostics keeps node contracts, raw snapshots and old quota revisions accessible without presenting historical settings as active policy.

The native-concurrency extension applies SDK (0.1.25) safe-integer rules to the existing per-task fields and removes the extra Worker, subagent, component and discovery-count ceilings. It retains the chosen values, active budget/timeout/scope configuration and historical readers. The existing task compatibility region names the capability needed when those values exceed older nodes' supported range. [SDK_NATIVE_CONCURRENCY.md](../../docs/SDK_NATIVE_CONCURRENCY.md) owns this numeric and compatibility contract; no new settings route or capacity editor is added.

| Domain object | Surface role |
| --- | --- |
| Queue overview | Last successfully read active execution count, pending task count and online execution-node count. |
| Dispatch declaration | Server policy evidence: `dispatchMode: unbounded`, with `effectiveCapacity: null`. |
| Execution node | Agent identity, online/readiness state, reported dispatch mode, contract versions, diagnostic and last report time. |
| Historical quota revision | Original global/model/project values, revision, author and time; read-only evidence of an earlier configuration. |
| Task SDK configuration | Worker/component concurrency, budget, timeout, scope and immutable inputs selected for that task; configured in the existing launch workflow. |

The Go Server + Go Agent + Node Scanner architecture, SDK (0.1.25), HTTP transport, shared artifacts and JSONL remain in place. Removing task-count admission quotas and fixed Agent slots does not change the task's chosen SDK settings or force them to maximum values. The [component launch](component-scans.md) and [identity binding](sdk-identities.md) retain their separate input, evidence and credential semantics.

## Colors

Neutral surfaces, primary/secondary text and fine dividers carry the status strip and diagnostic sections. Existing accent identifies links and focus. Policy copy stays secondary neutral; missing declaration, unavailable nodes and legacy-node notices use existing warning text with explicit meaning. Request errors retain the incumbent error treatment. Counts are ordinary aligned values, without colored statistic cards, utilization fills or a new palette role.

## Typography

The existing UI family and compact scale remain unchanged. The execution heading and count values use the medium text role with semibold weight; explanation, count labels and the diagnostics link use the extra-small role. Counts use tabular figures. Diagnostics uses the existing large section heading, medium subsection headings and small body/history text. Raw snapshots use functional monospace and the extra-small role. Long diagnostic text and JSON wrap within their own surfaces.

## Layout

`ExecutionSettings.vue` remains at `/settings?section=execution` within the existing settings navigation. The heading and small ghost reload button share one row. The explanatory sentence precedes `QueueCapacity.vue`, whose three inline count groups sit between straight horizontal dividers. Policy and domain notices follow on their own lines. The diagnostics link follows the strip. There is no replacement quota form, save action, finite-capacity denominator or fabricated large capacity number.

The count strip wraps, using `--space-6` between groups on desktop and `--space-3` at (600px) and below. Labels and their values use `--space-2`. Its vertical padding uses `--space-4`, and its following separation uses `--space-5`. The explanation is bounded to a readable (80ch). Existing settings navigation stacks above content at (1100px); its categories and buttons reflow again at (600px). These are incumbent layout relationships, not new page-wide spacing rules.

`AdvancedDiagnostics.vue` remains at `/settings?section=diagnostics`. Its sections order execution nodes and component compatibility, identity diagnostics, old concurrency records, storage diagnostics and runtime architecture. Fine dividers and section spacing preserve this reading order. Node data remains a table with local horizontal overflow and a visible narrow-screen scroll hint; its focusable scroll region retains all contract columns. History rows wrap their revision, original values and author/time rather than becoming cards. Raw JSON uses bounded, locally scrollable disclosures.

## Elevation & Depth

The execution strip, node table and historical records stay flat. Dividers, text tone and grouping establish hierarchy. Shared buttons retain their incumbent control treatment and pending state. This extension introduces no resting shadow, hover movement or decorative motion.

## Shapes

Main sections and diagnostic data surfaces remain rectangular. Buttons retain existing token geometry and compact small sizing. No radius, shadow or control-size token is added.

## Components

### Current execution snapshot

The strip shows `active` executions, `pending` tasks and the number of agents whose `online` value is true. It does not add Agent slots to infer capacity. `effectiveCapacity: null` expresses that this dispatch policy has no finite platform task-count limit; it is not a measured throughput promise. The existing manage link, where shown by other callers, leads to the execution settings section.

The section loads on mount, supports explicit reload and refreshes a previously loaded snapshot when its kept-alive view is activated. The shared button indicates pending work, and the loader prevents overlapping refresh requests. An initial read displays “正在读取扫描执行状态…”. A read failure names the failed operation. If a successful snapshot already exists, the values remain visible alongside the explicit notice that they are the last successful snapshot and require a new read to confirm current state. No synthetic replacement values or percentage appear.

### Declared policy and mixed versions

Only an overview declaring `dispatchMode: unbounded` produces “平台不设置全局、模型或项目并发配额。” A missing/older declaration instead asks the operator to update the paired components and confirm execution behavior. With no online nodes the surface explains that queued work awaits execution-service recovery; partially ready nodes direct the operator to advanced diagnostics. Any online node without the new mode produces the separate legacy-service update notice.

The new Agent first requires the Server's `taskDispatch: unbounded` capability and then reports `dispatchMode: unbounded` with `slots: 0`. Zero is the explicit absence of a fixed slot ceiling, not zero execution ability. The current Agent refuses a Server lacking that declaration rather than silently reverting to its former dispatch loop. Positive-slot legacy metadata remains readable and is labelled “旧版固定并发” in the node table. Contract capability and runtime readiness remain separate from online presence and dispatch mode.

Node rows expose Agent ID, execution mode, current state, contract capabilities and last report time. Reported runtime diagnostics and optional missing-capability information remain legible beside that node. “节点原始上报” and “调度原始快照” expose the underlying metadata through native disclosures. A node reports every (10 seconds) and becomes offline after (30 seconds) without a report; these timing rules are described in diagnostics, not converted into an invented capacity metric.

### Historical configuration and compatibility

“旧版并发配置记录” retains original revisions and global/model/project values. With a confirmed new Server, its explanation says these records are only for tracing history and no longer participate in scheduling. Without the declaration it instead states that the Server has not confirmed the new policy and asks for paired updates; a history record alone cannot establish retirement. An empty successful history read states that no change records exist. A failed history read has its own domain error.

`GET /api/v1/settings/execution` retains the old settings and recent history with `canEdit: false` and the current dispatch declaration. The retired `PUT` route still checks administrator permission before returning (410) for an authorized request; it cannot restore quota editing. Migration (023) permits the explicitly declared zero-slot value without deleting older quota or execution history. Current Server/Agent/Web components must be updated together; the [task-management contract](../../docs/TASK_MANAGEMENT.md) records the upgrade boundary. This source change does not imply an existing Docker deployment has been upgraded.

### Task options and remaining execution boundaries

The old global/model/project quota variables and `AXIOM_AGENT_CONCURRENCY` no longer set task-count admission limits for the current implementation. Native Worker and component concurrency still follow the frozen task configuration. Budgets, timeouts, scope and knowledge/prompts/reasoning parameters keep their existing meaning. The input-forwarding extension remains in the [component execution record](component-scans.md) and [SDK parameter contract](../../docs/SDK_PARAMETERS.md).

Authentication, source identity, storage admission, task/attempt leases, cost accounting and credential-generation checks remain distinct from the retired quotas. Confirmed v12 nodes allow multiple parent executions on the same API Key or ChatGPT identity. Each execution owns private SDK state; ChatGPT credential copying and compare-and-swap writeback use short critical sections. Login/account operations and revocation cleanup retain their exclusion against active executions, as recorded in the [identity surface](sdk-identities.md#concurrent-parents-and-private-credential-state). Unknown fees still require their existing reconciliation path, and lost leases cannot silently replay model calls. The status strip does not collapse these domain reasons into generic “capacity full” or system-failure messages.

### SDK-native quantity controls and compatibility

The existing labelled numeric fields accept the SDK's safe-integer domain, whose maximum is (9007199254740991). This numeric boundary comes from `Number.isSafeInteger`; it is not a new execution quota or a hardware-throughput promise. Negative, fractional, non-finite and unsafe values are rejected by the Web and contract boundaries. Newly submitted discovery-count (0) is invalid; historical stored (0) retains its prior unconfigured meaning.

| Existing Web field | Accepted value and SDK mapping |
| --- | --- |
| Deep Worker count | Positive safe integer → `run().workers`; blank omits the override. |
| Subagents per Worker, in advanced SDK parameters | Nonnegative safe integer → `subagents`; explicit (0) disables subagents, blank omits the override; Deep only. |
| Component concurrency | Positive safe integer → `runComponentScans().workers`; initial value (4), matching the SDK default. |
| Deep discovery count and consecutive-no-new count | Positive safe integers → `maxDiscoveryRuns` and `stopAfterNoNew`; blank omits each override. |

The Web forwards accepted values unchanged. It does not reduce them to the former Worker/subagent (64), component (8) or discovery-count (1000) ceilings, or to the selected component count. The SDK naturally schedules at most `min(workers, selected component count)` for that batch. The frozen [component launch](component-scans.md) keeps its cost reservation and acknowledgement; the Deep disclosure keeps its stopping explanation and coverage limitation. The same compact inputs, labels and inline domain errors remain in their original forms. Desktop field grids reflow on narrow screens; the maximum safe integer remains readable without enlarging controls or introducing a new container pattern.

Values above the former numeric domains require Scanner Description (v12), `sdkConcurrencyVersion: 1`, SDK (0.1.25), the control API (4) capability and a valid runtime report from the last (30 seconds). Old, missing, unknown or stale capability cannot claim those tasks. The thresholds identify older readers; they never truncate a request. Existing Job/Assignment versions and historical reports/results remain readable, and older nodes may still claim historical work within the ranges they support. Same-identity parent concurrency is enabled only for nodes with the confirmed new capability.

`ExecutionCompatibility.vue` orders the upgrade action before its required-contract properties and node evidence. For `sdk_concurrency_incompatible`, its status text asks to update Agent and Scanner together and explicitly says the configuration remains frozen and concurrency will not be lowered automatically. “SDK 原生并发参数” appears in both the required capabilities and a node's missing-capability explanation. Node rows retain current state, Scanner description/version and report time; “节点原始上报” and “兼容判定原始数据” expose the underlying reports and decision through native disclosures. On narrow screens, the named focusable table region uses local horizontal scrolling with its visible hint. These evidence controls preserve task context while the operator resolves the node boundary.

### Keyboard and read semantics

Settings categories remain native buttons with `aria-current` and visible focus; reload and diagnostic navigation use the shared native button/link controls. Loading and retained-snapshot notices use status semantics, while request failures use alert semantics. The node table retains headers and a labelled, keyboard-focusable scroll region. Raw snapshots use native `details`/`summary`. This bounded surface adds no row-selection shortcut, custom table navigation or complete accessibility certification. Viewing execution state and history does not start a scan, login or settings write.

## Do's and Don'ts

- Do keep active work, pending work, online presence and contract readiness distinct.
- Do show the current Server declaration before asserting that quota policy is retired.
- Do preserve last-read context and label it when a refresh fails.
- Do keep old revision values readable with explicit historical meaning.
- Do keep per-task SDK controls and identity, source, cost and lease boundaries intact.
- Do preserve SDK-native values and the explicit node-upgrade reason while a task waits for compatibility.
- Don't turn `slots: 0` into an offline state or invent a finite capacity from it.
- Don't treat an old settings record as an editable current limit.
- Don't equate unbounded dispatch with measured hardware capacity or verified provider throughput.
- Don't introduce decorative statistics cards or a new visual system for this settings extension.

### 2026-10-19 retained evidence

The final [desktop execution capture](../../output/unbounded-dispatch-20261019/final/browser-dispatch/dispatch-desktop.png) and [mobile execution capture](../../output/unbounded-dispatch-20261019/final/browser-dispatch/dispatch-mobile.png) show the heading, actual fixture counts, declared policy and diagnostic link. The [desktop history capture](../../output/unbounded-dispatch-20261019/final/browser-dispatch/dispatch-history-desktop.png) and [mobile history capture](../../output/unbounded-dispatch-20261019/final/browser-dispatch/dispatch-history-mobile.png) show the old revision explicitly marked inactive. These are targeted light-theme region captures, not full-workspace, every-state or dark-theme coverage. Their values come from the declared Fixtures / Mock acceptance environment and are not production measurements.

The retained [browser record](../../output/unbounded-dispatch-20261019/final/browser-dispatch/browser.json) reports (8) passing checks: seven actual Agent executions with the retired controls removed; online zero-slot presence without fabricated capacity; retained-snapshot recovery; older Server declaration handling; older Server history wording; offline-node explanation; legacy-Agent update notice; and desktop/mobile read-only history. It records no browser errors, external requests, browser writes, real audits, planning-model calls, SDK logins, external publication or business-data writes. The final [dispatch pipeline log](../../output/unbounded-dispatch-20261019/final/dispatch-pipeline.log) records `TestUnboundedDispatchPipeline` passing in (4.77 seconds), with seven simultaneous synthetic SDK entries despite legacy quota values and Agent concurrency set to one.

The separate [confirmed compatibility record](../../output/unbounded-dispatch-20261019/compatibility-confirmed/verification.json) and [log](../../output/unbounded-dispatch-20261019/compatibility-confirmed/compatibility-pipeline.log) record the focused compatibility rerun passing in (7.13 seconds). They identify the SDK version, disposable PostgreSQL tmpfs teardown and absence of business-database access or real audit/planning/login calls. These scoped results do not turn an earlier broad run or exploratory failure into a passing whole-suite claim. Additional implementation coverage is described separately in [TASK_MANAGEMENT.md](../../docs/TASK_MANAGEMENT.md), rather than inferred from captures.

The retained fresh [finish review](../../output/unbounded-dispatch-20261019/finish-review.md) returned **ship**, with no material fixes within the five changed UI-source targets and the four supplied captures/browser record. It used the shipped degraded finish-reviewer role because the named definition was absent; it inspected those artifacts without running a browser. The single [detector record](../../output/unbounded-dispatch-20261019/design-detect.json) is empty (`[]`); this documentation pass did not rerun it. No backend-safety, full accessibility, all-theme, provider throughput or complete regression certification follows from this bounded review.

This documentation pass inspected source and retained artifacts only. It ran no browser, tests, model, authentication or deployment action. Existing Docker deployments and GitHub were not updated for this increment. Earlier component/identity evidence linked from their surface records remains historical evidence for those increments.

### 2026-10-20 diagnostic reading refinement

The screenshot-led refinement keeps the existing Advanced Diagnostics route, section order, permission boundaries and C / Cold Precision language. `DiagnosticSnapshot.vue` now owns the four native JSON disclosures for execution-node reports, dispatch, authorization and storage. Each disclosure starts closed and reports the complete JSON's line count and UTF-8 size. Its expanded reader offers wrap, copy and JSON download with explicit failure feedback; a bounded, focusable monospace region keeps the whole payload accessible without making the document grow with every line. No new card, token, decorative icon or visual world is introduced.

The node table gives identity and state a clearer text hierarchy, retains blocked/offline meaning, and preserves its named local scroll region and optional missing-capability column. Server and storage snapshot times remain source evidence, not a freshness guarantee. Desktop settings navigation stays visible while reading long diagnostics; at the incumbent (1100px) breakpoint it returns to normal document flow. Authorization operation history, manual binding, historical quota values and storage bookmarks retain their existing behavior. Storage read failures now explicitly identify the retained successful snapshot, matching node and authorization read semantics.

The final [desktop reading capture](../../output/playwright/diagnostics-style-20261020/desktop-scrolled-viewport.png), [dark reading capture](../../output/playwright/diagnostics-style-20261020/desktop-dark-viewport.png) and [narrow reader capture](../../output/playwright/diagnostics-style-20261020/mobile-light-expanded-viewport.png) come from the read-only in-memory Fixtures / Mock environment. The [browser record](../../output/playwright/diagnostics-style-20261020/browser.json) reports seven passing acceptance groups: complete JSON/copy/download for all four readers; keyboard disclosure and local scrolling; wrap and copy/download failure feedback; retained evidence across independent read failures; 320/390/900/1440 layouts, desktop sticky navigation and both themes; initial loading and honest empty states; and viewer/admin boundaries plus storage bookmarks. Measured raw/toolbar/selected-wrap text contrast ranges from (5.45:1) to (15.55:1) across both themes; this is scoped reader evidence, not whole-application accessibility certification.

`npm run build` and all (65) existing Web tests passed. `npm run test:diagnostics` is wired into the existing Web CI workflow; remote CI was not run for these uncommitted changes. The single [detector record](../../output/playwright/diagnostics-style-20261020/design-detect.json) is empty. Browser fixtures perform no business writes, database access, real SDK login, audit, planning-model call or external publication. SDK (0.1.25), Server/Agent/Scanner contracts, transport and historical readers remain unchanged. This increment updates local source and tests; it does not update Docker or GitHub.

### 2026-10-19 native-concurrency evidence

The final [desktop Deep fields](../../output/native-concurrency-20261019/native-browser-final/browser-sdk-parameters/native-deep-desktop.png) and [mobile Deep fields](../../output/native-concurrency-20261019/native-browser-final/browser-sdk-parameters/native-deep-mobile.png) show the maximum safe integer in both discovery-count fields and Worker count. The [desktop compatibility region](../../output/native-concurrency-20261019/native-browser-final/browser-sdk-parameters/native-compatibility-desktop.png) and [mobile compatibility region](../../output/native-concurrency-20261019/native-browser-final/browser-sdk-parameters/native-compatibility-mobile.png) show the explicit upgrade/no-lowering message, required native capability, older description (11) and node/raw-data disclosures. The six supplied captures include the [component field pair](component-scans.md#2026-10-19-native-concurrency-evidence). They are light-theme target-region captures from (1440 × 1000) and (390 × 844) browser viewports, not complete workspaces, all states or dark-theme coverage.

The retained [creation browser record](../../output/native-concurrency-20261019/native-browser-final/browser-sdk-parameters/browser-create.json) reports (12) fixture checks including Deep/subagent numeric boundaries, invalid-input rejection, retained operator budget/timeout/scope, component default (4), Worker (9), and the older-runtime upgrade message without clamping. The adjacent [inspection record](../../output/native-concurrency-20261019/native-browser-final/browser-sdk-parameters/browser-verify.json) reports (3) frozen-input checks. Both state fixture-only execution and zero real model calls or SDK logins. They do not establish real provider throughput, authentication refresh correctness or backend safety from screenshots.

The independent [finish review](../../output/native-concurrency-20261019/finish-review.md) returned **ship**, with no material UI fixes in the supplied boundary. It inspected source, six captures, browser records and the retained component pipeline result without running a browser. The existing [detector record](../../output/native-concurrency-20261019/design-detect.json) is empty (`[]`); this documentation pass did not rerun it. The [final verification record](../../output/native-concurrency-20261019/native-browser-final/verification.json) records SDK (0.1.25), destroyed disposable PostgreSQL tmpfs, no business-database access and zero real audit/planning model calls or SDK logins. This pass viewed retained images and source only; no browser/test rerun, real authentication, publication or deployment was performed.
