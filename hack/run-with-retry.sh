#!/usr/bin/env bash
# Runs a script up to MAX_TRIES times; succeeds on the first passing attempt.
# Usage: run-with-retry.sh <script> [args...]
# Env:   MAX_TRIES (default: 3)
set -euo pipefail

MAX_TRIES="${MAX_TRIES:-3}"
SCRIPT="$1"
shift

attempt=0
while true; do
  attempt=$((attempt + 1))
  echo "--- Attempt ${attempt}/${MAX_TRIES}: $SCRIPT $* ---"
  if bash "$SCRIPT" "$@"; then
    exit 0
  fi
  if [ "$attempt" -ge "$MAX_TRIES" ]; then
    echo "FAIL: $SCRIPT failed after ${MAX_TRIES} attempts."
    exit 1
  fi
  echo "Retrying ($((MAX_TRIES - attempt)) attempt(s) left) ..."
done
