#!/bin/sh

set -eu

: "${OPENUSAGE_WEB_TOKEN:?OPENUSAGE_WEB_TOKEN must be set}"

state_dir="${XDG_STATE_HOME:-/data/state}/openusage"
socket_path="${OPENUSAGE_TELEMETRY_SOCKET:-${state_dir}/telemetry.sock}"
mkdir -p "$state_dir"

openusage telemetry daemon --verbose &
daemon_pid=$!
web_pid=""

shutdown() {
  if [ -n "$web_pid" ]; then
    kill "$web_pid" 2>/dev/null || true
    wait "$web_pid" 2>/dev/null || true
  fi
  kill "$daemon_pid" 2>/dev/null || true
  wait "$daemon_pid" 2>/dev/null || true
}
trap shutdown INT TERM EXIT

i=0
while [ ! -e "$socket_path" ]; do
  if ! kill -0 "$daemon_pid" 2>/dev/null; then
    wait "$daemon_pid" || true
    exit 1
  fi
  i=$((i + 1))
  if [ "$i" -ge 100 ]; then
    printf '%s\n' "OpenUsage telemetry daemon did not create $socket_path" >&2
    exit 1
  fi
  sleep 0.1
done

openusage web \
  --listen "${OPENUSAGE_WEB_LISTEN:-0.0.0.0:8787}" \
  --allow-public \
  --no-open &
web_pid=$!

set +e
wait "$web_pid"
status=$?
set -e
exit "$status"
