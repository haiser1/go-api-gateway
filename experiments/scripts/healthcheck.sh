#!/usr/bin/env bash
set -euo pipefail

TARGET_URL="${1:-http://localhost:8080/mock}"
MAX_RETRIES="${2:-60}"
RETRY_INTERVAL="${3:-0.5}"

echo "Waiting for Gateway to become healthy at ${TARGET_URL}..."

count=0
while [ "${count}" -lt "${MAX_RETRIES}" ]; do
  # Check if endpoint responds with 200 or any valid HTTP response (< 500)
  HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" --connect-timeout 2 "${TARGET_URL}" || echo "000")
  if [ "${HTTP_CODE}" = "200" ] || [ "${HTTP_CODE}" = "401" ] || [ "${HTTP_CODE}" = "404" ]; then
    echo "Gateway is ready! Received HTTP status ${HTTP_CODE} from ${TARGET_URL}"
    exit 0
  fi
  sleep "${RETRY_INTERVAL}"
  count=$(( count + 1 ))
done

echo "Error: Gateway failed to become ready after $(( MAX_RETRIES / 2 )) seconds (last status: ${HTTP_CODE})"
exit 1

