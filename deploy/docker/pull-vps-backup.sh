#!/usr/bin/env bash
set -euo pipefail

# 用法：
#   ./pull-vps-backup.sh [remote_backups_dir] [remote_dump]
# 示例：
#   ./pull-vps-backup.sh /opt/Axiom-backups/task-management-20260910-005733 axiom.dump

VPS_HOST=${AXIOM_VPS_HOST:-root@8.145.59.173}
VPS_KEY=${AXIOM_VPS_KEY:-/Users/noah/Documents/ToolsDev/dev.pem}
BACKUPS_DIR=$(cd "$(dirname "$0")" && pwd)/backups

REMOTE_DIR=${1:-/opt/Axiom-backups}
REMOTE_DUMP=${2:-}

mkdir -p "$BACKUPS_DIR"

if [ -n "$REMOTE_DUMP" ]; then
  scp -i "$VPS_KEY" "$VPS_HOST:$REMOTE_DIR/$REMOTE_DUMP" "$BACKUPS_DIR/axiom.dump"
  echo "saved: $BACKUPS_DIR/axiom.dump"
  exit 0
fi

if ! command -v scp >/dev/null 2>&1; then
  echo "scp unavailable" >&2
  exit 1
fi

echo "listing $VPS_HOST:$REMOTE_DIR"
ssh -i "$VPS_KEY" "$VPS_HOST" "ls -1 $REMOTE_DIR"

echo "please choose one file (例如 axiom.dump 或 backup.tar.gz) and rerun:"
echo "./pull-vps-backup.sh \"$REMOTE_DIR\" <filename>"  
