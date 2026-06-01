#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP_BIN="$SCRIPT_DIR/cli-proxy-api"
CONFIG_FILE="$SCRIPT_DIR/config.yaml"
LOG_DIR="$SCRIPT_DIR/logs"
PID_FILE="$LOG_DIR/cli-proxy-api.pid"
STOP_TIMEOUT_SECONDS="${STOP_TIMEOUT_SECONDS:-10}"

is_running() {
  local pid="$1"
  [[ "$pid" =~ ^[0-9]+$ ]] && kill -0 "$pid" 2>/dev/null
}

wait_for_exit() {
  local pid="$1"
  local waited=0

  while (( waited < STOP_TIMEOUT_SECONDS )); do
    if ! is_running "$pid"; then
      return 0
    fi

    sleep 1
    waited=$((waited + 1))
  done

  return 1
}

pids=()

if [[ -f "$PID_FILE" ]]; then
  pid_from_file="$(cat "$PID_FILE")"
  if is_running "$pid_from_file"; then
    pids+=("$pid_from_file")
  else
    rm -f "$PID_FILE"
  fi
fi

if [[ "${#pids[@]}" -eq 0 ]] && command -v pgrep >/dev/null 2>&1; then
  while IFS= read -r matched_pid; do
    if is_running "$matched_pid"; then
      pids+=("$matched_pid")
    fi
  done < <(pgrep -f "$APP_BIN --config $CONFIG_FILE" || true)
fi

if [[ "${#pids[@]}" -eq 0 ]]; then
  echo "No running cli-proxy-api process found."
  exit 0
fi

for pid in "${pids[@]}"; do
  if is_running "$pid"; then
    echo "Stopping cli-proxy-api gracefully. PID: $pid"
    kill "$pid" 2>/dev/null || true
  fi
done

remaining_pids=()

for pid in "${pids[@]}"; do
  if is_running "$pid" && ! wait_for_exit "$pid"; then
    remaining_pids+=("$pid")
  fi
done

for pid in "${remaining_pids[@]}"; do
  if is_running "$pid"; then
    echo "Force killing cli-proxy-api. PID: $pid"
    kill -KILL "$pid" 2>/dev/null || true
  fi
done

sleep 1

failed_pids=()
for pid in "${pids[@]}"; do
  if is_running "$pid"; then
    failed_pids+=("$pid")
  fi
done

if [[ "${#failed_pids[@]}" -gt 0 ]]; then
  echo "Failed to stop cli-proxy-api process: ${failed_pids[*]}" >&2
  exit 1
fi

rm -f "$PID_FILE"
echo "cli-proxy-api stopped."
