---
name: Axiom SDK execution identities
description: Existing settings and execution workflow extension for Agent-bound SDK credentials, explicit authentication operations and frozen task identities.
primary_target: "code-audit-web/src/components/SDKIdentities.vue"
related_targets: ["code-audit-web/src/components/SDKIdentityPicker.vue", "code-audit-web/src/components/SDKIdentityBinding.vue", "code-audit-web/src/views/Settings.vue", "code-audit-web/src/views/NewScan.vue", "code-audit-web/src/components/ComponentScanLaunch.vue", "code-audit-web/src/views/ComponentPlans.vue", "code-audit-web/src/views/ScanDetail.vue", "code-audit-web/src/views/ModelConfigs.vue"]
---

# Surface: SDK execution identities

## Overview

This is the implemented SDK execution-identity extension as of 2026-10-18. It serves administrators establishing credentials on a bound Agent and operators choosing a credential boundary for an execution. It extends the approved C / Cold Precision environment through the existing [design philosophy](../DESIGN_PHILOSOPHY.md), [component language](../COMPONENTS.md), [tokens](../DESIGN_TOKENS.css) and [DESIGN.md](../../DESIGN.md). It introduces no new visual world, font, palette or global token. [PRODUCT.md](../../PRODUCT.md) supplies product context; [SDK_EXECUTION_IDENTITIES.md](../../docs/SDK_EXECUTION_IDENTITIES.md) defines the implementation contract and operational limits.

Axiom local accounts, SDK execution identities and model configurations remain distinct objects. The operator can create an API Key or ChatGPT device-code identity in Settings, explicitly log in or query its SDK account state, choose it for a supported native execution and inspect the frozen binding on the task. Logout is expressed as revocation followed by confirmed local cleanup. Authentication is an SDK observation, not evidence of Trusted Access, model access, plan coverage or provider billing.

| Domain object | Surface role |
| --- | --- |
| SDK identity | Primary object: name, type, ID, bound Agent and credential generation. |
| Identity snapshot | Last recorded authentication state, SDK check time and local cleanup state. |
| Identity operation | Explicit API Key login, device-code login, account query or logout, with lifecycle and creation time. |
| Device instructions | Restricted authorization URL, device code and local waiting deadline, visible only to an administrator while active. |
| Frozen task identity | Task-bound identity ID, name, type, Agent and generation; displayed separately from the latest identity state. |
| Model configuration | Model/endpoint choice; may retain its own API Key or require a separately selected SDK identity. |

The SDK remains at (0.1.25), using public `loginApiKey()`, `loginChatGPTDeviceCode()`, `account()` and `logout()`. The Go Server, Go Agent and Node Scanner architecture, HTTP transport, shared artifacts and JSONL remain in place. All identity controls start from the Web; the surface introduces no user-terminal login step. Historical tasks without an identity binding retain their existing credential rules.

## Colors

Neutral surfaces, primary/secondary text and fine semantic borders carry the table, properties and operation history. Accent identifies selected rows, primary actions, links and focus. Identity and operation states are readable text rather than a wall of colored badges. Errors use the existing danger text role with an explicit message. The feature inherits the incumbent light and optional dark tokens; it adds no gradients, decorative state fills or palette roles.

## Typography

The existing UI font stack and compact hierarchy remain unchanged. The section header uses the medium small heading role; the selected identity uses semibold text. Labels and table content use the small role; boundary notes, properties, history and picker explanations use the extra-small role and body line height. Shared controls retain their existing text sizing.

Agent IDs and device codes use functional monospace. Identity names and ordinary explanatory text remain proportional UI type. Long inspector values and notes wrap locally. The table retains compact aligned columns and local horizontal scrolling rather than reducing the complete workspace to miniature text.

## Layout

Settings exposes the persistent route `/settings?section=sdk-identities` within its existing category navigation. The identity section orders its heading/refresh action, boundary note, inline error, administrator creation form and table/inspector workspace. Selecting a row updates the adjacent inspector while preserving the list. The three table columns are identity/type, authentication/cleanup state and execution boundary (Agent/generation).

The desktop workspace uses a flexible table and a bounded inspector (`minmax(260px, 320px)`), separated by a straight divider and `--space-4`. The creation form has three flexible fields and its submit action. With no selected object, the table uses the available width. At (1050px) and below the inspector stacks beneath the table, its divider becomes horizontal and the creation form uses two columns. At (550px) and below the creation fields stack into one column. Table overflow remains local; action groups wrap. The retained (390px) capture preserves the same object, state, boundary and action order.

