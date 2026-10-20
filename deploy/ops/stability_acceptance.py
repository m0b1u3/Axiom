#!/usr/bin/env python3
"""Bounded, model-free stability acceptance on new Linux/amd64 containers only."""
import argparse
import hashlib
import json
import math
import os
from pathlib import Path
import platform
import statistics
import subprocess
import time
import uuid
import zipfile

from acceptance import Web, wait_for
from stack import ROOT, Stack, env_bytes, private_write, read_env


def validate_profile(seconds, files, mib, parallel, burst):
    # Test bounds protect the test host; none are platform or SDK execution caps.
    if not 180 <= seconds <= 7200 or not 1000 <= files <= 100000:
        raise ValueError("Acceptance requires 180–7200 seconds and 1000–100000 synthetic files")
    if not 8 <= mib <= 512 or not 4 <= parallel <= 32 or not 500 <= burst <= 10000:
        raise ValueError("Acceptance profile outside its explicit resource envelope")


def fixture_archive(destination, files, source_bytes):
    """Deterministic, explicitly synthetic source with an independently hashed tree."""
    if files < 1 or source_bytes < files * 128:
        raise ValueError("Synthetic source needs at least 128 bytes per file")
    tree = hashlib.sha256()
    names = ["src/a,b.ts"] + [f"src/file-{i:06d}.ts" for i in range(1, files)]
    each, extra = divmod(source_bytes, files)
    with zipfile.ZipFile(destination, "w", compression=zipfile.ZIP_STORED) as archive:
        def store(name, content):
            entry = zipfile.ZipInfo(name, (2026, 1, 1, 0, 0, 0)); entry.external_attr = 0o100644 << 16
            archive.writestr(entry, content)
        for index, name in enumerate(sorted(names)):
            size = each + (index < extra)
            seed = ("// Fixtures / Mock only; no real vulnerability. " + hashlib.sha256(name.encode()).hexdigest() + "\n").encode()
            content = (seed * math.ceil(size / len(seed)))[:size]
            tree.update(name.encode() + b"\0" + hashlib.sha256(content).hexdigest().encode() + b"\n")
            store(name, content)
        inventory = {"files":files, "bytes":source_bytes, "sha256":tree.hexdigest()}
        store("fixture-manifest.json", json.dumps({"fixtureOnly":True, "inventory":inventory}).encode())
    return inventory


def inspect(container):
    return json.loads(subprocess.check_output(["docker", "inspect", container]))[0]


def unaffected_containers():
    names = subprocess.check_output(["docker", "ps", "--all", "--format", "{{.ID}}"], text=True).splitlines()
    return {name: {"image":v["Image"], "startedAt":v["State"]["StartedAt"]} for name in names for v in [inspect(name)]
            if not (v["Config"].get("Labels") or {}).get("com.docker.compose.project", "").startswith("axiom-accept-")}


def write_control(folder, release, burst):
    file = folder / "config.json"
    temp = folder / "config.tmp"
    temp.write_text(json.dumps({"fixtureOnly":True, "releaseAll":release, "sessionBurst":burst, "maximumSeconds":9000}))
    temp.chmod(0o644); temp.replace(file)


def upload(web, archive):
    boundary = "axiom-stability-fixture-boundary"
    body = (f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="{archive.name}"\r\nContent-Type: application/zip\r\n\r\n'.encode()
            + archive.read_bytes() + f"\r\n--{boundary}--\r\n".encode())
    old = web.timeout; web.timeout = 120
    try:
        code, response, _ = web.raw("POST", "/api/v1/uploads", body, "multipart/form-data; boundary=" + boundary)
    finally:
        web.timeout = old
    assert code == 200, "Synthetic source upload failed: HTTP " + str(code)
    return json.loads(response)["data"]


