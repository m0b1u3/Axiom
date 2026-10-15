# Axiom SDK build inputs

`manifest.json` pins official npm packages, complete file hashes and five Axiom modifications.
`overrides/` contains source files copied from the working deployment or its customized plugin cache.
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
node deploy/sdk/rebuild.mjs --output /absolute/new/path
```

Detailed audit and deployment instructions: `docs/SDK_REPRODUCIBILITY.md`.