The inspector orders object header and state/Agent, identity/generation, last SDK check, local cleanup, applicable login/account/logout actions, active operation, device instructions and recent operations. Property and history groups use `--space-1`/`--space-2`; actions and fields use `--space-2`/`--space-3`; pane and section relationships use `--space-4`. Device instructions are a divided section, without a nested card.

`SDKIdentityPicker.vue` is a compact form section next to the model selection in new scans, component suggestions and frozen-plan component launches. It includes the identity selector, boundary/status note, recoverable read error and link to identity settings. `SDKIdentityBinding.vue` is a wrapping, divided metadata strip in task detail, keeping the frozen selection next to the execution rather than moving it to a separate details page.

## Elevation & Depth

The identity table, inspector, binding strip and history remain flat. Fine dividers, type and selection tone establish hierarchy. Shared Select and revocation confirmation reuse the incumbent floating-control/dialog treatment. Hover changes background; row focus uses the existing visible outline. Pending controls use the shared loading/disabled behavior. No feature-specific shadow, hover movement or ambient animation is introduced.

## Shapes

The main workspace, inspector and divided device instructions remain rectangular. Button, Input and Select retain the existing token geometry and compact control sizing: default buttons/inputs are (32px), small buttons are (28px), and the shared table uses the (34px) row-height token as its baseline while allowing secondary metadata. This extension introduces no new radius or shape token.

## Components

### Identity creation and inspection

Administrators create a name, an identity type and a bound Agent ID. The Agent field points to the existing execution-node identity; it is not an account selector or an automatic node assignment. Creation requires nonempty name/Agent fields and uses the native field constraints. Loading and request errors remain in the section; an empty list explains that existing models and historical scans continue with their original credentials.

The identity list and inspector refresh approximately every (2.5 seconds), with an explicit refresh action. The inspector exposes the last SDK check time or “尚未检查”, so a retained record is not presented as a live guarantee. Invalid snapshot/operation contracts are rejected by the client validator instead of silently showing an incompatible record.

| Identity state | Visible meaning |
| --- | --- |
| `unconfigured` | 未登录. |
| `authenticating` | 正在登录; operation details show the current work. |
| `authenticated` | 已认证; bounded to the SDK authentication observation. |
| `signed_out` | 未认证. |
| `revoked` | 已撤销; new task use is blocked. |
| `unknown` | 状态未知; no automatic credential fallback. |
| `cleanupPending` | Separate “等待节点清理” signal; revocation is not yet confirmed local cleanup. |

### Explicit authentication operations

API Key login uses a password field scoped to the selected identity. A successful operation submission clears the browser input; a failed submission retains it for an explicit retry. Selection change and unmount clear it. Keys are not shown in identity metadata, task bindings or operation history.

ChatGPT login requires “开始 ChatGPT 设备码登录”. Active instructions show the code, a link to the allowlisted `https://auth.openai.com/codex/device` page and a local waiting deadline. The link opens only on user action. Copy explains that this deadline is not a provider validity guarantee and Axiom does not request a ChatGPT password. Instructions are removed when the waiting operation ends or is canceled.

“查询 SDK 账户状态” records the SDK authentication observation and check time. It does not display raw SDK account details or claim model/policy access. Recent operations retain action, status, generation, creation time and sanitized errors. Raw SDK authentication JSON, SDK `details` and native stdout/stderr are intentionally outside the public Web contract; generic raw-data access patterns must not expose those credentials here.

| Operation state | Visible meaning |
| --- | --- |
| `queued` | 等待绑定节点. |
| `running` | 正在执行. |
| `waiting` | 等待设备码授权. |
| `completed` | 已完成. |
| `failed` | 失败, with the reported domain error. |
| `canceled` | 已取消. |
| `expired` | 已过期或节点失联. |

An active operation disables conflicting login/account actions. Non-logout operations expose cancellation. Cancellation is not pause/resume and does not automatically replay a login; a new login requires another explicit action and uses a new generation. Operation-history read errors have their own “重读身份操作” recovery rather than replacing the selected identity.

### Revocation and local cleanup

“撤销并退出” uses the existing confirmation dialog. Its text names the immediate boundary change, cancellation request for running tasks and preservation of historical results. Revocation immediately prevents new use; local cleanup completes only after the bound node confirms SDK logout and deletion of that identity's local credential generations. Offline or failed cleanup remains visibly pending and exposes “重试退出清理”. A completed cleanup leaves the identity revoked. Other identities and retained task results are separate objects.

The UI does not equate Axiom revocation with provider-side API Key revocation or logout on other devices. SDK operation deadlines, node readiness, leases and cleanup sequencing remain Server/Agent responsibilities. This first implementation assumes trusted users in a single-machine self-hosted application; it does not establish tenant or OS-user isolation.

