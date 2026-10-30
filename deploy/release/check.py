#!/usr/bin/env python3
"""Check local Beta delivery and, optionally, CI for one exact remote commit."""
import argparse
import hashlib
import json
from pathlib import Path
import re
import subprocess
from urllib.parse import urlparse

from source import ROOT, entry_for, selected_files, tree_hash, version

WORKFLOWS = ["contracts.yml", "go-boundaries.yml", "input-chain.yml", "production-stack.yml", "release-candidate.yml"]
REQUIRED = ["LICENSE", "NOTICE", "README.md", "CONTRIBUTING.md", "SECURITY.md", "CHANGELOG.md",
            "docs/RELEASE_PROCESS.md", "docs/BETA_INSTALLATION.md", ".github/PULL_REQUEST_TEMPLATE.md",
            ".github/ISSUE_TEMPLATE/bug_report.yml", ".github/ISSUE_TEMPLATE/feature_request.yml",
            ".github/ISSUE_TEMPLATE/config.yml"]


def check_real_sdk_acceptance(root, release):
    """Preserve scoped historical evidence without certifying this candidate."""
    status = release.get("realSDKAcceptance")
    reference = release.get("realSDKAcceptanceEvidence")
    if status == "not-run" and reference is None:
        return {"status": "not-run"}
    if status != "limited" or not isinstance(reference, dict):
        raise ValueError("Real SDK acceptance must be not-run or limited with reviewed evidence")
    relative = reference.get("path", "")
    if not re.fullmatch(r"docs/acceptance/[a-z0-9-]+\.json", relative):
        raise ValueError("Acceptance evidence must be a source-controlled docs/acceptance JSON file")
    path = Path(root)/relative
    if not path.is_file() or path.is_symlink():
        raise ValueError("Acceptance evidence is missing or not a regular source file")
    expected = reference.get("sha256", "")
    if not re.fullmatch(r"[a-f0-9]{64}", expected) or hashlib.sha256(path.read_bytes()).hexdigest() != expected:
        raise ValueError("Acceptance evidence SHA-256 mismatch")
    evidence = json.loads(path.read_text())
    if (evidence.get("schemaVersion") != 1 or evidence.get("kind") != "scoped-real-model-acceptance"
            or evidence.get("status") != "passed_for_recorded_deployment"
            or evidence.get("candidateRelationship") != "historical_workflow_evidence"
            or evidence.get("provider") != "deepseek" or evidence.get("sdkVersion") != "0.5.0"
            or evidence.get("wireAPI") != "responses" or evidence.get("reviewOutputMode") != "native_schema"
            or evidence.get("reviewStrictSchema") is not True):
        raise ValueError("Acceptance evidence does not match the reviewed Responses / Schema scope")
    workflow = evidence.get("workflow", {})
    if any(workflow.get(k) != "passed" for k in ("scan", "reviewReport", "sourceReferences", "formalArchive", "evidenceGraph")):
        raise ValueError("Acceptance evidence has an incomplete workflow")
    requests = evidence.get("requests", {})
    if (any(type(requests.get(k)) is not int or requests[k] < 0 for k in ("scan", "review", "total", "failed", "automaticRetries"))
            or requests["scan"] == 0 or requests["review"] == 0
            or requests["scan"]+requests["review"] != requests["total"]
            or requests["failed"] != 0 or requests["automaticRetries"] != 0):
        raise ValueError("Acceptance request counts are missing or inconsistent")
    for value in (release, evidence):
        if (value.get("currentCandidateRealModelAcceptance") != "not-run"
                or value.get("configuredPricingRealAcceptance") != "not-run"):
            raise ValueError("Historical workflow evidence cannot certify candidate pricing or a fresh model run")
    if evidence.get("providerInvoiceReconciled") is not False or not evidence.get("limitations"):
        raise ValueError("Limited acceptance must preserve billing and coverage limitations")
    return {"status": "limited", "evidence": relative, "sha256": expected,
            "currentCandidateRealModelAcceptance": "not-run", "configuredPricingRealAcceptance": "not-run"}


