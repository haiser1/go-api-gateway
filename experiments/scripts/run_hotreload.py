#!/usr/bin/env python3
import csv
import http.client
import json
import os
import statistics
import subprocess
import threading
import time
from datetime import datetime, timezone
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
EXP = REPO / "experiments"
COMPOSE = EXP / "compose" / "docker-compose.test.yml"
RUNTIME = EXP / "runtime"
FIXTURES = EXP / "fixtures"
RESULTS = EXP / "results"
RAW = RESULTS / "raw" / "hotreload"
ADMIN_TOKEN = "test-admin-token"
ADMIN_HEADERS = {"Content-Type": "application/json", "X-Admin-Token": ADMIN_TOKEN}


def now_iso():
    return datetime.now(timezone.utc).isoformat()


def compose(*args):
    return ["docker", "compose", "-f", str(COMPOSE), *args]


def compose_env():
    env = os.environ.copy()
    env["CONFIG_DIR"] = str(RUNTIME)
    return env


def wait_gateway(timeout=90):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        connection = http.client.HTTPConnection("127.0.0.1", 8080, timeout=2)
        try:
            connection.request("GET", "/health")
            if connection.getresponse().status == 200:
                return
        except Exception:
            pass
        finally:
            connection.close()
        time.sleep(0.25)
    raise RuntimeError("gateway did not become healthy")


def switch_config(config):
    subprocess.run([str(EXP / "scripts" / "prepare_config.sh"), config], check=True, env=compose_env())
    subprocess.run(
        compose("up", "-d", "--no-deps", "--force-recreate", "gateway"),
        check=True,
        env=compose_env(),
    )
    wait_gateway()


def fixture_credentials():
    tokens = json.loads((FIXTURES / "tokens.json").read_text())["tokens"]
    keys = json.loads((FIXTURES / "api_keys.json").read_text())
    return keys["valid"]["value"], tokens["valid_long"]["token"]


def admin_update_key(secret):
    started = time.perf_counter()
    started_wall = now_iso()
    connection = http.client.HTTPConnection("127.0.0.1", 8081, timeout=10)
    body = json.dumps({"secret": secret})
    connection.request("PUT", "/api/config/auth/api-key", body, ADMIN_HEADERS)
    response = connection.getresponse()
    response.read()
    status = response.status
    connection.close()
    return status, started, started_wall, time.perf_counter()


def request_status(api_key, jwt, path="/api/v1/users", timeout=2):
    connection = http.client.HTTPConnection("127.0.0.1", 8080, timeout=timeout)
    headers = {"X-API-Key": api_key, "Authorization": f"Bearer {jwt}"}
    started = time.perf_counter()
    try:
        connection.request("GET", path, headers=headers)
        response = connection.getresponse()
        response.read()
        return response.status, (time.perf_counter() - started) * 1000
    except Exception:
        return 0, (time.perf_counter() - started) * 1000
    finally:
        connection.close()


def health_status(timeout=2):
    connection = http.client.HTTPConnection("127.0.0.1", 8080, timeout=timeout)
    started = time.perf_counter()
    try:
        connection.request("GET", "/health")
        response = connection.getresponse()
        response.read()
        return response.status, (time.perf_counter() - started) * 1000
    except Exception:
        return 0, (time.perf_counter() - started) * 1000
    finally:
        connection.close()


def container_id():
    result = subprocess.run(
        ["docker", "inspect", "test-api-gateway", "--format", "{{.Id}}"],
        check=False,
        capture_output=True,
        text=True,
    )
    return result.stdout.strip() if result.returncode == 0 else ""


