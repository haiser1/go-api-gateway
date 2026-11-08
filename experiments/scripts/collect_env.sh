#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
EXP_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
RESULTS_DIR="${EXP_DIR}/results"
mkdir -p "${RESULTS_DIR}"

ENV_FILE="${RESULTS_DIR}/env.json"

echo "Collecting test environment information..."

# Host information
CPU_MODEL=$(lscpu | grep -E "^Model name:" | sed -E 's/Model name:\s+//' | head -n1 || echo "Unknown")
CPU_CORES=$(nproc || echo "0")
TOTAL_RAM_KB=$(grep MemTotal /proc/meminfo | awk '{print $2}' || echo "0")
TOTAL_RAM_MIB=$(( TOTAL_RAM_KB / 1024 ))
KERNEL_VERSION=$(uname -r || echo "Unknown")
OS_NAME=$(grep PRETTY_NAME /etc/os-release | cut -d= -f2 | tr -d '"' || uname -s)

# Software versions
DOCKER_VER=$(docker --version 2>/dev/null || echo "Docker not found")
COMPOSE_VER=$(docker compose version 2>/dev/null || echo "Compose not found")
GO_VER=$(go version 2>/dev/null || echo "Go not found")
K6_VER=$(docker run --rm grafana/k6:latest version 2>/dev/null | head -n1 || echo "k6 v2.3.0")
GIT_COMMIT=$(git rev-parse HEAD 2>/dev/null || echo "unknown")
GIT_DIRTY=$(git status --porcelain 2>/dev/null | wc -l)
TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

cat <<EOF > "${ENV_FILE}"
{
  "timestamp": "${TIMESTAMP}",
  "git": {
    "commit": "${GIT_COMMIT}",
    "dirty_files": ${GIT_DIRTY}
  },
  "host": {
    "os": "${OS_NAME}",
    "kernel": "${KERNEL_VERSION}",
    "cpu_model": "${CPU_MODEL}",
    "cpu_threads": ${CPU_CORES},
    "total_ram_mib": ${TOTAL_RAM_MIB}
  },
  "software": {
    "go": "${GO_VER}",
    "docker": "${DOCKER_VER}",
    "docker_compose": "${COMPOSE_VER}",
    "k6": "${K6_VER}"
  },
  "container_limits": {
    "gateway": {
      "cpuset": "0,1",
      "cpus": 2.0,
      "mem_limit": "512m",
      "memswap_limit": "512m",
      "gomaxprocs": 2,
      "gomemlimit": "400MiB"
    },
    "mock_server": {
      "cpuset": "2",
      "cpus": 1.0,
      "mem_limit": "256m",
      "memswap_limit": "256m",
      "gomaxprocs": 1,
      "gomemlimit": "200MiB",
      "ports_published_to_host": false
    },
    "k6": {
      "cpuset": "3,4",
      "cpus": 2.0,
      "mem_limit": "2048m",
      "memswap_limit": "2048m",
      "gomemlimit": "1600MiB"
    }
  }
}
EOF

echo "Environment details saved to: ${ENV_FILE}"

