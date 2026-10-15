#!/usr/bin/env bash
# 全链路演示：独立 demo 库，跑一次 mock 扫描，展示结果与报告。
# 先到先服务；结束后清理 server，保留 demo 数据供检查。
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEMO="$ROOT/code-audit-agent/data/demo"
DB="$DEMO/agent.db"
SNAPS="$DEMO/snapshots"
mkdir -p "$DEMO" "$SNAPS"
rm -f "$DB"

PORT="${AXIOM_DEMO_PORT:-8097}"
BASE="http://127.0.0.1:$PORT"

echo "[demo] server on $BASE (db=$DB)"
(cd "$ROOT/code-audit-server" && \
  AXIOM_HTTP_ADDR="127.0.0.1:$PORT" AXIOM_ADMIN_PASSWORD=admin \
  AXIOM_AGENT_DB="$DB" AXIOM_SNAPSHOT_DIR="$SNAPS" \
  go run ./cmd/server > "$DEMO/server.log" 2>&1) &
SERVER_PID=$!
trap 'kill $SERVER_PID 2>/dev/null || true' EXIT

TOKEN=""
for _ in $(seq 1 60); do
  TOKEN=$(curl -s -X POST "$BASE/api/v1/auth/login" -H 'Content-Type: application/json' \
    -d '{"username":"admin","password":"admin"}' | python -c "import sys,json;print(json.load(sys.stdin)['data']['token'])" 2>/dev/null) || TOKEN=""
  [ -n "$TOKEN" ] && break
  sleep 1
done
[ -n "$TOKEN" ] || { echo "[demo] server did not start, see $DEMO/server.log"; exit 1; }
echo "[demo] login ok"

echo "[demo] uploading fixtures.zip -> snapshot"
export TOKEN BASE DEMO_DIR="$DEMO"
SNAP=$(python - <<'PY'
import json, os, uuid, zipfile, urllib.request

base = os.environ['BASE']; token = os.environ['TOKEN']
repo = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
fixtures = os.path.join(repo, 'test-fixtures')
out = os.path.join(os.environ['DEMO_DIR'], 'fixtures.zip')
with zipfile.ZipFile(out, 'w', zipfile.ZIP_DEFLATED) as z:
    for r, _d, fs in os.walk(fixtures):
        for fn in fs:
            fp = os.path.join(r, fn)
            z.write(fp, os.path.relpath(fp, fixtures).replace('\\', '/'))
boundary = uuid.uuid4().hex
with open(out, 'rb') as fh:
    data = fh.read()
body = (f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="fixtures.zip"\r\n'
        f'Content-Type: application/zip\r\n\r\n').encode() + data + b'\r\n' + f'--{boundary}--\r\n'.encode()
req = urllib.request.Request(base + '/api/v1/uploads', data=body, method='POST')
req.add_header('Content-Type', f'multipart/form-data; boundary={boundary}')
req.add_header('Authorization', 'Bearer ' + token)
snap = json.loads(urllib.request.urlopen(req, timeout=120).read().decode())['data']
print(snap['id'])
PY
)
echo "[demo] snapshot id=$SNAP"

echo "[demo] create scan"
SCAN=$(curl -s -X POST "$BASE/api/v1/scans" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d "{\"sourceSnapshotId\":$SNAP,\"provider\":\"openai\",\"model\":\"gpt-5.6-sol\"}" \
  | python -c "import sys,json;print(json.load(sys.stdin)['data']['id'])")
echo "[demo] scan id=$SCAN"

echo "[demo] agent consumes (mock scan, ~10s)"
(cd "$ROOT/code-audit-agent" && \
  AXIOM_SCAN_MOCK=1 AXIOM_DATA_DIR="$DEMO" \
  AXIOM_SCANNER_DIR="$ROOT/code-audit-scanner" \
  go run ./cmd/agent -once >> "$DEMO/agent.log" 2>&1)

echo "[demo] waiting for result"
for _ in $(seq 1 60); do
  RES=$(curl -s "$BASE/api/v1/scans/$SCAN" -H "Authorization: Bearer $TOKEN")
  ST=$(echo "$RES" | python -c "import sys,json;print(json.load(sys.stdin)['data']['status'])" 2>/dev/null || echo pending)
  case "$ST" in
    completed|partial|failed|canceled|timeout) break ;;
  esac
  sleep 2
done
echo "$RES" | python -c "
import sys,json
d=json.load(sys.stdin)['data']
print('[demo] status=', d['status'], ' findings=', d['findingCount'])
print('[demo] html report: $BASE/api/v1/scans/$SCAN/report')
print('[demo] pdf report:  $BASE/api/v1/scans/$SCAN/report.pdf')
"
echo "[demo] demo data kept in: $DEMO"
