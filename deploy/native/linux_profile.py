#!/usr/bin/env python3
"""Generate, but do not load, a bwrap policy for this disposable Actions host."""
import argparse
import hashlib
import json
import os
from pathlib import Path


def profile_for(binary, expected, template):
    binary = Path(binary).resolve(strict=True)
    if any(c in str(binary) for c in ('"', '\\', '*', '?', '[', ']', '{', '}', '\n')):
        raise ValueError("Policy attachment must be one literal executable path")
    if hashlib.sha256(binary.read_bytes()).hexdigest() != expected:
        raise ValueError("Sandbox helper differs from the pinned SDK inventory")
    if "flags=(unconfined)" in template or "audit deny capability," not in template:
        raise ValueError("Namespace capability boundary is missing")
    if template.count("@BWRAP@") != 1:
        raise ValueError("Unexpected policy template")
    return template.replace("@BWRAP@", str(binary))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--baseline", type=Path, required=True)
    args = parser.parse_args()
    if os.environ.get("GITHUB_ACTIONS") != "true" or os.environ.get("RUNNER_OS") != "Linux":
        raise ValueError("This generator is limited to a disposable GitHub Linux runner")
    baseline = json.loads(args.baseline.read_text())
    row = next(x for x in baseline["checks"] if x["id"] == "native-sandbox-preflight")
    if row["status"] != "failed" or "bwrap:" not in row.get("error", ""):
        raise ValueError("No observed bwrap host-prerequisite failure to diagnose")
    here = Path(__file__).resolve().parent
    root = here.parents[1]
    manifest = json.loads((root / "deploy/sdk/manifest.json").read_text())
    package = next(p for p in manifest["packages"] if p.get("platform") == "linux-x64")
    relative = next(name for name in package["files"] if name.endswith("/codex-resources/bwrap"))
    binary = root / "code-audit-scanner/node_modules" / package["path"] / relative
    policy = profile_for(binary, package["files"][relative], (here / "linux-bwrap.profile.in").read_text())
    args.output.write_text(policy)
    args.output.with_suffix(".json").write_text(json.dumps({"scope": "disposable diagnostic only", "binary": str(binary),
        "binarySha256": package["files"][relative], "profileSha256": hashlib.sha256(policy.encode()).hexdigest(),
        "globalSysctlChanged": False, "sdkSandboxDisabled": False, "childCapabilitiesDenied": True}, indent=2) + "\n")


if __name__ == "__main__":
    main()
