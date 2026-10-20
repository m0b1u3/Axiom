# Axiom SDK build inputs

This worktree is the isolated **SDK 0.5.0 candidate**; the existing deployment remains on 0.1.25.
`manifest.json` pins official npm packages, complete file hashes and seven Axiom modifications.
`overrides/` rebases only those seven changes onto the original 0.5.0 published files.
`patch-review-050.json` records each decision; `legacy/0.1.25/` preserves the old build baseline.
These vendored Markdown files are **plugin build data**, not development instructions for this repository.
`patches/` contains reviewable diffs against the original published files.
`UPSTREAM_LICENSE` preserves the upstream Apache-2.0 license.

Do not edit installed dependencies as the source of truth. Update the replacement file, reviewable diff,
before/after hashes and expected package file hash together, then rerun the clean build.

Commands from the repository root:

```sh
node deploy/sdk/apply.mjs                 # apply known changes only
node deploy/sdk/apply.mjs --check         # read-only complete-file verification
node --test deploy/sdk/apply.test.mjs
node --test deploy/sdk/sdk050-patches.test.mjs
node deploy/sdk/rebuild.mjs --output /absolute/new/path
```

Candidate scope and acceptance: `docs/SDK_050_COMPATIBILITY.md`.
Historical audit and deployment instructions: `docs/SDK_REPRODUCIBILITY.md`.
