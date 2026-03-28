#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WEB_DIR="$ROOT_DIR/web"

BACKEND_PORT="${BACKEND_PORT:-8081}"
FRONTEND_PORT="${FRONTEND_PORT:-3001}"
DATA_DIR="${DATA_DIR:-$ROOT_DIR/.local-data}"
SOURCE_DATA_DIR="${SOURCE_DATA_DIR:-$ROOT_DIR/data}"
LOG_DIR="${LOG_DIR:-$ROOT_DIR/.local-logs}"
INSTANCE_URL="${INSTANCE_URL:-http://localhost:${FRONTEND_PORT}}"
DEV_PROXY_SERVER="${DEV_PROXY_SERVER:-http://localhost:${BACKEND_PORT}}"
GOTOOLCHAIN_VALUE="${GOTOOLCHAIN:-auto}"
SKIP_INITIAL_IMPORT="${SKIP_INITIAL_IMPORT:-1}"

BACKEND_LOG="$LOG_DIR/backend.log"
FRONTEND_LOG="$LOG_DIR/frontend.log"
IMPORT_MARKER="$DATA_DIR/.initialized-from-source-data"

mkdir -p "$DATA_DIR" "$LOG_DIR"

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing required command: $1" >&2
    exit 1
  fi
}

pick_frontend_runner() {
  if command -v pnpm >/dev/null 2>&1; then
    echo "pnpm"
    return
  fi
  if command -v corepack >/dev/null 2>&1; then
    echo "corepack pnpm"
    return
  fi
  echo ""
}

FRONTEND_RUNNER="$(pick_frontend_runner)"

require_command go
require_command lsof
require_command curl
require_command rsync
if [[ -z "$FRONTEND_RUNNER" ]]; then
  echo "Missing required command: pnpm (or corepack with pnpm support)" >&2
  exit 1
fi

cleanup() {
  local exit_code=$?

  if [[ -n "${FRONTEND_PID:-}" ]] && kill -0 "$FRONTEND_PID" >/dev/null 2>&1; then
    kill "$FRONTEND_PID" >/dev/null 2>&1 || true
  fi
  if [[ -n "${BACKEND_PID:-}" ]] && kill -0 "$BACKEND_PID" >/dev/null 2>&1; then
    kill "$BACKEND_PID" >/dev/null 2>&1 || true
  fi

  wait "${FRONTEND_PID:-}" 2>/dev/null || true
  wait "${BACKEND_PID:-}" 2>/dev/null || true

  exit "$exit_code"
}

trap cleanup EXIT INT TERM

kill_port_processes() {
  local port="$1"
  local label="$2"
  local pids

  pids="$(lsof -tiTCP:"${port}" -sTCP:LISTEN 2>/dev/null | tr '\n' ' ' | xargs || true)"
  if [[ -z "${pids:-}" ]]; then
    return
  fi

  echo "${label} port ${port} is already in use. Stopping existing process(es): ${pids}"
  kill ${pids} >/dev/null 2>&1 || true
  sleep 1

  pids="$(lsof -tiTCP:"${port}" -sTCP:LISTEN 2>/dev/null | tr '\n' ' ' | xargs || true)"
  if [[ -n "${pids:-}" ]]; then
    echo "${label} port ${port} is still busy. Force killing: ${pids}"
    kill -9 ${pids} >/dev/null 2>&1 || true
    sleep 1
  fi
}

is_dir_empty() {
  local dir="$1"
  if [[ ! -d "$dir" ]]; then
    return 0
  fi
  if [[ -z "$(find "$dir" -mindepth 1 -maxdepth 1 -print -quit 2>/dev/null)" ]]; then
    return 0
  fi
  return 1
}

is_only_default_local_db() {
  local dir="$1"
  if [[ ! -d "$dir" ]]; then
    return 1
  fi

  local entry_count
  entry_count="$(find "$dir" -mindepth 1 -maxdepth 1 | wc -l | tr -d ' ')"
  if [[ "$entry_count" -ne 1 ]]; then
    return 1
  fi

  [[ -f "$dir/memos_prod.db" ]]
}

import_initial_data_if_needed() {
  if [[ "$SKIP_INITIAL_IMPORT" == "1" ]]; then
    echo "Skipping initial data import because SKIP_INITIAL_IMPORT=1"
    return
  fi

  if [[ -f "$IMPORT_MARKER" ]]; then
    return
  fi

  if [[ ! -d "$SOURCE_DATA_DIR" ]]; then
    echo "Source data directory not found, skipping initial import: $SOURCE_DATA_DIR"
    return
  fi

  if is_only_default_local_db "$DATA_DIR"; then
    echo "Detected a default local SQLite database. Replacing it with source data from $SOURCE_DATA_DIR"
  fi

  if ! is_dir_empty "$DATA_DIR"; then
    local backup_dir
    backup_dir="${DATA_DIR}.preimport-backup-$(date +%Y%m%d-%H%M%S)"
    echo "Backing up existing local data from $DATA_DIR to $backup_dir before initial import"
    mkdir -p "$backup_dir"
    rsync -a "$DATA_DIR"/ "$backup_dir"/
    find "$DATA_DIR" -mindepth 1 -maxdepth 1 ! -name "$(basename "$IMPORT_MARKER")" -exec rm -rf {} +
  fi

  echo "Importing initial local data from $SOURCE_DATA_DIR to $DATA_DIR"
  rsync -a "$SOURCE_DATA_DIR"/ "$DATA_DIR"/
  date +"%Y-%m-%d %H:%M:%S" >"$IMPORT_MARKER"
}

wait_for_url() {
  local url="$1"
  local label="$2"
  local attempts="${3:-90}"
  local delay="${4:-1}"

  for ((i = 1; i <= attempts; i++)); do
    if curl -fsS "$url" >/dev/null 2>&1; then
      echo "${label} is ready: $url"
      return 0
    fi
    sleep "$delay"
  done

  echo "${label} did not become ready in time: $url" >&2
  return 1
}

kill_port_processes "$BACKEND_PORT" "Backend"
kill_port_processes "$FRONTEND_PORT" "Frontend"
import_initial_data_if_needed

echo "Starting Memos backend on http://localhost:${BACKEND_PORT}"
(
  cd "$ROOT_DIR"
  MEMOS_DATA="$DATA_DIR" \
    MEMOS_INSTANCE_URL="$INSTANCE_URL" \
    GOTOOLCHAIN="$GOTOOLCHAIN_VALUE" \
    go run ./cmd/memos --port "$BACKEND_PORT"
) >"$BACKEND_LOG" 2>&1 &
BACKEND_PID=$!

echo "Starting Memos frontend on http://localhost:${FRONTEND_PORT}"
(
  cd "$WEB_DIR"
  DEV_PROXY_SERVER="$DEV_PROXY_SERVER" \
    ${FRONTEND_RUNNER} dev --host 0.0.0.0 --port "$FRONTEND_PORT"
) >"$FRONTEND_LOG" 2>&1 &
FRONTEND_PID=$!

wait_for_url "http://127.0.0.1:${BACKEND_PORT}" "Backend"
wait_for_url "http://127.0.0.1:${FRONTEND_PORT}" "Frontend"

echo
echo "Backend log:  $BACKEND_LOG"
echo "Frontend log: $FRONTEND_LOG"
echo "Data dir:     $DATA_DIR"
echo "App URL:      http://localhost:${FRONTEND_PORT}"
echo
echo "Press Ctrl+C to stop both processes."

wait "$BACKEND_PID" "$FRONTEND_PID"
