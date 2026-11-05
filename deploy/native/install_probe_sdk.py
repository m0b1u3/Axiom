#!/usr/bin/env python3
"""Install the existing npm lock with the selected portable Node, then verify."""
import json
import os
from pathlib import Path
import subprocess
import sys

root = Path(__file__).resolve().parents[2]
runtime = json.loads(Path(sys.argv[1]).read_text())
out = Path(sys.argv[1]).parent
environment = dict(os.environ, PATH=os.pathsep.join(runtime["path"]))
with (out / "sdk-install.log").open("w", encoding="utf-8") as log:
    for directory in ("contracts", "code-audit-scanner"):
        subprocess.run([runtime["node"], runtime["npmCli"], "ci", "--ignore-scripts", "--no-audit", "--no-fund"],
                       cwd=root / directory, env=environment, stdout=log, stderr=subprocess.STDOUT, check=True)
    for arguments in ([], ["--check"]):
        subprocess.run([runtime["node"], str(root / "deploy/sdk/apply.mjs"), *arguments],
                       cwd=root, env=environment, stdout=log, stderr=subprocess.STDOUT, check=True)