class Probe(threading.Thread):
    def __init__(self, path, api_key="", jwt="", interval=0.05):
        super().__init__(daemon=True)
        self.path = path
        self.api_key = api_key
        self.jwt = jwt
        self.interval = interval
        self.stop_event = threading.Event()
        self.samples = []
        self.lock = threading.Lock()

    def run(self):
        connection = None
        while not self.stop_event.is_set():
            started = time.perf_counter()
            started_wall = now_iso()
            status = 0
            try:
                if connection is None:
                    connection = http.client.HTTPConnection("127.0.0.1", 8080, timeout=3)
                headers = {}
                if self.api_key:
                    headers["X-API-Key"] = self.api_key
                if self.jwt:
                    headers["Authorization"] = f"Bearer {self.jwt}"
                connection.request("GET", self.path, headers=headers)
                response = connection.getresponse()
                response.read()
                status = response.status
            except Exception:
                status = 0
                if connection is not None:
                    connection.close()
                connection = None
            elapsed_ms = (time.perf_counter() - started) * 1000
            with self.lock:
                self.samples.append({
                    "timestamp": started_wall,
                    "perf": started,
                    "status": status,
                    "latency_ms": elapsed_ms,
                })
            remaining = self.interval - (time.perf_counter() - started)
            if remaining > 0:
                time.sleep(remaining)
        if connection is not None:
            connection.close()

    def stop(self):
        self.stop_event.set()
        self.join(timeout=5)

    def snapshot(self):
        with self.lock:
            return list(self.samples)


def revocation(api_key, jwt, reps=10):
    rows = []
    details = []
    for rep in range(1, reps + 1):
        status, _ = request_status(api_key, jwt)
        if status != 200:
            raise RuntimeError(f"pre-revocation request returned {status} on rep {rep}")

        probe = Probe("/api/v1/users", api_key=api_key, jwt=jwt, interval=0.05)
        probe.start()
        time.sleep(0.3)
        new_secret = f"revoked-{rep}-{time.time_ns()}"
        admin_status, t_start, t_change_wall, t_done = admin_update_key(new_secret)
        if admin_status != 200:
            probe.stop()
            raise RuntimeError(f"admin revocation returned {admin_status} on rep {rep}")

        first_reject = None
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            samples = probe.snapshot()
            for sample in samples:
                if sample["perf"] >= t_start and sample["status"] in (401, 403):
                    first_reject = sample
                    break
            if first_reject is not None:
                break
            time.sleep(0.01)
        time.sleep(0.05)
        probe.stop()
        samples = probe.snapshot()

        if first_reject is None:
            reject_ms = None
        else:
            reject_ms = round((first_reject["perf"] - t_start) * 1000, 3)

        in_flight_ok = sum(
            1 for sample in samples
            if t_start <= sample["perf"] < (first_reject["perf"] if first_reject else time.perf_counter())
            and sample["status"] == 200
        )

        restore_status, _, _, _ = admin_update_key(api_key)
        if restore_status != 200:
            raise RuntimeError(f"admin restore failed on rep {rep}: {restore_status}")
        deadline = time.monotonic() + 5
        restored = False
        while time.monotonic() < deadline:
            status, _ = request_status(api_key, jwt)
            if status == 200:
                restored = True
                break
            time.sleep(0.05)
        if not restored:
            raise RuntimeError(f"old key was not restored on rep {rep}")

        rows.append({
            "rep": rep,
            "t_change": t_change_wall,
            "t_first_reject_ms": "" if reject_ms is None else reject_ms,
        })
        details.append({
            "rep": rep,
            "t_change": t_change_wall,
            "admin_status": admin_status,
            "admin_done_ms": round((t_done - t_start) * 1000, 3),
            "t_first_reject_ms": reject_ms,
            "first_reject_status": None if first_reject is None else first_reject["status"],
            "in_flight_200": in_flight_ok,
            "samples": [
                {
                    "ms": round((sample["perf"] - t_start) * 1000, 3),
                    "status": sample["status"],
                    "latency_ms": round(sample["latency_ms"], 3),
                }
                for sample in samples
            ],
        })
        with (RAW / f"revocation_r{rep}.csv").open("w", newline="") as handle:
            writer = csv.DictWriter(handle, fieldnames=["ms", "status", "latency_ms"])
            writer.writeheader()
            for sample in samples:
                writer.writerow({
                    "ms": round((sample["perf"] - t_start) * 1000, 3),
                    "status": sample["status"],
                    "latency_ms": round(sample["latency_ms"], 3),
                })
        (RAW / f"revocation_r{rep}.json").write_text(json.dumps(details[-1], indent=2) + "\n")
        time.sleep(0.5)
    return rows, details


