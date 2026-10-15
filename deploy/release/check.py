#!/usr/bin/env python3
"""Check local Beta delivery and, optionally, CI for one exact remote commit."""
import argparse
import hashlib
import json
from pathlib import Path
import re
import subprocess
from urllib.parse import urlparse

from source import ROOT, selected_files, version

WORKFLOWS = ["contracts.yml", "go-boundaries.yml", "input-chain.yml", "production-stack.yml", "release-candidate.yml"]
REQUIRED = ["LICENSE", "NOTICE", "README.md", "CONTRIBUTING.md", "SECURITY.md", "CHANGELOG.md",
            "docs/RELEASE_PROCESS.md", "docs/BETA_INSTALLATION.md", ".github/PULL_REQUEST_TEMPLATE.md",
            ".github/ISSUE_TEMPLATE/bug_report.yml", ".github/ISSUE_TEMPLATE/feature_request.yml",
            ".github/ISSUE_TEMPLATE/config.yml"]


def check(root=ROOT):
    root = Path(root)
    release = json.loads((root/"deploy/release/release.json").read_text())
    if release.get("schemaVersion") != 1 or release.get("version") != version(root) or release.get("channel") != "beta":
        raise ValueError("Release metadata and VERSION disagree")
    if (release.get("platforms") != ["linux/amd64"] or release.get("deployment") != "single-host-trusted-users"
            or release.get("license") != "Apache-2.0" or release.get("realSDKAcceptance") != "not-run"):
        raise ValueError("Beta support or validation boundary changed; update the release policy deliberately")
    repo = release.get("repository", "")
    parsed = urlparse(repo)
    if parsed.scheme != "https" or parsed.netloc != "github.com" or not re.fullmatch(r"/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", parsed.path):
        raise ValueError("Release repository must be an explicit GitHub repository URL")
    if release.get("securityReportUrl") != repo+"/security/advisories/new":
        raise ValueError("Security reporting URL differs from release repository")
    for relative in REQUIRED:
        path = root/relative
        if not path.is_file() or path.is_symlink() or not path.read_text().strip():
            raise ValueError(f"Missing release document: {relative}")
    if hashlib.sha256((root/"LICENSE").read_bytes()).hexdigest() != "d17f227e4df5da1600391338865ce0f3055211760a36688f816941d58232d8dc":
        raise ValueError("Apache-2.0 license text differs from the preserved upstream license")
    for relative in ("SECURITY.md", ".github/ISSUE_TEMPLATE/config.yml"):
        if release["securityReportUrl"] not in (root/relative).read_text():
            raise ValueError(f"Security reporting channel missing from {relative}")
    for package in ("contracts", "contracts/ts", "code-audit-scanner", "code-audit-web"):
        if json.loads((root/package/"package.json").read_text()).get("license") != "Apache-2.0":
            raise ValueError(f"Missing package license: {package}")
    for name in WORKFLOWS:
        text = (root/".github/workflows"/name).read_text()
        if "workflow_dispatch:" not in text or "contents: read" not in text or "timeout-minutes:" not in text:
            raise ValueError(f"Workflow needs manual entry, minimal permissions and timeout: {name}")
        if "pull_request_target" in text or "secrets." in text:
            raise ValueError(f"Release CI must not expose secrets to candidate code: {name}")
        for action in re.findall(r"uses:\s*([^\s]+)", text):
            if not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_./-]+@[0-9a-f]{40}", action):
                raise ValueError(f"Unpinned workflow action in {name}")
    files = selected_files(root)
    return {"version": release["version"], "platforms": release["platforms"],
            "localReleaseChecks": "passed", "sourceFiles": len(files), "realSDKAcceptance": "not-run",
            "repository": repo, "securityReportUrl": release["securityReportUrl"]}


def remote_check(repository, revision):
    if not re.fullmatch(r"[0-9a-f]{40}", revision or ""):
        raise ValueError("Remote checks require --revision with a full commit SHA")
    slug = urlparse(repository).path.strip("/")
    def api(path):
        return json.loads(subprocess.check_output(["gh", "api", path], text=True))
    if api(f"repos/{slug}/private-vulnerability-reporting").get("enabled") is not True:
        raise ValueError("Private vulnerability reporting is not enabled remotely")
    runs = api(f"repos/{slug}/actions/runs?head_sha={revision}&per_page=100")["workflow_runs"]
    evidence = []
    for name in WORKFLOWS:
        matches = [run for run in runs if run.get("head_sha") == revision
                   and run.get("path", "").split("@", 1)[0] == ".github/workflows/"+name]
        if not matches:
            raise ValueError(f"No remote run for exact commit: {name}")
        latest = max(matches, key=lambda run: (run["run_number"], run.get("run_attempt", 1)))
        if latest.get("status") != "completed" or latest.get("conclusion") != "success":
            raise ValueError(f"Remote workflow has not passed: {name}: {latest['html_url']}")
        evidence.append({"workflow": name, "url": latest["html_url"], "conclusion": "success"})
    return {"revision": revision, "privateVulnerabilityReporting": "enabled", "workflows": evidence}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--remote", action="store_true")
    parser.add_argument("--revision")
    parser.add_argument("--report")
    args = parser.parse_args()
    report = check()
    if args.remote:
        report["remote"] = remote_check(report["repository"], args.revision)
    if args.report:
        with Path(args.report).open("x") as output:
            json.dump(report, output, ensure_ascii=False, indent=2)
            output.write("\n")
    print(json.dumps(report, ensure_ascii=False))


if __name__ == "__main__":
    main()