def suite(env_file, work, seconds=180, files=12000, mib=48, parallel=6, burst=1000):
    validate_profile(seconds, files, mib, parallel, burst)
    sdk_version = json.loads((ROOT/"code-audit-scanner/package.json").read_text())["dependencies"]["@openai/codex-security"]
    work = Path(work).resolve()
    if work.exists():
        raise ValueError("Stability work directory must be new")
    values = read_env(env_file)
    if not values["AXIOM_STACK_NAME"].startswith("axiom-accept-"):
        raise ValueError("Only a disposable axiom-accept-* input configuration is allowed")
    work.mkdir(parents=True, mode=0o700)
    protected = unaffected_containers()
    values.update(AXIOM_STACK_NAME="axiom-accept-stability-" + uuid.uuid4().hex[:12], AXIOM_WEB_PORT="0", AXIOM_BIND_ADDR="127.0.0.1", AXIOM_MAX_RECOVERIES="0", AXIOM_SCAN_MOCK="0")
    env_file = private_write(work/"runtime.env", env_bytes(values))
    control = work/"control"; control.mkdir(mode=0o755); write_control(control, False, burst)
    extra = work/"fixture.compose.yml"
    extra.write_text("""services:
  server:
    networks: [default, fixture-only]
  agent:
    networks: !override [fixture-only]
    environment:
      AXIOM_STABILITY_FIXTURE: '1'
      AXIOM_INPUT_TEST_CAPTURE: /app/data/stability/sdk.json
      AXIOM_RESULT_TEST_SCENARIO: rich
      NODE_OPTIONS: --import=/opt/axiom/deploy/ops/fixtures/stability-register.mjs
    volumes:
      - type: bind
        source: FIXTURES
        target: /opt/axiom/deploy/ops/fixtures
        read_only: true
      - type: bind
        source: CONTRACTS
        target: /opt/axiom/contracts/tests
        read_only: true
      - type: bind
        source: CONTROL
        target: /opt/axiom/stability-control
        read_only: true
networks:
  fixture-only:
    internal: true
""".replace("FIXTURES", json.dumps(str(ROOT/"deploy/ops/fixtures"))).replace("CONTRACTS", json.dumps(str(ROOT/"contracts/tests"))).replace("CONTROL", json.dumps(str(control))))
    stack = Stack(env_file, [extra]); stack.ensure_unused()
    checks, samples, latencies = [], [], []
    report = None
    def progress(step, **data):
        print(json.dumps({"fixtureOnly":True, "step":step, **data}), flush=True)
    def marker(task_id, attempt=1, name="started.json"):
        command = ["exec", "-T", "agent", "cat", f"/app/data/stability/{task_id}-{attempt}/{name}"]
        result = subprocess.run(stack.prefix + command, env=stack.process_env, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
        return json.loads(result.stdout) if result.returncode == 0 else None
    def terminal(task_id, timeout=90):
        return wait_for(lambda: (t if (t := web.data("GET", f"/api/v1/scans/{task_id}"))["status"] in ["completed", "partial", "failed", "canceled", "timeout"] else None), timeout, "Fixture task did not settle")
    def browser(phase, ids):
        output = work/("browser-"+phase)
        env = {**os.environ, "AXIOM_TEST_WEB_URL":web.base, "AXIOM_TEST_STABILITY_TOKEN":web.token, "AXIOM_TEST_STABILITY_PROJECT":stack.project,
               "AXIOM_TEST_STABILITY_IDS":json.dumps(ids), "AXIOM_TEST_STABILITY_FILES":str(files), "AXIOM_TEST_STABILITY_PHASE":phase, "AXIOM_TEST_STABILITY_REPORT":str(output), "AXIOM_TEST_STABILITY_SDK":sdk_version}
        with (work/("browser-"+phase+".log")).open("wb") as log:
            subprocess.run(["node", str(ROOT/"code-audit-web/tests/stability-web-acceptance.mjs")], env=env, stdout=log, stderr=subprocess.STDOUT, check=True)
        assert json.loads((output/"browser.json").read_text())["status"] == "passed"
    def sentinel():
        script = "const fs=require('fs'),c=require('crypto'),p='/app/data/.sdk-identities/stability-fixture/auth.json';console.log(JSON.stringify({sha256:c.createHash('sha256').update(fs.readFileSync(p)).digest('hex'),mode:fs.statSync(p).mode&511,directoryMode:fs.statSync(require('path').dirname(p)).mode&511}))"
        return json.loads(stack.output("exec", "-T", "agent", "node", "-e", script))
    def create(snapshot):
        return web.data("POST", "/api/v1/scans", {"sourceSnapshotId":snapshot["id"], "modelConfigId":model["id"], "projectId":project["id"], "maxCostUsd":2, "maxTotalCostUsd":5,
                    "timeoutMinutes":max(15,math.ceil((seconds+300)/60))})["id"]
    try:
        stack.call("up", "-d", "--no-build", "--wait", "--wait-timeout", "120", stdout=subprocess.DEVNULL)
        agent_id = stack.output("ps", "--quiet", "agent")
        server_id = stack.output("ps", "--quiet", "server")
        agent = inspect(agent_id)
        assert len(agent["NetworkSettings"]["Networks"]) == 1
        network = next(iter(agent["NetworkSettings"]["Networks"]))
        assert json.loads(subprocess.check_output(["docker", "network", "inspect", network]))[0]["Internal"]
        assert agent["Config"]["User"] == "axiom" and agent["HostConfig"]["ReadonlyRootfs"]
        assert stack.output("exec", "-T", "agent", "node", "-p", "require('/opt/axiom/code-audit-scanner/node_modules/@openai/codex-security/package.json').version") == sdk_version
        web = Web(stack); web.login()  # A newly generated synthetic console identity, never a business account.
        model = web.data("POST", "/api/v1/model-configs", {"name":"Fixtures / Mock stability", "provider":"openai", "model":"fixture-stability-model", "apiKey":"fixture-not-a-real-key"})
        web.data("PUT", f"/api/v1/model-configs/{model['id']}/enable", {"enabled":True})
        project = web.data("POST", "/api/v1/projects", {"name":"Fixtures / Mock stability"})
        large_path, small_path = work/"fixture-large.zip", work/"fixture-small.zip"
        inventory = fixture_archive(large_path, files, mib*1024*1024); fixture_archive(small_path, 2, 8192)
        large, small = upload(web, large_path), upload(web, small_path)
        progress("source-uploaded", files=files, sourceBytes=inventory["bytes"], archiveBytes=large_path.stat().st_size)
        script = "const fs=require('fs'),p='/app/data/.sdk-identities/stability-fixture';fs.mkdirSync(p,{recursive:true,mode:448});fs.writeFileSync(p+'/auth.json',JSON.stringify({fixtureOnly:true,credential:'synthetic-never-authenticate'}),{mode:384})"
        stack.call("exec", "-T", "agent", "node", "-e", script, stdout=subprocess.DEVNULL); credentials = sentinel()
        ids = [create(large)] + [create(small) for _ in range(parallel-1)]
        for task in ids:
            started = wait_for(lambda: marker(task), 120, "Fixture SDK did not start")
            assert started["fixtureOnly"] and not started["leakedCredentials"] and started["attempt"] == 1
        assert marker(ids[0])["inventory"] == inventory
        started_at = time.monotonic()
        overview = web.data("GET", "/api/v1/executions/overview")
        assert overview["dispatchMode"] == "unbounded" and overview["active"] == parallel and overview["effectiveCapacity"] is None
        progress("parallel-running", active=parallel)
        checks.append("large immutable archive hashes and simultaneous same-project same-model executions")
        browser("running", {"long":ids[0], "cancel":ids[-1], "parallel":parallel})
        assert terminal(ids[-1])["status"] == "canceled"
        canceled = web.data("GET", f"/api/v1/scans/{ids[-1]}/budget")
        assert canceled["state"] == "unknown" and canceled["remainingMicros"] is None and not canceled["retryAllowed"]
        checks.append("Web cancellation retains unknown cost and leaves other executions running")
        stack.call("restart", "server", stdout=subprocess.DEVNULL)
        wait_for(lambda: web.raw("GET", "/readyz")[0] == 200, 30, "Server did not recover")
        stack.call("stop", "postgres", stdout=subprocess.DEVNULL)
        try:
            assert web.raw("GET", "/readyz")[0] == 503
            time.sleep(3)
        finally:
            stack.call("up", "-d", "--no-build", "--no-recreate", "--wait", "--wait-timeout", "90", "postgres", stdout=subprocess.DEVNULL)
        wait_for(lambda: web.raw("GET", "/readyz")[0] == 200, 30, "Database did not recover")
        checks.append("real Server restart and short database outage do not duplicate held SDK calls")
        revisions = {task:0 for task in ids[:-1]}; next_sample = 0
        while time.monotonic()-started_at < seconds:
            before = time.monotonic(); current = [web.data("GET", f"/api/v1/scans/{task}") for task in ids[:-1]]; latencies.append(time.monotonic()-before)
            assert all(t["attempt"] == 1 and t["status"] in ["claimed", "scanning"] for t in current)
            for task, task_value in zip(ids[:-1], current):
                snapshot = task_value["progress"]
                assert snapshot["execution"] == {"taskId":str(task),"attempt":1}
                assert snapshot["revision"] >= revisions[task]; revisions[task] = snapshot["revision"]
            elapsed = time.monotonic()-started_at
            if elapsed >= next_sample:
                stats = subprocess.check_output(["docker", "stats", "--no-stream", "--format", "{{json .}}", server_id, agent_id], text=True)
                samples.append({"elapsedSeconds":round(elapsed,2), "stats":[json.loads(line) for line in stats.splitlines()]})
                progress("holding", elapsedSeconds=round(elapsed), active=len(current)); next_sample = elapsed+15
            time.sleep(2)
        hold_elapsed = time.monotonic()-started_at
        write_control(control, True, burst)
        archives = {}
        for task in ids[:-1]:
            last = terminal(task, 120); assert last["status"] == "completed" and last["attempt"] == 1
            archive = web.data("GET", f"/api/v1/scans/{task}/attempts/1/result")
            native = archive["result"]["extensions"]["sdk"]["stability"]
            assert native["taskId"] == str(task) and native["elapsedMs"] >= seconds*1000 and native["inventory"]["sha256"] == marker(task)["inventory"]["sha256"]
            assert web.data("GET", f"/api/v1/scans/{task}/budget")["estimatedMicros"] == 1125000
            after = 0; total = 0
            while True:
                page = web.data("GET", f"/api/v1/scans/{task}/attempts/1/sdk-sessions?after={after}")
                for event in page["items"]:
                    total += 1; assert event["sequence"] == total and event["execution"] == {"taskId":str(task), "attempt":1}
                    assert "fixture-stability-private-token" not in json.dumps(event)
                after = page["next"]
                if not page["hasMore"]:
                    assert page["summary"]["persistedCount"] == total and page["summary"]["omittedCount"] == 0; break
            assert total >= burst+73
            archives[task] = archive
        checks.append("multiple lease windows, monotonic progress, complete paginated native evidence and exact synthetic cost ledgers")
        progress("native-archives-verified", completed=len(archives), holdSeconds=round(hold_elapsed,2))
        browser("completed", {"long":ids[0]})
        old_pids = [pid for task in ids for pid in (marker(task)["scannerPid"],marker(task)["childPid"])]
        def no_descendants():
            script = "const ids=JSON.parse(process.argv[1]);console.log(JSON.stringify(ids.filter(pid=>{try{process.kill(pid,0);return true}catch{return false}})))"
            return not json.loads(stack.output("exec", "-T", "agent", "node", "-e", script, json.dumps(old_pids)))
        wait_for(no_descendants,15,"Completed or canceled Scanner left a child process alive")
        checks.append("completed and Web-canceled executions reap Scanner and descendant processes")
        # Crash after a recorded fee observation. The Server must wait for real lease expiry.
        write_control(control, False, burst); fault = create(small)
        wait_for(lambda: marker(fault), 45, "Crash fixture did not start")
        wait_for(lambda: web.data("GET", f"/api/v1/scans/{fault}/budget")["estimatedMicros"] >= 125000, 30, "Fee observation was not persisted before crash")
        subprocess.run(["docker", "update", "--restart=no", agent_id], check=True, stdout=subprocess.DEVNULL)
        stack.call("kill", "-s", "SIGKILL", "agent", stdout=subprocess.DEVNULL)
        progress("agent-killed-awaiting-Web-expiry", taskId=fault)
        crashed_at = time.monotonic()
        browser("interrupt", {"fault":fault})
        dead = terminal(fault, 15)
        lease_expiry_elapsed = time.monotonic()-crashed_at
        progress("Web-interruption-registered", afterKillSeconds=round(lease_expiry_elapsed,2))
        assert dead["status"] == "failed" and dead["errorCode"] == "lease_expired" and dead["attempt"] == 1
        unknown = web.data("GET", f"/api/v1/scans/{fault}/budget")
        assert unknown["state"] == "unknown" and unknown["remainingMicros"] is None and not unknown["retryAllowed"]
        partial_path = f"/api/v1/scans/{fault}/attempts/1/sdk-sessions"
        partial = web.data("GET", partial_path)
        assert partial["uncommitted"] and partial["summary"] is None and partial["capturedCount"] >= burst
        code, partial_bytes, partial_headers = web.raw("GET", partial_path+"?download=1")
        assert code == 200 and partial_headers.get("X-Axiom-Artifact-Status") == "uncommitted"
        assert partial_headers.get("X-Axiom-SHA256") == hashlib.sha256(partial_bytes).hexdigest()
        assert b"fixture-stability-private-token" not in partial_bytes
        stack.call("up", "-d", "--no-build", "--no-recreate", "--wait", "--wait-timeout", "90", "agent", stdout=subprocess.DEVNULL)
        subprocess.run(["docker", "update", "--restart=unless-stopped", agent_id], check=True, stdout=subprocess.DEVNULL)
        assert sentinel() == credentials and marker(fault, 2) is None
        write_control(control, True, burst)
        browser("recovery", {"fault":fault})
        recovered = terminal(fault, 120); assert recovered["status"] == "completed" and recovered["attempt"] == 2
        final_budget = web.data("GET", f"/api/v1/scans/{fault}/budget")
        assert final_budget["estimatedMicros"] == 1250000 and not final_budget["unknownAttempts"]
        assert web.raw("GET", partial_path+"?download=1")[1] == partial_bytes, "Retry changed the original interrupted evidence"
        assert sentinel() == credentials
        calls = [json.loads(line) for line in stack.output("exec", "-T", "agent", "cat", "/app/data/stability/invocations.jsonl").splitlines()]
        expected_calls = {(str(task),1) for task in ids} | {(str(fault),1),(str(fault),2)}
        assert len(calls) == len(expected_calls) and {(item["taskId"],item["attempt"]) for item in calls} == expected_calls, "SDK fixture was replayed or omitted"
        for task, archived in archives.items():
            assert web.data("GET", f"/api/v1/scans/{task}/attempts/1/result") == archived
        assert not inspect(agent_id)["State"]["OOMKilled"]
        checks.append("SIGKILL, natural lease expiry visible in open Web panel, explicit interruption, fee reconciliation and new attempt preserve old results and credentials")
        images = stack.image_inventory()
        for image in images.values():
            metadata = json.loads(subprocess.check_output(["docker","image","inspect",image["imageId"]]))[0]
            assert metadata["Os"] == "linux" and metadata["Architecture"] == "amd64"
            image.update(os=metadata["Os"], architecture=metadata["Architecture"])
        report = {"status":"passed", "fixtureOnly":True, "sdk":sdk_version, "checks":checks,
                  "profile":{"holdSeconds":seconds,"files":files,"sourceBytes":inventory["bytes"],"archiveBytes":large_path.stat().st_size,"parallel":parallel,"sessionBurstPerTask":burst,"defaultLeaseSeconds":60},
                  "observations":{"holdElapsedSeconds":round(hold_elapsed,2),"leaseExpiryAfterKillSeconds":round(lease_expiry_elapsed,2),"apiBatchP95Seconds":round(statistics.quantiles(latencies,n=20)[18] if len(latencies)>1 else max(latencies),3),"resourceSamples":samples},
                  "host":{"system":platform.system(),"machine":platform.machine(),"docker":json.loads(subprocess.check_output(["docker","info","--format","{{json .}}" ]))["Architecture"]},
                  "images":images,"project":stack.project,"realAudits":0,"realSDKLogins":0,"realPlanningModelCalls":0,"externalPublications":0,"businessDatabaseAccess":False}
    finally:
        # This exact fresh project passed ensure_unused before any mutation.
        stack.call("down", "--volumes", "--remove-orphans", stdout=subprocess.DEVNULL)
        after = unaffected_containers()
        assert all(after.get(key) == value for key,value in protected.items()), "An unrelated container changed during stability acceptance"
    assert report is not None
    report.update(protectedContainersUnchanged=True, temporaryProjectRemoved=True)
    (work/"report.json").write_text(json.dumps(report,indent=2)+"\n"); progress("passed", checks=len(checks))


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--env", required=True); parser.add_argument("--work", type=Path, required=True)
    parser.add_argument("--hold-seconds", type=int, default=180); parser.add_argument("--files", type=int, default=12000)
    parser.add_argument("--source-mib", type=int, default=48); parser.add_argument("--parallel", type=int, default=6); parser.add_argument("--session-burst", type=int, default=1000)
    args = parser.parse_args()
    suite(args.env,args.work,args.hold_seconds,args.files,args.source_mib,args.parallel,args.session_burst)