def k6_availability(summary_path, log_path, vus=50, duration="60s"):
    container_summary = f"/workdir/{summary_path.relative_to(REPO)}"
    args = compose(
        "--profile", "loadtest", "run", "--rm", "-T", "--no-deps",
        "-e", "BASE_URL=http://gateway:8080",
        "-e", "PATH_UNDER_TEST=/health",
        "-e", "MODE=closed",
        "-e", f"VUS={vus}",
        "-e", f"DURATION={duration}",
        "-e", f"SUMMARY_PATH={container_summary}",
        "k6", "run", "--quiet", "/workdir/experiments/k6/perf.js",
    )
    with log_path.open("wb") as handle:
        return subprocess.Popen(
            args,
            env=compose_env(),
            stdout=handle,
            stderr=subprocess.STDOUT,
        )


def availability(api_key, jwt):
    summary_path = RAW / "availability_summary.json"
    log_path = RAW / "availability_k6.log"
    probe = Probe("/health", interval=0.02)
    process = k6_availability(summary_path, log_path)
    started = time.perf_counter()
    sleep_until = started + 2
    while time.perf_counter() < sleep_until:
        time.sleep(0.01)
    probe.start()

    reload_times = []
    for index in range(1, 6):
        target = started + 10 * index
        while time.perf_counter() < target:
            time.sleep(0.01)
        new_secret = f"reload-{index}-{time.time_ns()}"
        status, t_start, t_wall, t_done = admin_update_key(new_secret)
        if status != 200:
            probe.stop()
            process.wait(timeout=10)
            raise RuntimeError(f"reload {index} returned {status}")
        reload_times.append({
            "index": index,
            "t_change": t_wall,
            "perf": t_start,
            "admin_done_ms": round((t_done - t_start) * 1000, 3),
        })

    process.wait(timeout=120)
    probe.stop()
    samples = probe.snapshot()

    restore_status, _, _, _ = admin_update_key(api_key)
    if restore_status != 200:
        raise RuntimeError(f"admin restore after availability failed: {restore_status}")
    deadline = time.monotonic() + 5
    while time.monotonic() < deadline:
        status, _ = request_status(api_key, jwt)
        if status == 200:
            break
        time.sleep(0.05)

    with (RAW / "availability_probe.csv").open("w", newline="") as handle:
        writer = csv.DictWriter(handle, fieldnames=["timestamp", "status", "latency_ms"])
        writer.writeheader()
        for sample in samples:
            writer.writerow({
                "timestamp": sample["timestamp"],
                "status": sample["status"],
                "latency_ms": round(sample["latency_ms"], 3),
            })

    summary = json.loads(summary_path.read_text()) if summary_path.exists() else {}
    metrics = summary.get("metrics", {})
    k6_error_rate = metrics.get("http_req_failed", {}).get("values", {}).get("rate")
    k6_rps = metrics.get("http_reqs", {}).get("values", {}).get("rate")
    k6_p95 = metrics.get("http_req_duration", {}).get("values", {}).get("p(95)")

    baseline = [sample["latency_ms"] for sample in samples if sample["status"] == 200]
    median_baseline = statistics.median(baseline) if baseline else None
    reload_rows = []
    for item in reload_times:
        window = [
            sample for sample in samples
            if abs(sample["perf"] - item["perf"]) <= 0.5
        ]
        latencies = [sample["latency_ms"] for sample in window if sample["status"] == 200]
        reload_rows.append({
            "index": item["index"],
            "t_change": item["t_change"],
            "admin_done_ms": item["admin_done_ms"],
            "window_requests": len(window),
            "window_non_200": sum(1 for sample in window if sample["status"] != 200),
            "window_max_latency_ms": round(max(latencies), 3) if latencies else None,
            "window_p95_latency_ms": round(
                statistics.quantiles(latencies, n=20)[18], 3
            ) if len(latencies) >= 20 else (round(max(latencies), 3) if latencies else None),
        })

    with (RAW / "availability_reloads.csv").open("w", newline="") as handle:
        writer = csv.DictWriter(handle, fieldnames=list(reload_rows[0].keys()))
        writer.writeheader()
        writer.writerows(reload_rows)

    result = {
        "k6": {
            "vus": 50,
            "duration": "60s",
            "rps": k6_rps,
            "error_rate": k6_error_rate,
            "p95_ms": k6_p95,
            "exit_code": process.returncode,
        },
        "probe": {
            "requests": len(samples),
            "non_200": sum(1 for sample in samples if sample["status"] != 200),
            "median_latency_ms": round(median_baseline, 3) if median_baseline is not None else None,
            "max_latency_ms": round(max(baseline), 3) if baseline else None,
        },
        "reloads": reload_rows,
    }
    (RAW / "availability.json").write_text(json.dumps(result, indent=2) + "\n")
    return result


