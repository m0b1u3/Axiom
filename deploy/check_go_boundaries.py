#!/usr/bin/env python3
"""Check actual Go build/test dependencies and module graphs, without model calls."""

import argparse
import json
import os
from pathlib import Path
import subprocess
import sys


MODULES = {
    "code-audit-agent": "axiom/code-audit-agent",
    "code-audit-server": "axiom/code-audit-server",
    "contracts/go": "axiom/contracts",
    "shared/sourceidentity": "axiom/sourceidentity",
}
DATABASE_PREFIXES = ("github.com/jackc/", "modernc.org/", "github.com/mattn/go-sqlite3")
SHARED = ("axiom/contracts", "axiom/sourceidentity")


def within(name, prefix):
    return name == prefix or name.startswith(prefix + "/")


def decode_stream(raw):
    decoder = json.JSONDecoder()
    offset = 0
    while offset < len(raw):
        if raw[offset].isspace():
            offset += 1
            continue
        value, offset = decoder.raw_decode(raw, offset)
        yield value


def violations(owner, packages, modules):
    """Pure policy function, also exercised against injected regressions."""
    problems = []
    allowed = (owner,) + (SHARED if owner in MODULES.values() and owner not in SHARED else ())
    for package in packages:
        name = package.get("ImportPath", "").split(" [", 1)[0].removesuffix(".test")
        if name.startswith("axiom/") and not any(within(name, p) for p in allowed):
            problems.append(f"{owner}: forbidden package {name}")
        if owner != "axiom/code-audit-server" and (
            name in ("database/sql", "database/sql/driver") or name.startswith(DATABASE_PREFIXES)
        ):
            problems.append(f"{owner}: database dependency {name}")
        if owner == "axiom/sourceidentity" and not package.get("Standard", False) and not within(name, owner):
            problems.append(f"{owner}: source identity must use only the standard library: {name}")
    for module in modules:
        name = module.get("Path", "")
        if name.startswith("axiom/") and not any(within(name, p) for p in allowed):
            problems.append(f"{owner}: forbidden module {name}")
        if owner != "axiom/code-audit-server" and name.startswith(DATABASE_PREFIXES):
            problems.append(f"{owner}: database module {name}")
    return sorted(set(problems))


def inspect(root, directory, owner):
    # A developer's go.work must not mask independent module requirements.
    env = dict(os.environ, GOWORK="off")
    records = []
    for arguments in (
        ["list", "-mod=readonly", "-deps", "-test", "-json", "./..."],
        ["list", "-mod=readonly", "-m", "-json", "all"],
    ):
        result = subprocess.run(["go", *arguments], cwd=root / directory, env=env, text=True, capture_output=True)
        if result.returncode:
            raise RuntimeError(f"{directory}: dependency inspection failed\n{result.stderr.strip()}")
        records.append(list(decode_stream(result.stdout)))
    packages, modules = records
    return {"module": owner, "packagesIncludingTests": len(packages), "modules": len(modules)}, violations(owner, packages, modules)


def check(root):
    report, problems = [], []
    for directory, owner in MODULES.items():
        summary, found = inspect(root, directory, owner)
        report.append(summary)
        problems.extend(found)
    for obsolete in ("pkg/store", "pkg/providers", "pkg/cryptokey", "pkg/sourceidentity", "cmd/migrate"):
        if (root / "code-audit-agent" / obsolete).exists():
            problems.append(f"database/configuration ownership returned to Agent: {obsolete}")
    if not (root / "code-audit-server/internal/store/migrations/009_observation_contract.sql").is_file():
        problems.append("Server must own the existing migration history")
    for directory in ("contracts", "contracts/ts", "code-audit-web"):
        package = json.loads((root / directory / "package.json").read_text())
        for section in ("dependencies", "devDependencies", "optionalDependencies", "peerDependencies"):
            if any(name.startswith("@openai/codex") for name in package.get(section, {})):
                problems.append(f"{directory}: SDK dependencies belong to Scanner")
    return report, problems


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parents[1])
    args = parser.parse_args()
    try:
        report, problems = check(args.root.resolve())
    except (OSError, ValueError, RuntimeError) as error:
        print(error, file=sys.stderr)
        return 1
    print(json.dumps({"modules": report, "violations": problems}, ensure_ascii=False, indent=2))
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main())
