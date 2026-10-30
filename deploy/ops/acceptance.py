#!/usr/bin/env python3
"""Production-image acceptance in fresh, disposable Compose projects.

The Agent's test-only network has no external access. A mounted SDK fixture
replaces all model calls. Existing application stacks are never selected.
"""
import argparse
import hashlib
import io
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import time
from urllib.error import HTTPError, URLError
from urllib.parse import quote
from urllib.request import Request, urlopen
import zipfile

from stack import APP_SERVICES, ROOT, Stack, backup, env_bytes, private_write, read_env, restore, verify_backup


def wait_for(check, seconds, message):
    until = time.monotonic() + seconds
    while time.monotonic() < until:
        value = check()
        if value:
            return value
        time.sleep(0.25)
    raise AssertionError(message)


class Web:
    def __init__(self, stack, timeout=10):
        self.base = "http://" + stack.output("port", "web", "8080").splitlines()[0]
        self.token = None
        self.values = stack.values
        self.timeout = timeout

    def raw(self, method, path, payload=None, content_type="application/json"):
        body = payload if isinstance(payload, bytes) else (json.dumps(payload).encode() if payload is not None else None)
        headers = {"Content-Type": content_type}
        if self.token:
            headers["Authorization"] = "Bearer " + self.token
        request = Request(self.base+path, data=body, headers=headers, method=method)
        try:
            with urlopen(request, timeout=self.timeout) as response:
                return response.status, response.read(), response.headers
        except HTTPError as error:
            return error.code, error.read(), error.headers

    def data(self, method, path, payload=None):
        code, body, _ = self.raw(method, path, payload)
        assert code == 200, f"Web API {path}: HTTP {code}"
        value = json.loads(body)
        assert value["code"] == 0, f"Web API {path}: rejected"
        return value["data"]

    def login(self):
        self.token = self.data("POST", "/api/v1/auth/login", {"username":self.values.get("AXIOM_ADMIN_USER", "admin"), "password":self.values["AXIOM_ADMIN_PASSWORD"]})["token"]


def task_wait(web, task_id):
    def get():
        try:
            value = web.data("GET", f"/api/v1/scans/{task_id}")
            return value if value["status"] in ("completed", "partial", "failed", "canceled", "timeout") else None
        except (URLError, AssertionError):
            return None
    return wait_for(get, 50, "fixture scan did not reach a terminal state")


def fixture_token_pricing():
    """One USD per million synthetic Tokens; never a real provider price."""
    return {"schemaVersion":1, "currency":"USD", "unit":"per_million_tokens", "cacheMode":"input_only",
            "inputPerMillion":"1", "outputPerMillion":"1", "cacheReadPerMillion":None,
            "cacheWritePerMillion":None, "note":"Fixtures / Mock only: synthetic usage, no model calls or supplier charge."}


def fixture_credentials(stack, create=False):
    """Private, synthetic SDK-state sentinel; never authenticate with its content."""
    assert stack.project.startswith("axiom-accept-")
    script = """const fs=require('fs'),c=require('crypto'),path=require('path');
const folder='/app/data/.sdk-identities/release-fixture',file=path.join(folder,'auth.json');
if(CREATE){fs.mkdirSync(folder,{recursive:true,mode:448});fs.writeFileSync(file,JSON.stringify({fixtureOnly:true,credential:'synthetic-never-authenticate'}),{mode:384,flag:'wx'})}
console.log(JSON.stringify({sha256:c.createHash('sha256').update(fs.readFileSync(file)).digest('hex'),mode:fs.statSync(file).mode&511,directoryMode:fs.statSync(folder).mode&511,uid:fs.statSync(file).uid,gid:fs.statSync(file).gid}));
""".replace("CREATE", "true" if create else "false")
    result = json.loads(stack.output("exec", "-T", "agent", "node", "-e", script))
    assert result["mode"] == 0o600 and result["directoryMode"] == 0o700
    return result


