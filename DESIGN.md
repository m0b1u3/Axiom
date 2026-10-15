---
name: Axiom Security Console — approved C implementation
description: API-connected Vue frontend; light by default with an optional navy dark theme.
colors:
  light-surface-app: "#fafafa"
  light-surface-workspace: "#fafafa"
  light-surface-primary: "#ffffff"
  light-surface-secondary: "#fbfbfc"
  light-surface-tertiary: "#f1f2f3"
  light-surface-hover: "#f0f1f2"
  light-surface-selected: "#edf3ff"
  light-surface-raised: "#ffffff"
  light-text-primary: "#20242c"
  light-text-secondary: "#636a76"
  light-text-tertiary: "#727781"
  light-text-disabled: "#adb1b7"
  light-text-inverse: "#ffffff"
  light-border-subtle: "#eff0f2"
  light-border-default: "#e5e7eb"
  light-border-strong: "#cdd2db"
  light-border-focus: "#246bfe"
  light-accent-primary: "#2761ee"
  light-accent-hover: "#195de6"
  light-accent-subtle: "#edf3ff"
  light-state-success: "#218358"
  light-state-success-bg: "#edf8f2"
  light-state-warning: "#a76612"
  light-state-warning-bg: "#fff7e8"
  light-state-danger: "#c53a3a"
  light-state-danger-bg: "#fff0ef"
  light-state-info: "#356cc4"
  light-state-info-bg: "#eff5ff"
  light-state-neutral: "#737982"
  light-state-neutral-bg: "#f1f2f3"
  light-severity-critical: "#b4232c"
  light-severity-high: "#d14a28"
  light-severity-medium: "#b97914"
  light-severity-low: "#6f7681"
  light-severity-info: "#356cc4"
  light-evidence-observed: "#356cc4"
  light-evidence-verified: "#218358"
  light-evidence-inferred: "#8056b3"
  light-evidence-contradicted: "#c53a3a"
  light-evidence-unknown: "#737982"
  light-policy-allowed: "#218358"
  light-policy-blocked: "#c53a3a"
  light-policy-review: "#a76612"
  light-execution-running: "#246bfe"
  light-execution-waiting: "#a76612"
  light-execution-complete: "#218358"
  light-execution-failed: "#c53a3a"
  light-execution-queued: "#737982"
  dark-surface-app: "#0d1118"
  dark-surface-workspace: "#0f1217"
  dark-surface-primary: "#141b27"
  dark-surface-secondary: "#182131"
  dark-surface-tertiary: "#1f2533"
  dark-surface-hover: "#242b3b"
  dark-surface-selected: "#1c2b48"
  dark-surface-raised: "#17202e"
  dark-text-primary: "#dce2ec"
  dark-text-secondary: "#9aa2ad"
  dark-text-tertiary: "#91a0b7"
  dark-text-disabled: "#474e59"
  dark-text-inverse: "#ffffff"
  dark-border-subtle: "rgba(255, 255, 255, 0.08)"
  dark-border-default: "#253044"
  dark-border-strong: "rgba(255, 255, 255, 0.24)"
  dark-border-focus: "#3b82f6"
  dark-accent-primary: "#326bfa"
  dark-accent-hover: "#2563eb"
  dark-accent-subtle: "rgba(59, 130, 246, 0.15)"
  dark-state-success: "#22c55e"
  dark-state-success-bg: "rgba(34, 197, 94, 0.14)"
  dark-state-warning: "#f59e0b"
  dark-state-warning-bg: "rgba(245, 158, 11, 0.14)"
  dark-state-danger: "#ef4444"
  dark-state-danger-bg: "rgba(239, 68, 68, 0.14)"
  dark-state-info: "#3b82f6"
  dark-state-info-bg: "rgba(59, 130, 246, 0.14)"
  dark-state-neutral: "#94a3b8"
  dark-state-neutral-bg: "rgba(148, 163, 184, 0.14)"
  dark-severity-critical: "#ef4444"
  dark-severity-high: "#f97316"
  dark-severity-medium: "#eab308"
  dark-severity-low: "#94a3b8"
  dark-severity-info: "#3b82f6"
  dark-execution-running: "#3b82f6"
  dark-execution-waiting: "#f59e0b"
  dark-execution-complete: "#22c55e"
  dark-execution-failed: "#ef4444"
  dark-execution-queued: "#6b7380"
  light-surface-sidebar: "#fbfbfc"
  dark-surface-sidebar: "#101722"
  light-border-control: "#dfe2e8"
  dark-border-control: "#344058"
