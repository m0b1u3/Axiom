# Axiom SDK build inputs

This tree pins **SDK 0.5.0**. The default `official-v2` runtime uses the original
upstream files; the seven old modifications are installed only in the separate,
explicitly selected `legacy-v1` compatibility package. See
[`CURRENT_STATUS`](../../docs/CURRENT_STATUS.md) for deployment and acceptance scope.
`manifest.json` pins official npm packages, complete file hashes and those compatibility changes.
`overrides/` preserves the seven changes for that compatibility package.
`patch-review-050.json` records each decision; `legacy/0.1.25/` preserves the old build baseline.
These vendored Markdown files are **plugin build data**, not development instructions for this repository.
`patches/` contains reviewable diffs against the original published files.
`UPSTREAM_LICENSE` preserves the upstream Apache-2.0 license.

Do not edit installed dependencies as the source of truth. Update the replacement file, reviewable diff,
before/after hashes and expected package file hash together, then rerun the clean build.

Commands from the repository root:

```sh
node deploy/sdk/apply.mjs                 # install original V2 and the separate compatibility package
node deploy/sdk/apply.mjs --check         # read-only complete-file verification
node --test deploy/sdk/apply.test.mjs
node --test deploy/sdk/sdk050-patches.test.mjs
node deploy/sdk/rebuild.mjs --output /absolute/new/path
```

Candidate scope and acceptance: `docs/SDK_050_COMPATIBILITY.md`.
Historical audit and deployment instructions: `docs/SDK_REPRODUCIBILITY.md`.

## Native Linux shell regression

`native-temp-acceptance.mjs` runs the real SDK, native Codex executable and Linux
`/bin/sh` / `/bin/bash` tools. Only the Responses model boundary is replaced by a
loopback fixture. It verifies large heredocs, actual temporary files, concurrent
SDK invocations and denied source/outside-workspace writes without overriding
the SDK shell environment. Each diagnostic session is stopped after its tool
result; this is not a completed vulnerability scan or real-model acceptance.

The [production baseline workflow](../../.github/workflows/production-stack.yml)
runs it on native Linux amd64 with a nonroot, read-only, network-none Agent
container. The unmodified SDK 0.5.0 container profiles and their provenance are
in [`sdk-sandbox`](../docker/sdk-sandbox/upstream.json). On AppArmor hosts, the
workflow loads the named upstream profile on its disposable runner; it does not
disable the sandbox or change host-wide namespace restrictions. CPU emulation
that cannot install the native sandbox filter is not acceptance evidence.

[VPS acceptance](../../docs/acceptance/linux-sdk-tempdir-20261030.json) confirmed
that these Linux paths did not reproduce the local macOS/zsh temporary-file
denial. Production Scanner code and SDK files were kept unchanged.