def write_summary(rows, details, availability_result):
    values = [row["t_first_reject_ms"] for row in rows if row["t_first_reject_ms"] != ""]
    lines = [
        "# Ringkasan Hot Reload dan Pencabutan",
        "",
        "## Pencabutan API key (config D)",
        "",
        f"- Repetisi: {len(rows)}",
        f"- Rerata t_first_reject: {round(statistics.mean(values), 3)} ms" if values else "- Rerata t_first_reject: n/a",
        f"- Minimum: {round(min(values), 3)} ms" if values else "- Minimum: n/a",
        f"- Maksimum: {round(max(values), 3)} ms" if values else "- Maksimum: n/a",
        f"- Request in-flight yang masih 200: {sum(item['in_flight_200'] for item in details)}",
        "",
        "## Ketersediaan saat reload (50 VU, 60 s, rute /health)",
        "",
        f"- k6 RPS: {availability_result['k6']['rps']}",
        f"- k6 error rate: {availability_result['k6']['error_rate']}",
        f"- k6 p95: {availability_result['k6']['p95_ms']} ms",
        f"- Probe non-200: {availability_result['probe']['non_200']} dari {availability_result['probe']['requests']}",
        f"- Probe median: {availability_result['probe']['median_latency_ms']} ms",
        f"- Probe maksimum: {availability_result['probe']['max_latency_ms']} ms",
        "",
        "| Reload | t_change | admin selesai (ms) | non-200 di window | max latency window (ms) |",
        "|---|---|---|---|---|",
    ]
    for item in availability_result["reloads"]:
        lines.append(
            f"| {item['index']} | {item['t_change']} | {item['admin_done_ms']} | "
            f"{item['window_non_200']} | {item['window_max_latency_ms']} |"
        )
    (RAW / "summary.md").write_text("\n".join(lines) + "\n")


def main():
    RAW.mkdir(parents=True, exist_ok=True)
    api_key, jwt = fixture_credentials()
    switch_config("D")
    before_container = container_id()
    rows, details = revocation(api_key, jwt)
    with (RESULTS / "hotreload.csv").open("w", newline="") as handle:
        writer = csv.DictWriter(handle, fieldnames=["rep", "t_change", "t_first_reject_ms"])
        writer.writeheader()
        writer.writerows(rows)
    availability_result = availability(api_key, jwt)
    after_container = container_id()
    availability_result["container_same"] = before_container == after_container
    (RAW / "availability.json").write_text(json.dumps(availability_result, indent=2) + "\n")
    write_summary(rows, details, availability_result)
    print(json.dumps({"revocation": rows, "availability": availability_result}, indent=2))


if __name__ == "__main__":
    main()
