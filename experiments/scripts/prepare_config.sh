#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
EXP_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
REPO_DIR="$(cd "${EXP_DIR}/.." && pwd)"

CONFIG="${1:-A}"
RUNTIME_DIR="${EXP_DIR}/runtime"
COMPOSE_DIR="${EXP_DIR}/compose"
FIXTURES_DIR="${EXP_DIR}/fixtures"

if [ "${CONFIG}" = "Z" ]; then
  SOURCE_CONFIG="${COMPOSE_DIR}/config.A.yaml"
else
  SOURCE_CONFIG="${COMPOSE_DIR}/config.${CONFIG}.yaml"
fi

if [ ! -f "${SOURCE_CONFIG}" ]; then
  echo "Error: configuration file not found: ${SOURCE_CONFIG}" >&2
  exit 1
fi

rm -rf "${RUNTIME_DIR}"
mkdir -p "${RUNTIME_DIR}"
cp "${SOURCE_CONFIG}" "${RUNTIME_DIR}/config.yaml"

# Prefer generated test fixtures; fall back to the repository dev secrets
# during Phase 1 bootstrap (config A does not use auth anyway).
if [ -f "${FIXTURES_DIR}/.env" ]; then
  cp "${FIXTURES_DIR}/.env" "${RUNTIME_DIR}/.env"
elif [ -f "${REPO_DIR}/configs/.env" ]; then
  cp "${REPO_DIR}/configs/.env" "${RUNTIME_DIR}/.env"
else
  : > "${RUNTIME_DIR}/.env"
fi
chmod 600 "${RUNTIME_DIR}/.env"

echo "Prepared runtime config '${CONFIG}' in ${RUNTIME_DIR}"
