#!/usr/bin/env python3
"""Static Web proxy acceptance on an internal, disposable Docker network.

No business containers, database, model credentials or real authentication.
"""
import argparse
import hashlib
import http.client
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import time
import urllib.error
import urllib.request
import uuid

ROOT = Path(__file__).resolve().parents[2]
NODE_IMAGE = "node:22-bookworm-slim@sha256:c3de60bf2f9dd0ac6370e6117950ff62d6e339527e7472301c9c78a017978392"
LABEL = "io.axiom.static-web-acceptance"


def docker(*args, **kwargs):
    return subprocess.run(["docker", *args], check=True, **kwargs)


def output(*args):
    return subprocess.check_output(["docker", *args], text=True).strip()


def inspect(name, kind="container"):
    return json.loads(output(kind, "inspect", name))[0]


def wait(check, message, seconds=30):
    end = time.monotonic() + seconds
    while time.monotonic() < end:
        try:
            if check():
                return
        except (OSError, ValueError, subprocess.CalledProcessError):
            pass
        time.sleep(.2)
    raise AssertionError(message)


def request(base, path, method="GET"):
    try:
        with urllib.request.urlopen(urllib.request.Request(base+path, method=method), timeout=5) as response:
            return response.status, response.headers, response.read()
    except urllib.error.HTTPError as error:
        return error.code, error.headers, error.read()


def compose_check(work):
    env_file = work/"fixture-compose.env"
    env_file.write_text("AXIOM_IMAGE_PREFIX=fixture\nAXIOM_RELEASE=static-acceptance\nAXIOM_SCAN_MOCK=1\n")
    env_file.chmod(0o600)
    command = ["docker", "compose", "--env-file", str(env_file), "-p", "axiom-static-config-fixture", "-f", str(ROOT/"deploy/docker/docker-compose.yml")]
    clean = {k:v for k,v in os.environ.items() if not k.startswith(("AXIOM_", "COMPOSE_", "POSTGRES_"))}
    def config(extra=()):
        return json.loads(subprocess.check_output(command+list(extra)+["config", "--format", "json"], env=clean))
    base = config()
    web = base["services"]["web"]
    assert web["build"]["dockerfile"] == "deploy/docker/Dockerfile.web.production"
    assert len(web["ports"]) == 1 and web["ports"][0]["target"] == 8080 and str(web["ports"][0]["published"]) == "5173"
    assert web["read_only"] and "ALL" in web["cap_drop"] and "no-new-privileges:true" in web["security_opt"]
    assert web["logging"]["options"] == {"max-size":"10m", "max-file":"3"}
    assert "readyz" in " ".join(web["healthcheck"]["test"]) and web.get("command") is None
    assert base["services"]["server"]["environment"]["AXIOM_SCAN_MOCK"] == "1"
    assert base["services"]["agent"]["environment"]["AXIOM_SCAN_MOCK"] == "1"
    dev = config(["-f", str(ROOT/"deploy/docker/compose.web-development.yml")])
    for service in ["server", "agent", "postgres"]:
        assert base["services"][service] == dev["services"][service], "Vite override changes backend: "+service
    assert len(dev["services"]["web"]["ports"]) == 1 and dev["services"]["web"]["ports"][0]["target"] == 5173
    assert dev["services"]["web"].get("read_only", False) is False and dev["services"]["web"]["healthcheck"]["disable"]
    assert dev["services"]["web"]["build"]["dockerfile"] == "deploy/docker/Dockerfile.web"


