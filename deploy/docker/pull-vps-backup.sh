#!/usr/bin/env bash
set -euo pipefail

# 旧开发环境的 dump 下载工具；Beta 正式备份使用 deploy/ops/stack.py。
# 必须显式设置有权访问的 SSH 主机和本机私钥。
# 用法：
#   ./pull-vps-backup.sh [remote_backups_dir] [remote_dump]
# 示例：
#   AXIOM_VPS_HOST=operator@example-host AXIOM_VPS_KEY=/absolute/private/id_ed25519 ./pull-vps-backup.sh /absolute/private/backups/example axiom.dump

VPS_HOST=${AXIOM_VPS_HOST:?Set AXIOM_VPS_HOST to an explicitly authorized SSH destination}
VPS_KEY=${AXIOM_VPS_KEY:?Set AXIOM_VPS_KEY to your private SSH key path}
if [ ! -f "$VPS_KEY" ] || [ ! -r "$VPS_KEY" ]; then
  printf '%s\n' 'AXIOM_VPS_KEY must name a readable private key file' >&2
  exit 2
fi
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
