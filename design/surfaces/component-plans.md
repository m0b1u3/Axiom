---
name: Axiom component plans
description: Existing Operate workspace extension for source-bound component drafts, coverage evidence and immutable confirmed versions.
---

# Surface: Component plans

## Overview

This is the implemented component-plan surface as of 2026-10-17. It extends the existing Operate workspace and approved C / Cold Precision identity. It introduces no new visual identity or global design decisions. The governing sources remain [AGENTS.md](../../AGENTS.md), [DESIGN_PHILOSOPHY.md](../DESIGN_PHILOSOPHY.md), [COMPONENTS.md](../COMPONENTS.md), [DESIGN_TOKENS.css](../DESIGN_TOKENS.css) and [DESIGN.md](../../DESIGN.md). [PRODUCT.md](../../PRODUCT.md) supplies product context; [SDK_COMPONENT_PLANS.md](../../docs/SDK_COMPONENT_PLANS.md) defines the implementation contract and operational limits.

The operator selects uploaded immutable source, edits component boundaries, saves a draft, checks complete source coverage and confirms an immutable version. Planning and confirmation do not start a component audit. A separate [component audit execution](component-scans.md) extension now provides an explicit launch form inside an inspected frozen version, using SDK `runComponentScans`.

| Domain object | Surface role |
| --- | --- |
| Component plan | Primary object, identified by plan ID, title, current draft revision and manual/SDK-suggestion origin. |
| Immutable source snapshot | Binds the plan to snapshot ID and complete content SHA-256; optional frozen Git HEAD is retained. |
| Component boundary | Named component and its relative source paths, edited in a table or JSON. |
| Planning operation / execution | Local normalization or SDK suggestion, with operation state, execution link and cost semantics. |
| Coverage evidence | Covered/total files, overlap evidence, empty components, duplicate names and missing paths. |
| Frozen version | Retained immutable document, revision, confirmation actor/time, source identity, validation execution and digests. |

Git plans cover the frozen HEAD's complete source, independently of base/head differential audit scope. Source selection is locked while an existing plan is open. Draft, operation result, SDK suggestion and frozen version remain distinct objects.

## Colors

The feature inherits the current light and optional dark themes through semantic CSS properties. Primary and secondary surfaces, primary/secondary text, neutral dividers and existing control borders carry the workspace. Accent is limited to actions, links, selection and keyboard focus. Current plan navigation uses the existing hover surface and accent text, with `aria-current="page"` supplying the selection meaning.

Errors and results use readable text and explicit labels. Coverage problems are evidence to resolve rather than decorative severity colors. The feature adds no color primitives, gradients or full-surface status fills.

## Typography

The approved `--font-ui` stack remains the UI voice. The shared page header supplies the existing title hierarchy. Plan headings use `--font-size-sm` with medium or semibold weight; labels, provenance, notes and diagnostics use `--font-size-xs` and the existing body line height. The shared controls retain their compact text sizes.

Paths, SHA-256 values, optional HEAD and JSON use the existing monospace treatment. Long hashes, paths and JSON wrap within their containers. Ordinary metadata stays in proportional UI type; the surface does not adopt a terminal aesthetic.

## Layout

The existing `AppLayout` shell and `PageHeader` frame a flat, divided workspace. The left pane holds source search/selection, the plans for that snapshot, pagination and the manual-plan creation form. The main pane orders plan identity and source provenance, editing, local validation/confirmation, optional SDK suggestion controls and frozen history. Version details remain in the workspace below the history list, preserving editor context.

The desktop grid uses a bounded navigation column (`minmax(200px, 260px)`) and a flexible editor (`minmax(0, 1fr)`). At (900px) and below the navigation column becomes (`minmax(170px, 210px)`). At (650px) and below the panes stack, the vertical divider becomes a horizontal divider, editor left padding is removed and the header exposes “跳到当前计划”. The source property list stacks its labels and values at this breakpoint.

Plan navigation scrolls locally with a maximum height of twelve `--space-8` units on desktop and five on mobile. Toolbars, version rows and pagination wrap. The component table uses local overflow; raw result blocks scroll locally and wrap long content. The supplied (390px) mobile captures retain the complete workflow without horizontal page overflow.

Relationships determine spacing: `--space-1`/`--space-2` for nearby labels, rows and controls, `--space-3` for pane and section interiors, `--space-4` for the desktop editor inset and `--space-5` before divided domain sections. This is a table and property-list workspace, with no feature card grid or KPI surface.

## Elevation & Depth

Resting panes, table rows, diagnostics and history remain flat. Fine `--border-default` dividers and surface tone establish structure; no feature shadow is added. The model selector and confirmation reuse the existing floating Select/Dialog treatment. Hover changes background or text without moving elements. Motion remains the incumbent controls' short state feedback.

## Shapes

The workspace and its panes remain rectangular. Shared `Button`, `Input` and `Select` retain their existing token geometry and compact heights. Path/JSON textareas use `--radius-sm`, `--surface-primary` and `--border-default`. Raw evidence uses a simple bordered or tonal text surface. The feature introduces no radius, shape or shadow tokens.

## Components

### Reused shell and primitives

`ComponentPlans.vue` reuses `PageHeader`, the existing `Button` variants, `Input`, `Select`, `CatalogPagination`, the shared confirmation utility and existing time formatting. Its domain structure follows the established pane, editable table, InspectorSection and PropertyList language; those patterns do not imply a new generic card component.

