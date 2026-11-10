#!/usr/bin/env python3
import argparse
import csv
import http.client
import json
import socket
import time
from datetime import datetime, timezone

SOCKET_PATH = "/var/run/docker.sock"
PREFIXES = ("test-api-gateway", "test-mock-server", "test-k6", "compose-k6")


class UnixHTTPConnection(http.client.HTTPConnection):
    def __init__(self):
        super().__init__("localhost", timeout=5)

    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.settimeout(5)
        self.sock.connect(SOCKET_PATH)


def api_get(path):
    connection = UnixHTTPConnection()
    connection.request("GET", path)
    response = connection.getresponse()
    body = response.read()
    connection.close()
    if response.status >= 400:
        raise RuntimeError(f"docker API {path}: {response.status} {body[:200]!r}")
    return json.loads(body)


def running_containers():
    result = []
    for item in api_get("/containers/json"):
        name = item.get("Names", [""])[0].lstrip("/")
        if name.startswith(PREFIXES):
            result.append((item["Id"], name))
    return result


def cpu_percent(stats, previous):
    current = stats.get("cpu_stats", {})
    current_total = current.get("cpu_usage", {}).get("total_usage", 0)
    current_system = current.get("system_cpu_usage", 0)
    online = current.get("online_cpus") or len(current.get("cpu_usage", {}).get("percpu_usage") or [])
    if online <= 0:
        online = 1
    if previous is None:
        return 0.0, (current_total, current_system)
    previous_total, previous_system = previous
    if not current_total or not previous_total or not current_system or current_system <= previous_system:
        return 0.0, (current_total, current_system)
    value = (current_total - previous_total) / (current_system - previous_system) * online * 100.0
    return value, (current_total, current_system)


def memory_mib(stats):
    memory = stats.get("memory_stats", {})
    usage = memory.get("usage", 0)
    cache = memory.get("stats", {}).get("cache", 0)
    return max(0, usage - cache) / 1048576.0


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("output")
    parser.add_argument("--interval", type=float, default=1.0)
    args = parser.parse_args()

    with open(args.output, "w", newline="") as handle:
        writer = csv.writer(handle)
        writer.writerow(["timestamp", "container", "cpu_percent", "mem_usage_mib"])
        handle.flush()
        previous = {}
        while True:
            started = time.monotonic()
            timestamp = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
            try:
                containers = running_containers()
                active = {container_id for container_id, _ in containers}
                previous = {key: value for key, value in previous.items() if key in active}
                for container_id, name in containers:
                    try:
                        stats = api_get(f"/containers/{container_id}/stats?stream=false&one-shot=true")
                    except Exception:
                        continue
                    value, current = cpu_percent(stats, previous.get(container_id))
                    previous[container_id] = current
                    writer.writerow([
                        timestamp,
                        name,
                        f"{value:.2f}",
                        f"{memory_mib(stats):.2f}",
                    ])
                handle.flush()
            except Exception:
                pass
            elapsed = time.monotonic() - started
            time.sleep(max(0.0, args.interval - elapsed))


if __name__ == "__main__":
    main()