typography:
  body:
    fontFamily: "\"Avenir Next\", -apple-system, BlinkMacSystemFont, \"Segoe UI\", \"PingFang SC\", \"Microsoft YaHei\", sans-serif"
    fontSize: "14px"
  headline:
    fontSize: "25px"
    fontWeight: 600
  mono:
    fontFamily: "\"JetBrains Mono Variable\", \"JetBrains Mono\", \"SFMono-Regular\", Consolas, \"Liberation Mono\", monospace"
rounded:
  radius-xs: "3px"
  radius-sm: "4px"
  radius-md: "6px"
  radius-lg: "8px"
  radius-xl: "8px"
spacing:
  space-0-5: "2px"
  space-1: "4px"
  space-1-5: "6px"
  space-2: "8px"
  space-2-5: "10px"
  space-3: "12px"
  space-4: "16px"
  space-5: "20px"
  space-6: "24px"
  space-8: "32px"
  space-7: "28px"
  space-9: "36px"
components:
  button-primary:
    backgroundColor: "{colors.light-accent-primary}"
    textColor: "{colors.light-text-inverse}"
    rounded: "{rounded.radius-md}"
    height: "32px"
    padding: "0 12px"
---

# Design System: Axiom Security Console

## Overview

C is the approved main direction. The user explicitly accepted its direction and typography (“这个可以，字体也不错”). The geometric blue A, AXIOM wordmark and Security Console subtitle define the brand. The intended feel is restrained, referencing Vercel, Claude and OpenAI through typography, whitespace and fine borders. Light is the initial default; the optional navy-black dark theme follows the supplied reference and the user's choice persists locally. No separate creative metaphor has been approved.

The current implementation is the API-connected Vue 3 / TypeScript frontend in `code-audit-web/src`. `styles/design-tokens.css` is the token source, `styles/tokens.css` maps legacy `--app-*` names, and `styles/tailwind.css` bridges component utilities to those tokens. Component styles resolve layout details. The standalone `prototype/` remains historical; A (compact blue/dark sidebar/task-first) and B (sage/light narrow rail/project-first) are archived alternatives, not the selected identity.

**Key Characteristics:**

- Quiet neutral surfaces with selective blue emphasis.
- Compact security evidence, clear metadata and deliberate whitespace.
- Short state transitions, with no continuous decorative animation.

## Colors

Frontmatter records current semantic CSS colors with explicit light/dark prefixes for documentation. Runtime tokens retain their unprefixed names and dark overrides belong to the root `.dark` class. Use accent for primary actions, navigation selection and focus; semantic status, severity, evidence and execution colors carry separate meanings. Always accompany color with a readable label. Root page surfaces must remain light until dark mode is selected; global inversion filters are not a theme mechanism. The OpenAI provider icon alone has a scoped dark-mode inversion.

## Typography

The approved Avenir Next UI family retains system, Segoe UI and Chinese fallbacks. Body baseline is 14px; the dense UI scale is 10, 11, 12, 13, 14, 16, 18 and 25px, with 30px dashboard metrics. Most controls use 13px, metadata 10–12px, and headings weight 600. Brand text is 20px/650 with .065em tracking; the subtitle is 11px. Font appearance depends on installed UI fonts. JetBrains Mono Variable is bundled through `@fontsource-variable/jetbrains-mono`, with SFMono/Consolas fallbacks; numeric UI uses tabular figures.

## Layout

The application shell uses a 228px sticky full-height sidebar, 65px topbar and content capped at 1600px, padded 36px 32px 32px. There is no prototype review strip. Dashboard metrics use four columns; the main task/project column sits beside a 285px risk/engine column with a 24px gap. Evidence screens preserve their existing scan, findings, source-code and snapshot workflow. Model configuration uses a data table and a 760px add/edit dialog with six provider choices and two field columns.

