#!/usr/bin/env bash
# 开发机一键启动三件套：server + agent + web。Ctrl-C 一起退出。
# AXIOM_SCAN_MOCK=0 可关掉 mock（真实扫描需要对应 API Key）。
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
MOCK="${AXIOM_SCAN_MOCK:-1}"

DB="$ROOT/code-audit-agent/data/agent.db"
DATA="$ROOT/code-audit-agent/data"
SNAPS="$DATA/snapshots"
LOGDIR="$DATA/logs"
mkdir -p "$LOGDIR"

SERVER_ADDR="${AXIOM_HTTP_ADDR:-127.0.0.1:8080}"
WEB_PORT="${AXIOM_WEB_PORT:-5173}"

pids=()
cleanup() {
  echo "[dev-up] stopping..."
  for pid in "${pids[@]:-}"; do kill "$pid" 2>/dev/null || true; done
  exit 0
}
trap cleanup INT TERM EXIT

echo "[dev-up] server http://$SERVER_ADDR  (db=$DB)"
(cd "$ROOT/code-audit-server" && \
  AXIOM_HTTP_ADDR="$SERVER_ADDR" AXIOM_ADMIN_PASSWORD="${AXIOM_ADMIN_PASSWORD:-admin}" \
  AXIOM_AGENT_DB="$DB" AXIOM_SNAPSHOT_DIR="$SNAPS" \
  go run ./cmd/server >> "$LOGDIR/server.log" 2>&1) &
pids+=($!)

echo "[dev-up] agent (mock=$MOCK)"
(cd "$ROOT/code-audit-agent" && \
  AXIOM_SCAN_MOCK="$MOCK" AXIOM_DATA_DIR="$DATA" \
  AXIOM_SCANNER_DIR="$ROOT/code-audit-scanner" \
  go run ./cmd/agent >> "$LOGDIR/agent.log" 2>&1) &
pids+=($!)

echo "[dev-up] web http://127.0.0.1:$WEB_PORT  (proxy /api -> $SERVER_ADDR)"
(cd "$ROOT/code-audit-web" && npm run dev -- --port "$WEB_PORT" >> "$LOGDIR/web.log" 2>&1) &
pids+=($!)

echo "[dev-up] logs: $LOGDIR ; Ctrl-C 退出"
wait
