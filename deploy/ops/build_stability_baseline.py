#!/usr/bin/env python3
"""Build a pinned internal baseline from Git source, never a business installation."""
import argparse
import json
from pathlib import Path
import re
import subprocess
import sys
import tarfile

from stack import ROOT, APP_SERVICES

sys.path.insert(0, str(ROOT/"deploy/release"))
from source import export


def build(repository, work, prefix="axiom"):
    repository, work = Path(repository).resolve(), Path(work).resolve()
    if work.exists():
        raise ValueError("Baseline build directory must be new")
    if not re.fullmatch(r"[a-z0-9][a-z0-9./:_-]{0,250}", prefix):
        raise ValueError("Invalid baseline image prefix")
    baseline = json.loads((ROOT/"deploy/release/stability-baseline.json").read_text())
    revision, expected = baseline["revision"], baseline["sourceTreeSha256"]
    if baseline["kind"] != "internal-commit-baseline" or baseline["sdkVersion"] != "0.1.25" or not re.fullmatch(r"[a-f0-9]{40}", revision):
        raise ValueError("Unsupported immutable baseline")
    if subprocess.check_output(["git","-C",str(repository),"cat-file","-t",revision],text=True).strip() != "commit":
        raise ValueError("Baseline is not a commit")
    work.mkdir(parents=True, mode=0o700)
    archive = work/"git-source.tar"
    subprocess.run(["git","-C",str(repository),"archive","--format=tar","--output",str(archive),revision],check=True)
    checkout = work/"checkout"; checkout.mkdir()
    with tarfile.open(archive) as source:
        if any(not (entry.isfile() or entry.isdir()) for entry in source.getmembers()):
            raise ValueError("Baseline source contains links or special files")
        source.extractall(checkout, filter="data")
    manifest = export(checkout, work/"source", revision=revision)
    if manifest["sourceTreeSha256"] != expected:
        raise ValueError("Baseline source tree differs from the pinned release evidence")
    sdk = json.loads((work/"source/code-audit-scanner/package.json").read_text())["dependencies"]["@openai/codex-security"]
    if sdk != baseline["sdkVersion"]:
        raise ValueError("Baseline SDK differs from the pinned version")
    images = {}
    for service in APP_SERVICES:
        reference = f"{prefix}/{service}:{baseline['imageRelease']}"
        previous = subprocess.run(["docker","image","inspect",reference],stdout=subprocess.PIPE,stderr=subprocess.DEVNULL)
        if previous.returncode:
            filename = "Dockerfile.web.production" if service == "web" else "Dockerfile."+service
            command = ["docker","build","--platform","linux/amd64","--file",str(work/"source/deploy/docker"/filename),
                       "--build-arg","AXIOM_BUILD_VERSION="+baseline["imageRelease"],"--label","org.opencontainers.image.revision="+revision,
                       "--label","io.axiom.source-sha256="+expected,"--tag",reference,str(work/"source")]
            with (work/(service+"-build.log")).open("wb") as log:
                subprocess.run(command,stdout=log,stderr=subprocess.STDOUT,check=True)
        info = json.loads(subprocess.check_output(["docker","image","inspect",reference]))[0]
        assert info["Os"] == "linux" and info["Architecture"] == "amd64"
        assert info["Config"]["Labels"]["org.opencontainers.image.revision"] == revision
        assert info["Config"]["Labels"]["io.axiom.source-sha256"] == expected
        images[service] = {"reference":reference,"imageId":info["Id"]}
    report = {"status":"passed",**baseline,"images":images,"businessDatabaseAccess":False,"modelCalls":0,"realSDKLogins":0,"externalPublications":0}
    (work/"report.json").write_text(json.dumps(report,indent=2)+"\n")
    print(json.dumps({"status":"passed","baselineRevision":revision,"sdk":sdk,"images":images}),flush=True)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repository",type=Path,default=ROOT); parser.add_argument("--work",type=Path,required=True); parser.add_argument("--image-prefix",default="axiom")
    args = parser.parse_args(); build(args.repository,args.work,args.image_prefix)