- At 1200px and below, dashboard columns stack and auxiliary panels use two columns.
- At 1100px and below, shell content/topbar padding becomes 24px.
- At 760px and below, sidebar becomes a toggled off-canvas drawer, topbar is 56px, search becomes an icon, content padding is 24px 16px, metrics use two columns and auxiliary panels stack.
- At 650px and below, model form fields stack and provider choices use three columns.
- Dialog width never exceeds viewport minus 32px and height never exceeds viewport minus 48px. The body scrolls while header/footer remain visible. Tables and source code use local overflow where necessary. Individual legacy views may have additional responsive rules.

## Elevation & Depth

Resting panels use `--shadow-raised: none`; surfaces and 1px borders establish hierarchy. Floating popovers use the tokenized two-layer shadow; dialogs use `0 16px 48px rgba(20, 24, 30, 0.16)` in light mode and its black .7-opacity counterpart in dark mode. Motion tokens are 100/150/200ms with `cubic-bezier(0.2, 0, 0, 1)`. Dialog overlay entry is 130ms and content entry 160ms with 4px movement. Loading spinners communicate pending work; no continuous decorative animation is intended. Reduced-motion rules suppress animation and transitions.

## Shapes

Token radii are 3, 4, 6 and 8px. Standard controls/navigation use 6px, dashboard panels 8px, and the shared dialog uses a component-specific 10px radius. Default controls are 32px high, small controls 28px and large controls 36px. Icons use fine strokes; BrandMark uses the local geometric SVG at 42px. Avatars are circular; main workspace surfaces remain rectangular.

## Components

Existing `/api/v1` contracts remain connected for authentication, projects, scans, findings, snapshots, model configurations and settings. Dashboard values come from actual server objects. Global search fetches real projects, scan records and findings, includes page navigation, reports partial failures and limits the displayed matches to 20. Request errors, loading indicators and empty states are part of the application; do not replace them with fabricated success data.

Model add/edit, provider model-list retrieval, connection testing, deletion and enablement use existing APIs. Enabling a model selects the exclusive default for new scans. A blank key during editing preserves the existing server credential by omitting the key field; adding a model requires a key. These are live configuration controls, not the prototype's simulated actions. Shared Reka dialogs provide accessible titles, focus handling and portaled content behavior; confirmations and toast feedback use application components. The shell includes a skip link and Ctrl/Cmd+K search.

Validation evidence at this documentation update: local Vite on port 5179 proxies the existing server at `8.145.59.173`; real login and read-only dashboard, model and evidence views were exercised. Three client contract/compiled-CSS root inversion regression tests in `tests/contracts.test.mjs` passed. The final production build passed. An isolated local fixture also verified retry polling through pending → scanning → completed without refresh, and Enter-key expansion of raw JSON. No production writes or deployment were performed during these checks. This is bounded implementation evidence, not a certification of commercial readiness, complete accessibility, backend security, or every mutation workflow.

## Do's and Don'ts

- Do preserve the approved C identity, font stack and default light mode.
- Do use semantic tokens consistently in both themes.
- Do retain real API contracts and explicit request/loading/error states.
- Do keep evidence, inference and verification status distinguishable.
- Don't treat archived prototype data or A/B variants as the current application.
- Don't introduce continuous decorative animation or page-wide inversion filters.
- Don't imply untested writes, deployment or commercial readiness certification.


Deployment update (2026-09-09): user clarified that VPS is the development environment. Frontend synchronized to 8.145.59.173:/opt/Axiom/code-audit-web, prior version backed up, server tests and production build passed. Remote login, real data and dark mode verified. Backend business-data mutations remain untested.

## Scan activity stream update

`ScanActivity.vue` provides a retained log surface for active and completed scans. Important progress is the default; all records and issues remain directly selectable. Each native details/summary record retains source metadata and the original message. UI summaries never overwrite raw text. Times render in browser local time. Follow-latest can pause; fetch errors are visible and retryable. Download is explicitly limited to loaded records (recent1500 retention). Source total and loaded count are separate. Dark/light tokens and existing typography remain unchanged. Bounded review disposition: ship; details and unresolved scanner findings are in `work/scan10-review/REVIEW.md`.
