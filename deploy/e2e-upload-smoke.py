"""冒烟：打包 fixtures → 上传 → 快照 → 建扫描。用法: python deploy/e2e-upload-smoke.py <base_url> <token>"""
import json
import os
import sys
import zipfile

BASE = sys.argv[1].rstrip("/")
TOKEN = sys.argv[2]
FIXTURES = os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "test-fixtures")
OUT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "code-audit-agent", "data", "e2e-fixtures.zip")

with zipfile.ZipFile(OUT, "w", zipfile.ZIP_DEFLATED) as z:
    for root, _dirs, files in os.walk(FIXTURES):
        for fn in files:
            fp = os.path.join(root, fn)
            arc = os.path.relpath(fp, FIXTURES).replace("\\", "/")
            z.write(fp, arc)
print("zip size", os.path.getsize(OUT))


def req(method, path, payload=None, files=None):
    import urllib.request
    if files:
        import mimetypes
        import uuid
        boundary = uuid.uuid4().hex
        body = b""
        for field, (filename, fp) in files.items():
            with open(fp, "rb") as fh:
                data = fh.read()
            body += (
                f"--{boundary}\r\nContent-Disposition: form-data; name=\"{field}\"; "
                f"filename=\"{os.path.basename(filename)}\"\r\nContent-Type: {mimetypes.guess_type(filename)[0] or 'application/octet-stream'}\r\n\r\n"
            ).encode() + data + b"\r\n"
        body += f"--{boundary}--\r\n".encode()
        r = urllib.request.Request(f"{BASE}{path}", data=body, method=method)
        r.add_header("Content-Type", f"multipart/form-data; boundary={boundary}")
    else:
        r = urllib.request.Request(f"{BASE}{path}", method=method)
        if payload is not None:
            r.add_header("Content-Type", "application/json")
            r.data = json.dumps(payload).encode()
    r.add_header("Authorization", f"Bearer {TOKEN}")
    with urllib.request.urlopen(r) as resp:
        return json.loads(resp.read().decode())


data = req("POST", "/api/v1/uploads", files={"file": (OUT, OUT)})["data"]
print("snapshot", {k: data[k] for k in ("id", "originalFilename", "fileCount", "totalSize")})
print("archiveSha256", data["archiveSha256"][:16], "...")
assert data["fileCount"] >= 5 and len(data["archiveSha256"]) == 64

scan = req("POST", "/api/v1/scans", payload={"sourcePath": data["snapshotPath"], "provider": "openai", "model": "gpt-5.6-sol"})["data"]
print("scan", scan["id"], scan["status"])
assert scan["status"] == "pending"

snaps = req("GET", "/api/v1/snapshots")["data"]
print("snapshots count", len(snaps))
print("E2E OK")
