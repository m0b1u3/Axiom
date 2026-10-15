#!/usr/bin/env bash
set -euo pipefail

restore_one() {
  local db_name="$1"
  local dump_file="$2"

  if [ -z "${dump_file}" ]; then
    return 0
  fi

  if [ ! -f "$dump_file" ]; then
    echo "[axiom-restore] $db_name dump not found: $dump_file, skip"
    return 0
  fi

  psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$db_name" -Atc "SELECT 1 FROM schema_migrations LIMIT 1" >/dev/null 2>&1
  if [ $? -eq 0 ]; then
    echo "[axiom-restore] $db_name already initialized, skip"
    return 0
  fi

  echo "[axiom-restore] restoring $db_name from $dump_file"
  case "${dump_file##*.}" in
    sql)
      psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$db_name" < "$dump_file"
      ;;
    *)
      pg_restore --clean --if-exists --no-owner --no-privileges -d "$db_name" "$dump_file"
      ;;
  esac
}

if [ -n "${AXIOM_DUMP_DIR:-}" ] && [ -n "${AXIOM_DUMP_FILE:-}" ]; then
  restore_one "${POSTGRES_DB}" "${AXIOM_DUMP_DIR%/}/${AXIOM_DUMP_FILE}"
fi

if [ -n "${AXIOM_DUMP_DIR:-}" ] && [ -n "${AXIOM_STAGE_DUMP_FILE:-}" ]; then
  export TARGET_DB="${AXIOM_STAGE_DB:-axiom_stage}"
  if psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Atc "SELECT 1 FROM pg_database WHERE datname='${TARGET_DB}'" | grep -q 1; then
    restore_one "${TARGET_DB}" "${AXIOM_DUMP_DIR%/}/${AXIOM_STAGE_DUMP_FILE}"
  else
    psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c "CREATE DATABASE \"${TARGET_DB}\""
    restore_one "${TARGET_DB}" "${AXIOM_DUMP_DIR%/}/${AXIOM_STAGE_DUMP_FILE}"
  fi
fi

if [ -n "${AXIOM_DUMP_DIR:-}" ] && [ -n "${AXIOM_TEST_DUMP_FILE:-}" ]; then
  export TARGET_DB="${AXIOM_TEST_DB:-axiom_test}"
  if psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Atc "SELECT 1 FROM pg_database WHERE datname='${TARGET_DB}'" | grep -q 1; then
    restore_one "${TARGET_DB}" "${AXIOM_DUMP_DIR%/}/${AXIOM_TEST_DUMP_FILE}"
  else
    psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c "CREATE DATABASE \"${TARGET_DB}\""
    restore_one "${TARGET_DB}" "${AXIOM_DUMP_DIR%/}/${AXIOM_TEST_DUMP_FILE}"
  fi
fi