def accept(image, work, baseline=None):
    if work.exists():
        raise ValueError("Acceptance work directory must be new")
    work.mkdir(parents=True, mode=0o700)
    image_id = inspect(image, "image")["Id"]
    if baseline:
        inspect(baseline, "image")
    compose_check(work)
    token = uuid.uuid4().hex[:12]
    prefix = "axiom-static-accept-"+token
    network, edge, backend, web = prefix+"-net", prefix+"-edge", prefix+"-server", prefix+"-web"
    created = []
    checks = ["static-default-and-explicit-Vite-override-preserve-backends-mock-mode-and-host-port"]
    fixtures = work/"fixtures"
    (fixtures/"contracts").mkdir(parents=True)
    fixtures.chmod(0o755); (fixtures/"contracts").chmod(0o755)
    shutil.copyfile(ROOT/"deploy/ops/fixtures/static-web-backend.mjs", fixtures/"server.mjs")
    for name in ["storage-ready", "identity-snapshot", "runtime-ready"]:
        shutil.copyfile(ROOT/"contracts/fixtures/v1"/(name+".json"), fixtures/"contracts"/(name+".json"))
    for file in fixtures.rglob("*"):
        if file.is_file():
            file.chmod(0o644)
    def launch_backend(name, generation):
        created.append(("container", name))
        docker("run", "-d", "--name", name, "--label", LABEL+"="+token, "--platform", "linux/amd64", "--network", network,
               "--network-alias", "server", "--user", "1001:1001", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true",
               "--mount", "type=bind,source="+str(fixtures)+",target=/fixtures,readonly", "-e", "FIXTURE_GENERATION="+generation,
               NODE_IMAGE, "node", "/fixtures/server.mjs", stdout=subprocess.DEVNULL)
    def launch_web(name, reference, port=0, development=False):
        created.append(("container", name))
        # Only Web joins the host-facing bridge; the mock backend has no egress.
        args = ["create", "--name", name, "--label", LABEL+"="+token, "--platform", "linux/amd64", "--network", edge,
                "-p", "127.0.0.1:"+(str(port) if port else "")+":"+("5173" if development else "8080"),
                "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true", "--log-driver", "json-file", "--log-opt", "max-size=10m", "--log-opt", "max-file=3"]
        if development:
            args += ["-e", "AXIOM_API_TARGET=http://server:8080", reference, "npm", "run", "dev", "--", "--host", "0.0.0.0", "--port", "5173"]
        else:
            args += ["--read-only", "--tmpfs", "/tmp:uid=101,gid=101,mode=1777", "--health-cmd", "wget -q -O /dev/null http://127.0.0.1:8080/readyz",
                     "--health-interval", "2s", "--health-timeout", "2s", "--health-retries", "2", reference]
        docker(*args, stdout=subprocess.DEVNULL)
        docker("network", "connect", network, name, stdout=subprocess.DEVNULL)
        docker("start", name, stdout=subprocess.DEVNULL)
        return "http://"+output("port", name, "5173/tcp" if development else "8080/tcp").splitlines()[0]
    def control(value):
        script = "fetch('http://127.0.0.1:8080/__control?ready="+value+"').then(r=>{if(r.status!==200)process.exit(1)})"
        docker("exec", backend, "node", "-e", script, stdout=subprocess.DEVNULL)
    def state(base):
        status, _, body = request(base, "/api/fixture/state")
        return json.loads(body) if status == 200 else {}
    try:
        docker("network", "create", "--internal", "--label", LABEL+"="+token, network, stdout=subprocess.DEVNULL); created.append(("network", network))
        docker("network", "create", "--label", LABEL+"="+token, edge, stdout=subprocess.DEVNULL); created.append(("network", edge))
        launch_backend(backend, "A")
        base = launch_web(web, image)
        wait(lambda: request(base, "/readyz")[0] == 200, "Static Web/backend not ready")
        info = inspect(web)
        assert info["HostConfig"]["ReadonlyRootfs"] and info["Config"]["User"] == "101:101"
        assert info["HostConfig"]["LogConfig"]["Config"] == {"max-size":"10m", "max-file":"3"}
        for route in ["/", "/login", "/scans/1", "/settings?section=diagnostics"]:
            status, headers, body = request(base, route)
            assert status == 200 and b"/@vite/client" not in body and b"/src/main.ts" not in body
            assert headers.get("X-Content-Type-Options") == "nosniff" and headers.get("Referrer-Policy") == "same-origin"
            assert "no-store" in headers.get("Cache-Control", "")
        html = request(base, "/")[2].decode()
        asset = re.search(r'src="(/assets/[^"]+\.js)"', html).group(1)
        status, headers, _ = request(base, asset)
        assert status == 200 and "immutable" in ",".join(headers.get_all("Cache-Control", [])) and headers.get("X-Content-Type-Options") == "nosniff"
        for route in ["/assets/missing.js", "/@vite/client", "/src/main.ts", "/node_modules/vite/client.mjs", "/api/internal/claims"]:
            assert request(base, route)[0] == 404, route
        assert not any(r["path"].startswith("/api/internal/") for r in state(base)["requests"])
        assert request(base, "/api/v1/auth/me")[0] == 401
        checks.append("static-SPA-refresh-cache-headers-missing-assets-no-HMR-and-private-Agent-API-boundary")

        connection = http.client.HTTPConnection(base.removeprefix("http://"), timeout=6)
        connection.request("GET", "/api/fixture/events")
        started = time.monotonic(); response = connection.getresponse()
        assert response.status == 200 and response.readline().startswith(b"event: progress") and time.monotonic()-started < 2.5
        assert b"event: completed" in response.read(); connection.close()
        checks.append("SSE-first-event-arrives-before-completion-through-real-proxy")

        body = b"Fixtures / Mock upload\n" * 420000
        connection = http.client.HTTPConnection(base.removeprefix("http://"), timeout=8)
        connection.putrequest("POST", "/api/fixture/upload"); connection.putheader("Content-Type", "application/octet-stream"); connection.putheader("Content-Length", str(len(body))); connection.endheaders()
        connection.send(body[:65536]); wait(lambda: state(base).get("uploadStarted"), "Upload buffered before backend", seconds=4)
        connection.send(body[65536:]); response = connection.getresponse(); value = json.loads(response.read()); connection.close()
        assert response.status == 200 and value["bytes"] == len(body) and value["sha256"] == hashlib.sha256(body).hexdigest()
        too_large = http.client.HTTPConnection(base.removeprefix("http://"), timeout=5)
        too_large.putrequest("POST", "/api/fixture/upload"); too_large.putheader("Content-Length", str(2050*1024*1024)); too_large.endheaders()
        assert too_large.getresponse().status == 413; too_large.close()
        status, headers, body = request(base, "/api/fixture/report")
        assert status == 200 and 'filename="fixture-report.json"' in headers.get("Content-Disposition", "") and json.loads(body)["cost"] is None
        checks.append("streamed-large-upload-byte-hash-oversize-rejection-and-report-download-headers")

        control("false")
        assert request(base, "/healthz")[0] == 200 and request(base, "/readyz")[0] == 503
        wait(lambda: inspect(web)["State"]["Health"]["Status"] == "unhealthy", "Readiness fault not observed")
        control("true"); wait(lambda: inspect(web)["State"]["Health"]["Status"] == "healthy", "Readiness did not recover")
        assert request(base, "/api/fixture/disconnect")[0] == 502
        checks.append("backend-failure-is-not-ready-while-Web-is-alive-and-health-recovers")

        browser_env = {**os.environ, "AXIOM_TEST_STATIC_WEB_URL":base, "AXIOM_TEST_STATIC_WEB_REPORT_DIR":str(work/"browser")}
        with (work/"browser.log").open("wb") as log:
            subprocess.run(["node", str(ROOT/"code-audit-web/tests/static-web-acceptance.mjs")], env=browser_env, stdout=log, stderr=subprocess.STDOUT, check=True)
        assert json.loads((work/"browser/browser.json").read_text())["status"] == "passed"
        checks.append("real-browser-static-bundle-proxy-mock-identity-raw-downloads-dark-mobile-and-SSE")

        first_ip = next(iter(inspect(backend)["NetworkSettings"]["Networks"].values()))["IPAddress"]
        # Allocate the replacement while A still holds its address, then remove A's alias.
        replacement = prefix+"-replacement"; launch_backend(replacement, "B")
        docker("stop", backend, stdout=subprocess.DEVNULL); docker("network", "disconnect", network, backend, stdout=subprocess.DEVNULL)
        wait(lambda: state(base).get("generation") == "B", "Proxy DNS did not recover", seconds=25)
        assert next(iter(inspect(replacement)["NetworkSettings"]["Networks"].values()))["IPAddress"] != first_ip
        checks.append("backend-container-new-IP-is-discovered-by-static-proxy-DNS")

        if baseline:
            rollback = prefix+"-rollback"
            old_base = launch_web(rollback, baseline, development=True)
            wait(lambda: request(old_base, "/login")[0] == 200, "Rollback Web not ready")
            assert b"/@vite/client" in request(old_base, "/login")[2]
            assert state(old_base)["generation"] == "B"
            checks.append("previous-Vite-image-still-runs-against-unchanged-backend-with-no-data-restore")
        record = {"status":"passed", "checks":checks, "imageId":image_id, "fixtureOnly":True, "backendNetworkInternal":True, "webPublishedOn":"127.0.0.1",
                  "browserChecks":5, "rollbackVerified":bool(baseline), "businessDatabaseAccess":False, "realConsoleLogins":0, "realSDKLogins":0, "realAudits":0, "externalPublications":0}
        (work/"report.json").write_text(json.dumps(record, indent=2)+"\n")
        print(json.dumps(record), flush=True)
    finally:
        for kind, name in reversed(created):
            if subprocess.run(["docker", kind, "inspect", name], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode:
                continue
            obj = inspect(name, kind); labels = obj["Config"]["Labels"] if kind == "container" else obj["Labels"]
            assert labels.get(LABEL) == token
            docker(*(["rm", "-f", name] if kind == "container" else ["network", "rm", name]), stdout=subprocess.DEVNULL)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--image", required=True)
    parser.add_argument("--work", type=Path, required=True)
    parser.add_argument("--baseline-image")
    args = parser.parse_args()
    accept(args.image, args.work.resolve(), args.baseline_image)