### Selection and frozen task boundaries

The picker offers model-configuration credentials plus snapshots that are `authenticated`, explicitly authenticated and have no pending local cleanup. Identity choices name type and Agent. Unsupported model choices disable the independent identity selector and retain the model-credential explanation. The Server remains authoritative for native endpoint compatibility, generation, Agent readiness and permission checks. A model created with “扫描时使用 SDK 执行身份提供凭据” stores no model API Key and requires a separate identity when creating work; its connection guidance points to the identity's account query.

New scans, SDK component suggestions and frozen component audits submit an identity ID when selected. The Server freezes identity ID/name/type, Agent and credential generation; derived preflight, validation, component reexecution and ordinary retries inherit that binding. The new picker extends the existing [component plan](component-plans.md) and [component execution](component-scans.md) workflows without changing their source, plan, budget or acknowledgement boundaries.

The task strip displays the frozen identity and checks the latest snapshot approximately every (5 seconds). It labels the last recorded state/check time separately from the task's generation. A changed generation explicitly states that the task cannot adopt the new credentials and directs the operator to cancel or create another task. Unknown/unavailable/revoked states never imply fallback to model credentials or another account. Read errors retain the frozen binding and provide “重读身份状态”. Historical results remain attached to their original identity/generation after logout.

### Permissions, keyboard and recovery

Accounts with read access can inspect identity metadata and the selected identity's state. Creation, login, device instructions, operation history, account queries and revocation require administrator permission. Task operations continue to use the existing operation permission. Viewers receive the explanatory read-only note and do not receive the administrator form or credential controls.

The table has column headers and a screen-reader caption; identity buttons expose selected state and visible focus. Arrow Up/Down and J/K move selection/focus between identity buttons; Enter activates the focused native button. Forms and selectors have names/labels, progress uses status semantics and failures use alert semantics. Shared controls preserve their existing keyboard and focus behavior. There is no claimed custom Escape-to-close inspector behavior, identity search/sort/column-resize feature or complete accessibility certification in this bounded extension.

Read failures retain already loaded inspection data and expose manual recovery. Busy state prevents duplicate mutations. There is no fabricated successful authentication, synthetic production activity or percentage used to fill a loading/empty state.

## Do's and Don'ts

- Do keep authentication state, Agent boundary, generation, last check and cleanup state legible together.
- Do distinguish a frozen task selection from the latest identity snapshot.
- Do preserve explicit login, cancellation, revocation and cleanup-retry actions.
- Do keep secrets and raw SDK authentication details outside public metadata and artifacts.
- Don't equate SDK authentication with Trusted Access, model availability, plan entitlement or an actual bill.
- Don't silently substitute another identity, generation or model credential when the selected boundary becomes unavailable.
- Don't turn the list/inspector into decorative credential cards or introduce a new visual identity for this surface.

The supplied independent finish review returned **ship**, with no material fixes within its boundary. The retained [design-detector record](../../output/playwright/sdk-identities/private/design-detector.json) is empty (`[]`). No new global visual decision remains open; this surface record preserves the incumbent system without regenerating root design tokens or the existing sidecar.

The [desktop device-login capture](../../output/playwright/sdk-identities/browser/sdk-identities-device-desktop.png) and [mobile device-login capture](../../output/playwright/sdk-identities/browser/sdk-identities-device-mobile.png) show the identity region while waiting for device authorization. They are region captures, not full-workspace or terminal-state coverage. Synthetic identities are visibly labelled “Fixtures / Mock”.

The retained [browser.json](../../output/playwright/sdk-identities/browser/browser.json) reports (11) passing fixture checks: direct settings entry/refresh, failed-login input retention, Agent-bound API Key submission/clearing, account query across Agent processes, allowlisted device guidance and responsive inspector, cancellation/code removal, explicit new-login generation, task identity freezing without a model Key, revocation-before-cleanup, read-failure recovery and viewer permissions. It reports no browser errors or external requests, and zero real SDK logins, real audits, real planning-model calls or external publications. Table keyboard support is established by source inspection; this record contains no explicit keyboard-navigation check. Dark-mode appearance and terminal/error layouts are not demonstrated by these two captures.

The implementation contract separately describes Server/Agent/Scanner tests for lease loss, version/report conflicts, concurrency, session persistence and credential cleanup; those claims are not inferred from screenshots. This documentation pass inspected source and retained artifacts only. It does not claim an additional test run, production deployment, real authentication validation, provider-policy/model/billing validation, backend-security review or complete accessibility assessment. Existing Docker deployments have not applied this increment according to the implementation handoff.