def check(root=ROOT):
    root = Path(root).resolve()
    release = json.loads((root/"deploy/release/release.json").read_text())
    baseline = json.loads((root/"deploy/release/stability-baseline.json").read_text())
    if (baseline.get("schemaVersion") != 1 or baseline.get("kind") != "internal-commit-baseline"
            or baseline.get("repository") != release.get("repository") or baseline.get("sdkVersion") != "0.1.25"
            or not re.fullmatch(r"[a-f0-9]{40}", baseline.get("revision", ""))
            or not re.fullmatch(r"[a-f0-9]{64}", baseline.get("sourceTreeSha256", ""))
            or "ref: "+baseline["revision"] not in (root/".github/workflows/production-stack.yml").read_text()):
        raise ValueError("Immutable stability baseline and CI checkout differ")
    if release.get("schemaVersion") != 1 or release.get("version") != version(root) or release.get("channel") != "beta":
        raise ValueError("Release metadata and VERSION disagree")
    if (release.get("platforms") != ["linux/amd64"] or release.get("deployment") != "single-host-trusted-users"
            or release.get("license") != "Apache-2.0"):
        raise ValueError("Beta support or validation boundary changed; update the release policy deliberately")
    acceptance = check_real_sdk_acceptance(root, release)
    if acceptance["status"] == "limited":
        reference = release.get("referenceDeployment", {})
        candidate = release.get("sourceCandidate", reference)
        sdk = json.loads((root/"code-audit-scanner/package.json").read_text())["dependencies"]["@openai/codex-security"]
        description = re.search(r"SCANNER_DESCRIPTION_VERSION = (\d+)",
                                (root/"code-audit-scanner/src/runtime/contract-baseline.gen.ts").read_text())
        latest = max(p.name for p in (root/"code-audit-server/internal/store/migrations").glob("*.sql"))
        if (reference.get("provider") != "deepseek" or reference.get("wireAPI") != "responses"
                or reference.get("validationOutputMode") != "native_schema"
                or candidate.get("sdkVersion") != sdk or description is None
                or candidate.get("scannerDescriptionVersion") != int(description[1])
                or candidate.get("latestMigration") != latest):
            raise ValueError("Reference deployment metadata disagrees with the candidate SDK, protocol or migration")
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
    check_python_syntax(root, files)
    return {"version": release["version"], "platforms": release["platforms"],
            "localReleaseChecks": "passed", "sourceFiles": len(files), "realSDKAcceptance": acceptance["status"],
            "realSDKAcceptanceDetail": acceptance,
            "sourceTreeSha256": tree_hash([entry_for(path, root) for path in files]),
            "repository": repo, "securityReportUrl": release["securityReportUrl"]}


def check_python_syntax(root, files=None):
    root = Path(root).resolve()
    for path in files if files is not None else selected_files(root):
        if path.suffix == ".py" and path.relative_to(root).parts[0] == "deploy":
            try:
                compile(path.read_bytes(), str(path), "exec")
            except SyntaxError as error:
                raise ValueError(f"Invalid deployment Python source: {path.relative_to(root)}") from error


def git_inventory(root):
    root = Path(root).resolve()
    result = {}
    for path in selected_files(root):
        data = path.read_bytes()
        header = f"blob {len(data)}\0".encode()
        result[path.relative_to(root).as_posix()] = {
            "sha":hashlib.sha1(header+data, usedforsecurity=False).hexdigest(),
            "mode":"100755" if path.stat().st_mode & 0o111 else "100644"}
    return result


def remote_check(repository, revision, root=ROOT):
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
    tree = api(f"repos/{slug}/git/trees/{revision}?recursive=1")
    if tree.get("truncated") is not False:
        raise ValueError("Remote source inventory is incomplete")
    remote = {entry["path"]:{"sha":entry["sha"], "mode":entry["mode"]}
              for entry in tree["tree"] if entry["type"] != "tree"}
    if remote != git_inventory(root):
        raise ValueError("Local source tree does not match the exact remote candidate commit")
    return {"revision": revision, "privateVulnerabilityReporting": "enabled", "sourceMatchesCommit":True,
            "workflows": evidence}


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