def verify_runtime_baseline(runtime, matrix, description, identity, parameters, concurrency, sdk_version, release, adapter=None, description_contract=None):
    assert runtime["state"] == "ready", "Agent runtime is not ready"
    assert runtime["controlAPI"] == max(matrix["controlAPIs"]), "Agent selected a different control API"
    # Independent SDK capabilities advance the Scanner description without
    # adding new scan input/job profiles to the execution matrix. Check the
    # installed bundle against all current baselines, not only identity v10.
    expected_description = max(item["descriptionVersion"] for item in (identity, parameters, concurrency))
    if adapter is not None:
        assert adapter["sdkVersion"] == sdk_version, "SDK adapter differs from the pinned dependency"
        expected_description = max(expected_description, adapter["descriptionVersion"])
        assert description["sdkResultAdapterVersion"] == adapter["adapterVersion"], "SDK result adapter version drift"
    if description_contract is not None:
        assert description_contract["schemaVersion"] >= expected_description, "Scanner contract predates its SDK capability baselines"
        expected_description = description_contract["schemaVersion"]
        for key, value in description_contract.items():
            assert description.get(key) == value, f"Scanner {key} differs from its generated contract"
    assert description["schemaVersion"] == expected_description, "Scanner description differs from the SDK capability baselines"
    assert description["sdkIdentityVersion"] == identity["identityVersion"], "Scanner identity contract differs from the baseline"
    assert description["sdkParametersVersion"] == parameters["parametersVersion"], "Scanner parameters contract differs from the baseline"
    assert description["sdkConcurrencyVersion"] == concurrency["concurrencyVersion"], "Scanner concurrency contract differs from the baseline"
    if adapter is None:
        assert parameters["sdkVersion"] == concurrency["sdkVersion"] == sdk_version, "SDK capability baselines differ from the pinned dependency"
    else:
        assert parameters["sdkVersion"] in adapter["readableSDKVersions"] and concurrency["sdkVersion"] in adapter["readableSDKVersions"], "SDK capability baselines differ from the pinned dependency"
    assert runtime["scannerDescriptionVersion"] == description["schemaVersion"], "Agent did not report the installed Scanner description"
    assert description["configuredSdk"] == {"package":"@openai/codex-security", "version":sdk_version}, "Scanner SDK differs from the pinned dependency"
    assert runtime["agentBuild"] == release and runtime["scannerBuild"], "Agent/Scanner build identity is missing or stale"


def runtime_baseline(sdk_version):
    """Select a pinned baseline, never relabel a 0.1.25 runtime as 0.5.0."""
    current = json.loads((ROOT/"code-audit-scanner/package.json").read_text())["dependencies"]["@openai/codex-security"]
    if sdk_version == current:
        matrix_path = ROOT/"contracts/data/execution-compatibility.json"
        generated = (ROOT/"code-audit-scanner/src/runtime/contract-baseline.gen.ts").read_text()
        version = re.search(r"^export const SCANNER_DESCRIPTION_VERSION = (\d+) as const;$", generated, re.MULTILINE)
        assert version is not None, "Generated Scanner description version is missing"
        version = int(version[1])
        schema = json.loads((ROOT/f"contracts/schemas/v{version}/scanner-description.schema.json").read_text())
        properties = schema["$defs"][f"ScannerDescriptionV{version}"]["properties"]
        description_contract = {key:value["const"] for key,value in properties.items() if "const" in value}
        assert description_contract["schemaVersion"] == version, "Generated Scanner description and schema differ"
        return {"sdkVersion":current, "matrix":json.loads(matrix_path.read_text()), "matrixHash":hashlib.sha256(matrix_path.read_bytes()).hexdigest(),
                "descriptionContract":description_contract,
                **{key:json.loads((ROOT/("contracts/data/sdk-"+key+"-compatibility.json")).read_text()) for key in ("identity", "parameters", "concurrency", "adapter")}}
    if sdk_version == "0.1.25":
        baseline = json.loads((ROOT/"deploy/sdk/legacy/0.1.25/execution-baseline.json").read_text())
        assert baseline["schemaVersion"] == 1 and baseline["sdkVersion"] == sdk_version
        return baseline
    raise AssertionError("SDK runtime has no certified compatibility baseline")