Source search, source plans, versions and missing files retain explicit pagination and loading/error states. Missing paths show (50) entries per page. Overlap evidence lists the retained first (200) overlaps with the total when larger. Empty source/plan/history states explain what can happen next and do not generate sample components.

### Draft editing and confirmation

The editor exposes component names and one path per line, plus a JSON mode for exact documents and special filenames. Root source is `.`. The contract limits a plan to (128) components and each component to (64) paths. Removal retains at least one component. Parse, request and revision-conflict errors preserve local edits. Dirty-route guards use the existing confirmation dialog before abandoning edits.

Saving creates a new draft revision and invalidates the prior revision's validation for confirmation. “规范化并检查覆盖” runs the public SDK `normalizeComponentPlan` locally, with no model call and known cost ($0). A completed local result for the current revision must have no error, valid coverage and a valid result digest before confirmation is enabled. Unsaved edits, a running action, overlap, omission, empty components and duplicate names prevent confirmation. A version already inspected for the current revision also disables repeat confirmation in this view.

Confirmation uses the shared dialog to state the draft revision, immutable source/validation binding and absence of scan execution. Later edits create draft revisions; frozen history remains readable and downloadable. Stale results stay visible with their old revision and an explicit notice that they cannot confirm or overwrite the current draft.

### Operation states, errors and cost

| Operation status | Visible meaning |
| --- | --- |
| `pending` | Waiting for an execution node. |
| `claimed` | Preparing an independent source copy. |
| `scanning` | Planning or validating. |
| `cancel_requested` | Cancellation in progress; repeat cancellation disabled. |
| `completed` | Operation completed; coverage still determines whether confirmation is allowed. |
| `failed`, `canceled`, `timeout`, `partial` | Explicit failure, cancellation, timeout or incomplete termination. |
| No recognized operation | Not yet executed. |

The page links the current operation to its execution and polls active states. Cancellation retains operation identity and available evidence. It does not automatically replay model planning; an unreliable model cost remains unknown, including after cancellation without a result. Request failures remain inline with relevant retry controls. Operation errors retain their domain code and message.

SDK (0.1.25) `planComponents` requires a selected model and explicit acknowledgement that cost is unknown and outside the scan budget cap. The SDK offers no fee callback or cap. Generated suggestions remain “未确认”, are inspectable as JSON and require explicit adoption into a new draft. The operator must review and run local validation again before freezing. A suggestion does not silently replace a draft or become a verified conclusion.

### Evidence and historical result access

Source snapshot ID, content SHA-256, optional HEAD and current revision are adjacent to editing. Coverage shows counts alongside specific overlaps, empty component indices, duplicate names and full paginated omission evidence. Each overlap's component button focuses the corresponding path field. The raw planning/validation result is directly expandable.

Frozen details show version, immutability, confirmation actor/time, version SHA-256 and validation execution/attempt. The document is visible as JSON; download retains the complete frozen-version record. Adoption and confirmation requests carry the revision, execution ID and result digest. Existing source/result identity checks reject mismatched records rather than rendering them as current evidence.

`ComponentPlanningResult.vue` extends the execution history with a link back to its plan/revision, operation kind, coverage, cost semantics, error and original JSON. It validates the result contract and execution identity before display. An invalid binding produces an explicit warning and preserves the complete archive download path. The result states that no component scan or vulnerability conclusion was produced. Existing transport, scan history and backend behavior remain the authority.

### Permissions and keyboard access

Viewers can inspect plans, JSON, coverage, executions and frozen history and download confirmed versions. The operation capability gates creation, editing, saving, planning, adoption, cancellation and confirmation; viewer controls are hidden, disabled or read-only as appropriate. Model cost acknowledgement never replaces operation permission.

The table has column headers and named name/path fields; source/model inputs have labels. Current navigation, errors, progress, raw disclosure and pagination use their existing semantic/ARIA attributes. Textareas and feature links/buttons have visible focus outlines; overlap actions move focus to the affected field. The mobile editor jump targets the focusable editor section. No custom table key scheme is claimed by this surface record.

## Do's and Don'ts

- Do keep source identity, current draft revision, operation evidence and confirmed history legible together.
- Do distinguish local validation, unconfirmed SDK suggestions and immutable confirmed versions.
- Do preserve unknown model cost and require explicit selection/acknowledgement before a suggestion operation.
- Do keep raw evidence and historical downloads accessible, including when current validation is stale or fails.
- Don't imply that confirmation starts a scan, establishes a vulnerability or verifies a generated suggestion.
- Don't add sample security data, decorative containers, new palette roles or a new global visual rule to fill this surface.

The supplied [direction contract](../../output/playwright/component-planning/direction-contract.md) and [finish review](../../output/playwright/component-planning/finish-review.md) record this bounded extension; the review disposition is **ship**, with no material fixes. The four retained captures are [desktop diagnostics](../../output/playwright/component-planning/components-diagnostics-desktop.png), [mobile diagnostics](../../output/playwright/component-planning/components-diagnostics-mobile.png), [desktop frozen version](../../output/playwright/component-planning/components-frozen-desktop.png) and [mobile frozen version](../../output/playwright/component-planning/components-frozen-mobile.png).

[browser.json](../../output/playwright/component-planning/browser.json) records (13) passing fixture workflow/fault checks, including editing/freeze prerequisites, diagnostics focus, suggestion adoption, cancellation, conflicts, pagination and viewer permissions. These captures explicitly label synthetic content “Fixtures / Mock”. The record reports zero real planning-model calls, real audits, real SDK logins and external publications. This documentation reads those records and source; it does not claim an additional test run, a backend security review or whole-app accessibility certification.
