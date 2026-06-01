#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP_BIN="$SCRIPT_DIR/cli-proxy-api"
CONFIG_FILE="$SCRIPT_DIR/config.yaml"
LOG_DIR="$SCRIPT_DIR/logs"
PID_FILE="$LOG_DIR/cli-proxy-api.pid"
LOG_FILE="$LOG_DIR/cli-proxy-api.log"

mkdir -p "$LOG_DIR"

if [[ ! -x "$APP_BIN" ]]; then
  echo "Executable not found or not executable: $APP_BIN" >&2
  exit 1
fi

if [[ ! -f "$CONFIG_FILE" ]]; then
  echo "Config file not found: $CONFIG_FILE" >&2
  exit 1
fi

if [[ -f "$PID_FILE" ]]; then
  existing_pid="$(cat "$PID_FILE")"
  if [[ "$existing_pid" =~ ^[0-9]+$ ]] && kill -0 "$existing_pid" 2>/dev/null; then
    echo "cli-proxy-api is already running."
    echo "PID: $existing_pid"
    echo "Log: $LOG_FILE"
    exit 0
  fi

  rm -f "$PID_FILE"
fi

cd "$SCRIPT_DIR"
nohup "$APP_BIN" --config "$CONFIG_FILE" >>"$LOG_FILE" 2>&1 &
pid="$!"
echo "$pid" >"$PID_FILE"

sleep 1

if kill -0 "$pid" 2>/dev/null; then
  echo "cli-proxy-api started."
  echo "PID: $pid"
  echo "Config: $CONFIG_FILE"
  echo "Log: $LOG_FILE"
else
  echo "cli-proxy-api failed to start." >&2
  rm -f "$PID_FILE"
  if [[ -f "$LOG_FILE" ]]; then
    tail -n 50 "$LOG_FILE" >&2
  fi
  exit 1
fi