def verify_stack_runtime(stack, web, release):
    version = stack.output("exec", "-T", "agent", "node", "-p", "require('/opt/axiom/code-audit-scanner/node_modules/@openai/codex-security/package.json').version")
    baseline = runtime_baseline(version)
    def matching_runtime():
        agents = web.data("GET", "/api/v1/executions/overview")["agents"]
        return next((a.get("runtime") for a in agents if a.get("runtime", {}).get("matrixHash") == baseline["matrixHash"]), None)
    runtime = wait_for(matching_runtime, 30, "Agent did not publish its version's compatibility matrix")
    description = json.loads(stack.output("exec", "-T", "agent", "node", "/opt/axiom/code-audit-scanner/dist/main.js", "--describe"))
    verify_runtime_baseline(runtime,baseline["matrix"],description,baseline["identity"],baseline["parameters"],baseline["concurrency"],version,release,baseline["adapter"],baseline.get("descriptionContract"))


def suite(env_file, work, candidate_env=None):
    work = Path(work).resolve()
    work.mkdir(parents=True, exist_ok=True)
    values = read_env(env_file)
    candidate_values = read_env(candidate_env) if candidate_env else None
    if candidate_values is not None:
        changed = {key for key in values.keys() | candidate_values.keys() if values.get(key) != candidate_values.get(key)}
        assert changed and changed <= {"AXIOM_RELEASE", "AXIOM_IMAGE_PREFIX"}, "Candidate may change only release image selection"
    assert values["AXIOM_STACK_NAME"].startswith("axiom-accept-"), "Acceptance requires an axiom-accept-* project"
    original_project = values["AXIOM_STACK_NAME"]
    restored_project = original_project+"-restore"
    extra = work / "fixture.compose.yml"
    extra.write_text("""services:
  server:
    networks: [default, fixture-only]
  agent:
    networks: [fixture-only]
    environment:
      AXIOM_INPUT_TEST_CAPTURE: /app/data/acceptance/sdk.json
      AXIOM_RESULT_TEST_SCENARIO: reliability-controlled
      AXIOM_ACCEPTANCE_PRICED_USAGE_FIXTURE: '1'
      NODE_OPTIONS: --import=/opt/axiom/contracts/tests/input-sdk-register.mjs
    volumes:
      - type: bind
        source: FIXTURE_PATH
        target: /opt/axiom/contracts/tests
        read_only: true
networks:
  fixture-only:
    internal: true
""".replace("FIXTURE_PATH", json.dumps(str(ROOT/"contracts/tests"))))
    stack = Stack(env_file, [extra])
    stack.ensure_unused()
    restored = None
    checks = []
    try:
        stack.call("up", "-d", "--no-build", "--wait", "--wait-timeout", "120")
        web = Web(stack)
        web.login()
        credentials = fixture_credentials(stack, create=True)
        assert web.raw("GET", "/readyz")[0] == 200
        assert web.raw("GET", "/scans/new")[0] == 200
        assert b"/src/main.ts" not in web.raw("GET", "/")[1]
        assert web.raw("GET", "/assets/missing-test-asset.js")[0] == 404
        assert web.raw("POST", "/api/internal/v4/executions/claim", {})[0] == 404
        checks.append("static Web, SPA refresh, readiness, missing assets, private Agent API")

        initial_sdk = stack.output("exec", "-T", "agent", "node", "-p", "require('/opt/axiom/code-audit-scanner/node_modules/@openai/codex-security/package.json').version")
        model_input = {"name":"Fixture deployment (no model calls)", "provider":"openai", "model":"fixture-deployment-model", "apiKey":"fixture-not-a-real-key"}
        if initial_sdk != "0.1.25":
            model_input["pricing"] = fixture_token_pricing()
        model = web.data("POST", "/api/v1/model-configs", model_input)
        web.data("PUT", f"/api/v1/model-configs/{model['id']}/enable", {"enabled":True})
        memory = io.BytesIO()
        with zipfile.ZipFile(memory, "w") as archive:
            archive.writestr("src/a,b.ts", "// Synthetic deployment fixture; no real vulnerability.\n")
            archive.writestr("源码/测试.ts", "// Synthetic Unicode path fixture.\n")
        boundary = "axiom-acceptance-boundary"
        upload = (f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="fixture.zip"\r\nContent-Type: application/zip\r\n\r\n'.encode()+memory.getvalue()+f"\r\n--{boundary}--\r\n".encode())
        code, body, _ = web.raw("POST", "/api/v1/uploads", upload, "multipart/form-data; boundary="+boundary)
        assert code == 200, "fixture snapshot upload failed"
        snapshot = json.loads(body)["data"]
        task = web.data("POST", "/api/v1/scans", {"sourceSnapshotId":snapshot["id"], "modelConfigId":model["id"], "maxCostUsd":1.25, "maxTotalCostUsd":2.25})
        task_id = task["id"]

        # Use return-code polling: the fixture file is not present before SDK.run.
        wait_for(lambda: subprocess.run(stack.prefix+["exec","-T","agent","test","-f","/app/data/acceptance/sdk.json.processes"], env=stack.process_env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0, 30, "SDK fixture did not start")
        web.data("POST", "/api/v1/auth/logout", {})
        assert web.raw("GET", "/api/v1/auth/me")[0] == 401, "logout left the server token valid"
        web.login()
        assert web.data("GET", "/api/v1/auth/me")["role"] == "admin"
        checks.append("persistent administrator session; logout revokes token while Agent continues")
        stack.call("restart", "server", stdout=subprocess.DEVNULL)
        last = task_wait(web, task_id)
        assert last["status"] == "completed" and last["attempt"] == 1, "Server restart repeated or lost the scan"
        calls = stack.output("exec", "-T", "agent", "cat", "/app/data/acceptance/sdk.json.calls").splitlines()
        assert len(calls) == 1, "Server restart called SDK again"
        checks.append("real Server process restart retains Agent scan and one SDK invocation")

        verify_stack_runtime(stack, web, values["AXIOM_RELEASE"])
        checks.append("release build identity, Server/Agent matrix and Scanner contract preflight")

        result_path = f"/api/v1/scans/{task_id}/attempts/1/result"
        before = web.data("GET", result_path)
        result = before["result"]
        artifact = next(item for item in result["artifacts"] if str(item["relativePath"]).endswith("report.md"))
        artifact_path = f"/api/v1/scans/{task_id}/attempts/1/artifacts?path="+quote(artifact["relativePath"], safe="")
        artifact_bytes = web.raw("GET", artifact_path)[1]
        assert hashlib.sha256(artifact_bytes).hexdigest() == artifact["sha256"]
        model_before = web.data("GET", "/api/v1/model-configs")

        # A full container replacement must re-resolve DNS in the static Web proxy.
        stack.call("up", "-d", "--no-build", "--force-recreate", "--wait", "--wait-timeout", "90", "server", stdout=subprocess.DEVNULL)
        wait_for(lambda: web.raw("GET", "/readyz")[0] == 200, 30, "Web did not find the replaced Server")
        assert web.data("GET", result_path) == before
        checks.append("container replacement preserves results and Web proxy DNS")

        browser_env = dict(os.environ, AXIOM_TEST_WEB_URL=web.base,
                           AXIOM_TEST_ADMIN_USER=values.get("AXIOM_ADMIN_USER", "admin"),
                           AXIOM_TEST_ADMIN_PASSWORD=values["AXIOM_ADMIN_PASSWORD"],
                           AXIOM_TEST_DEPLOYMENT_TASK=str(task_id))
        subprocess.run(["node", str(ROOT/"code-audit-web/tests/deployment-web-acceptance.mjs")], env=browser_env, check=True)
        checks.append("synthetic console browser login, route refresh, archived result and JSON download on static Web")

        budget_path = f"/api/v1/scans/{task_id}/budget"
        first_budget = web.data("GET", budget_path)
        assert first_budget["estimatedMicros"] == 1125000 and first_budget["remainingMicros"] == 1125000
        assert first_budget["singleLimitUsd"] == 1.25 and first_budget["nextLimitMicros"] == 1125000
        web.data("POST", f"/api/v1/scans/{task_id}/retry", {})
        assert task_wait(web, task_id)["attempt"] == 2
        capture = json.loads(stack.output("exec", "-T", "agent", "cat", "/app/data/acceptance/sdk.json"))
        assert capture["fixtureOnly"]
        if initial_sdk == "0.1.25":
            assert capture["options"]["maxCostUsd"] == 1.125
        else:
            assert "maxCostUsd" not in capture["options"], "configured budget reached SDK built-in pricing"
            attempt = web.data("GET", f"/api/v1/scans/{task_id}")
            assignment = json.loads(stack.output("exec", "-T", "agent", "cat", attempt["outputPath"]+"/runtime/token-budget.json"))
            assert assignment["limitMicros"] == 1125000
        spent_budget = web.data("GET", budget_path)
        assert spent_budget["state"] == "exhausted" and spent_budget["estimatedMicros"] == 2250000
        assert not spent_budget["retryAllowed"] and web.raw("POST", f"/api/v1/scans/{task_id}/retry", {})[0] == 409
        assert web.data("GET", result_path) == before, "budget retry changed the original archived report"
        checks.append("task total budget, remaining SDK allowance, cumulative estimates and exhausted Web retry")

        stack.call("exec", "-T", "agent", "touch", "/app/data/acceptance/sdk.json.hold", stdout=subprocess.DEVNULL)
        busy = web.data("POST", "/api/v1/scans", {"sourceSnapshotId":snapshot["id"], "modelConfigId":model["id"], "maxTotalCostUsd":5})
        wait_for(lambda: len(stack.output("exec", "-T", "agent", "cat", "/app/data/acceptance/sdk.json.calls").splitlines()) == 3, 30, "busy fixture did not start")
        try:
            backup(stack, work/"busy-backup")
        except ValueError as error:
            assert "idle queue" in str(error)
            assert not (work/"busy-backup").exists()
        else:
            raise AssertionError("backup interrupted an active scan")
        web.base = Web(stack).base
        web.data("POST", f"/api/v1/scans/{busy['id']}/cancel", {})
        assert task_wait(web, busy["id"])["status"] == "canceled"
        assert len(stack.output("exec", "-T", "agent", "cat", "/app/data/acceptance/sdk.json.calls").splitlines()) == 3
        unknown_path = f"/api/v1/scans/{busy['id']}/budget"
        unknown_budget = web.data("GET", unknown_path)
        assert unknown_budget["state"] == "unknown" and unknown_budget["remainingMicros"] is None
        assert web.raw("POST", f"/api/v1/scans/{busy['id']}/retry", {})[0] == 409
        checks.append("unknown cancellation costs block capped retries without fabricating zero")
        checks.append("busy backup refuses without stopping Agent; Web cancellation remains available")

        expired = web.data("POST", "/api/v1/scans", {"sourceSnapshotId":snapshot["id"], "modelConfigId":model["id"]})
        wait_for(lambda: len(stack.output("exec", "-T", "agent", "cat", "/app/data/acceptance/sdk.json.calls").splitlines()) == 4, 30, "expiry fixture did not start")
        stack.call("exec", "-T", "postgres", "psql", "-U", values["POSTGRES_USER"], "-d", values["POSTGRES_DB"], "-c",
                   f"UPDATE scan_tasks SET lease_expires_at='2000-01-01T00:00:00Z' WHERE id={int(expired['id'])}", stdout=subprocess.DEVNULL)
        terminal = task_wait(web, expired["id"])
        assert terminal["status"] == "failed" and terminal["attempt"] == 1 and terminal["errorCode"] == "lease_expired"
        time.sleep(6) # Give the genuine heartbeat a chance to stop the old SDK fixture.
        assert len(stack.output("exec", "-T", "agent", "cat", "/app/data/acceptance/sdk.json.calls").splitlines()) == 4
        stack.call("exec", "-T", "agent", "rm", "/app/data/acceptance/sdk.json.hold", stdout=subprocess.DEVNULL)
        checks.append("production expiry requires Web retry and never automatically calls SDK again")

        # Readiness must fail when PostgreSQL is unavailable, then recover.
        stack.call("stop", "postgres", stdout=subprocess.DEVNULL)
        try:
            assert web.raw("GET", "/readyz")[0] == 503
        finally:
            stack.call("up", "-d", "--no-build", "--no-recreate", "--wait", "--wait-timeout", "90", "postgres", stdout=subprocess.DEVNULL)
        wait_for(lambda: web.raw("GET", "/readyz")[0] == 200, 30, "database readiness did not recover")
        checks.append("database outage and recovery drive actual readiness")

        storage = web.data("GET", "/api/v1/storage")
        assert storage["automaticHistoryDeletion"] is False and storage["cleanupSupported"]
        assert len(storage["roots"]) == 2 and all(root["availableBytes"] is not None for root in storage["roots"])
        policy = storage["policy"]
        web.data("PUT", "/api/v1/storage/policy", {**policy, "minFreeBytes":9007199254740991})
        blocked = web.data("POST", "/api/v1/scans", {"sourceSnapshotId":snapshot["id"], "modelConfigId":model["id"]})
        time.sleep(3)  # The genuine Agent polls, but the capacity gate returns no lease.
        held = web.data("GET", f"/api/v1/scans/{blocked['id']}")
        assert held["status"] == "pending" and held["attempt"] == 0
        assert len(stack.output("exec", "-T", "agent", "cat", "/app/data/acceptance/sdk.json.calls").splitlines()) == 4
        web.data("POST", f"/api/v1/scans/{blocked['id']}/cancel", {})
        policy = web.data("GET", "/api/v1/storage")["policy"]
        web.data("PUT", "/api/v1/storage/policy", {**policy, "minFreeBytes":0})
        storage_policy = web.data("GET", "/api/v1/storage")["policy"]
        checks.append("Web storage policy, actual volume probes and genuine Agent admission under low space")

        orphan = "/app/data/scans/fixture-storage-orphan"
        stack.call("exec", "-T", "server", "mkdir", "-p", orphan, stdout=subprocess.DEVNULL)
        stack.call("exec", "-T", "server", "touch", orphan+"/fixture-proof", stdout=subprocess.DEVNULL)
        stack.call("exec", "-T", "server", "touch", "-t", "202001010000", orphan, stdout=subprocess.DEVNULL)
        plan = web.data("POST", "/api/v1/storage/cleanup", {"kind":"orphans"})
        target = next(item for item in plan["items"] if item["relativePath"] == "fixture-storage-orphan")
        assert target["state"] == "planned" and any(item["state"] == "protected" for item in plan["items"])
        done = web.data("POST", f"/api/v1/storage/cleanup/{plan['id']}/execute", {"revision":plan["revision"]})
        assert done["state"] == "completed" and web.data("GET", result_path) == before
        assert web.raw("GET", artifact_path)[1] == artifact_bytes
        checks.append("explicit Web cleanup removes only orphan data and preserves historical SDK evidence")

        # Seed exactly the durable state left by a Server death after rename and
        # before its phase ACK. Native tests exercise the actual I/O failure.
        stranded = "/app/data/scans/fixture-storage-interrupted"
        stack.call("exec", "-T", "server", "mkdir", "-p", stranded, stdout=subprocess.DEVNULL)
        stack.call("exec", "-T", "server", "touch", stranded+"/fixture-proof", stdout=subprocess.DEVNULL)
        stack.call("exec", "-T", "server", "touch", "-t", "202001010000", stranded, stdout=subprocess.DEVNULL)
        interrupted = web.data("POST", "/api/v1/storage/cleanup", {"kind":"orphans"})
        index = next(i for i,item in enumerate(interrupted["items"]) if item["relativePath"] == "fixture-storage-interrupted")
        stack.call("exec", "-T", "server", "mkdir", "-p", "/app/data/scans/.axiom-trash", stdout=subprocess.DEVNULL)
        destination = f"/app/data/scans/.axiom-trash/{interrupted['id']}-{index}"
        stack.call("exec", "-T", "server", "mv", stranded, destination, stdout=subprocess.DEVNULL)
        interrupted["state"] = "running"
        interrupted["revision"] += 1
        document = json.dumps(interrupted, ensure_ascii=False).replace("'", "''")
        statement = f"UPDATE storage_cleanup_jobs SET state='running',document_json='{document}' WHERE id='{interrupted['id']}'"
        stack.call("exec", "-T", "postgres", "psql", "-U", values["POSTGRES_USER"], "-d", values["POSTGRES_DB"], "-v", "ON_ERROR_STOP=1", "-c", statement, stdout=subprocess.DEVNULL)
        checks.append("interrupted cleanup retains quarantine and a recoverable durable intent")

        directory = backup(stack, work/"backup")
        verify_backup(directory)
        baseline_images = stack.image_inventory()
        upgrade = {"kind":"same-version-replacement", "status":"passed"}
        upgraded_task = None
        if candidate_env:
            candidate = Stack(candidate_env, [extra])
            candidate_images = {}
            for service in APP_SERVICES:
                reference = f"{candidate_values.get('AXIOM_IMAGE_PREFIX', 'axiom')}/{service}:{candidate_values['AXIOM_RELEASE']}"
                info = json.loads(subprocess.check_output(["docker", "image", "inspect", reference], env=stack.process_env))[0]
                assert info["Os"] == "linux" and info["Architecture"] == "amd64", "Beta image must target Linux amd64"
                candidate_images[service] = {"reference":reference, "imageId":info["Id"]}
            assert candidate_images != baseline_images, "An upgrade must select a different image set"
            # Backup proved the queue idle. Keep Web closed until consumers and
            # producers have been replaced in the documented order.
            candidate.call("stop", "web", "agent", "server", stdout=subprocess.DEVNULL)
            for service in ("server", "agent", "web"):
                candidate.call("up", "-d", "--no-build", "--wait", "--wait-timeout", "120", service, stdout=subprocess.DEVNULL)
            web.base = Web(candidate).base
            assert web.data("GET", "/api/v1/auth/me")["role"] == "admin", "upgrade discarded a valid session"
            assert web.data("GET", result_path) == before and web.raw("GET", artifact_path)[1] == artifact_bytes
            assert web.data("GET", budget_path) == spent_budget and web.data("GET", unknown_path) == unknown_budget
            assert web.data("GET", "/api/v1/storage")["policy"] == storage_policy
            assert web.data("GET", f"/api/v1/storage/cleanup/{interrupted['id']}") == interrupted
            assert fixture_credentials(candidate) == credentials, "upgrade changed private SDK credential bytes or permissions"
            def candidate_ready():
                agents = web.data("GET", "/api/v1/executions/overview")["agents"]
                return any(a.get("runtime", {}).get("agentBuild") == candidate_values["AXIOM_RELEASE"] for a in agents)
            wait_for(candidate_ready, 30, "upgraded Agent did not publish the candidate build")
            verify_stack_runtime(candidate, web, candidate_values["AXIOM_RELEASE"])
            # Keep the old model and its historical unknown price untouched.
            # New finite-budget tasks require an explicitly priced connection.
            candidate_model = web.data("POST", "/api/v1/model-configs", {**model_input, "name":"Fixtures / Mock candidate configured pricing", "pricing":fixture_token_pricing()})
            web.data("PUT", f"/api/v1/model-configs/{candidate_model['id']}/enable", {"enabled":True})
            upgraded_task = web.data("POST", "/api/v1/scans", {"sourceSnapshotId":snapshot["id"], "modelConfigId":candidate_model["id"], "maxCostUsd":1.25, "maxTotalCostUsd":2.25})["id"]
            assert task_wait(web, upgraded_task)["status"] == "completed", "candidate could not run the isolated SDK fixture"
            upgrade = {"kind":"internal-image-upgrade", "status":"passed", "baselineImages":baseline_images,
                       "candidateImages":candidate_images, "historyAndCostEvidence":"preserved"}
            checks.append("internal baseline to candidate image upgrade preserves sessions, results, budgets, cleanup intent and accepts a new fixture task")
        # Corruption must be rejected before creating any restore resources.
        corrupt = work/"corrupt"
        shutil.copytree(directory, corrupt)
        with (corrupt/"database.dump").open("ab") as file:
            file.write(b"fixture-corruption")
        try:
            restore(corrupt, work/"rejected.env", restored_project+"-bad", 0, [extra])
        except ValueError:
            assert not (work/"rejected.env").exists()
        else:
            raise AssertionError("corrupt backup was accepted")
        restored = restore(directory, work/"restored.env", restored_project, 0, [extra], cleanup_on_failure=True)
        other = Web(restored)
        other.token = web.token
        assert other.raw("GET", "/api/v1/auth/me")[0] == 401, "restore reactivated old sessions"
        other.login()
        assert fixture_credentials(restored) == credentials, "rollback changed private SDK credential bytes or permissions"
        assert other.data("GET", result_path) == before, "restored result contract changed"
        assert other.raw("GET", artifact_path)[1] == artifact_bytes, "restored artifact changed"
        assert other.data("GET", "/api/v1/model-configs") == model_before, "model credential could not be restored"
        assert other.data("GET", budget_path) == spent_budget, "restore changed cumulative budget ledger"
        assert other.data("GET", unknown_path) == unknown_budget, "restore erased unknown cost evidence"
        if upgraded_task is not None:
            assert other.raw("GET", f"/api/v1/scans/{upgraded_task}")[0] == 404, "rollback retained post-backup task data"
            assert restored.image_inventory() == baseline_images, "rollback did not restore the original image identities"
            checks.append("pre-upgrade backup rollback uses original image IDs and excludes candidate-only task data")
        assert other.data("GET", "/api/v1/storage")["policy"] == storage_policy
        resumed_path = f"/api/v1/storage/cleanup/{interrupted['id']}"
        assert other.data("GET", resumed_path) == interrupted, "restore changed cleanup intent"
        # The restore must not automatically continue deleting data.
        restored.call("exec", "-T", "server", "test", "-d", destination, stdout=subprocess.DEVNULL)
        restored_browser = dict(browser_env, AXIOM_TEST_WEB_URL=other.base,
                                AXIOM_TEST_STORAGE_RECOVERY_JOB=interrupted["id"])
        subprocess.run(["node", str(ROOT/"code-audit-web/tests/deployment-web-acceptance.mjs")], env=restored_browser, check=True)
        assert other.data("GET", resumed_path)["state"] == "completed"
        assert other.data("GET", result_path) == before and other.raw("GET", artifact_path)[1] == artifact_bytes
        checks.append("fresh restore preserves policy and cleanup intent; only explicit Web continuation purges quarantine")
        checks.append("backup/restore preserves budget revisions, frozen allowances, sums and unknown costs")
        checks.append("consistent backup, checksum rejection, fresh restore of accounts/keys/results/artifacts with old sessions revoked")

        # Restoring over existing containers/volumes must be refused.
        try:
            restore(directory, work/"collision.env", restored_project, 0, [extra])
        except ValueError:
            assert other.data("GET", result_path) == before
        else:
            raise AssertionError("restore replaced existing data")
        checks.append("existing restore destination is preserved")
        report = {"status":"passed", "fixtureOnly":True, "modelCalls":0, "realSDKLogins":0, "externalPublications":0, "checks":checks,
                  "lifecycle":{"freshInstall":"passed", "upgrade":upgrade,
                               "rollback":{"status":"passed", "baselineImages":baseline_images},
                               "privateSDKCredentialBytesAndPermissions":"preserved-synthetic-sentinel",
                               "previousPublicRelease":None}}
        (work/"report.json").write_text(json.dumps(report, ensure_ascii=False, indent=2))
        print(json.dumps(report, ensure_ascii=False))
    except Exception:
        # Only synthetic acceptance credentials are present in this stack.
        stack.call("logs", "--no-color", "--tail", "30", *APP_SERVICES)
        raise
    finally:
        # Only disposable projects checked as unused at entry can reach cleanup.
        if restored is not None:
            restored.call("down", "--volumes", "--remove-orphans", stdout=subprocess.DEVNULL)
        stack.call("down", "--volumes", "--remove-orphans", stdout=subprocess.DEVNULL)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--env", required=True)
    parser.add_argument("--work", required=True)
    parser.add_argument("--candidate-env", help="optional different internal image baseline -> candidate upgrade; same private configuration")
    args = parser.parse_args()
    suite(args.env, args.work, args.candidate_env)
