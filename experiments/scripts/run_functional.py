#!/usr/bin/env python3
"""Functional test runner for the thesis experiments (F01-F09).

Every case appends one row to results/functional.csv and stores its full raw
observation under results/raw/functional/. The Markdown table is generated
from the CSV at the end (no hand-typed results).
"""
from __future__ import annotations

import csv
import json
import os
import shutil
import subprocess
import sys
import time
import urllib.error
import urllib.request
from datetime import datetime, timezone

EXP_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
REPO_DIR = os.path.dirname(EXP_DIR)
FIXTURES = os.path.join(EXP_DIR, "fixtures")
RESULTS = os.path.join(EXP_DIR, "results")
RAW_DIR = os.path.join(RESULTS, "raw", "functional")
TABLES = os.path.join(RESULTS, "tables")
RUNTIME_CONFIG = os.path.join(EXP_DIR, "runtime", "config.yaml")
COMPOSE_CONFIG = os.path.join(EXP_DIR, "compose")
BASE = "http://localhost:8080"
ADMIN = "http://localhost:8081"
ADMIN_TOKEN = "test-admin-token"

os.makedirs(RAW_DIR, exist_ok=True)
os.makedirs(TABLES, exist_ok=True)

with open(os.path.join(FIXTURES, "tokens.json"), encoding="utf-8") as fh:
    FIX = json.load(fh)

API_KEY = FIX["api_keys"]["valid"]["value"]
VALID_JWT = FIX["tokens"]["valid_long"]["token"]
VALID_JWT_RS = FIX["tokens"]["valid_long_rs256"]["token"]


def now_iso() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="milliseconds").replace("+00:00", "Z")


def run(cmd: list[str], check: bool = True, timeout: int = 300) -> subprocess.CompletedProcess:
    return subprocess.run(cmd, cwd=REPO_DIR, check=check, text=True,
                          stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=timeout)


def docker(*args: str, check: bool = True, timeout: int = 120) -> str:
    return run(["docker", *args], check=check, timeout=timeout).stdout.strip()


def http(method: str, url: str, headers: dict[str, str] | None = None,
         body: bytes | None = None, timeout: float = 10.0) -> dict:
    req = urllib.request.Request(url, data=body, headers=headers or {}, method=method)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            raw = resp.read()
            status = resp.status
            hdrs = dict(resp.headers.items())
    except urllib.error.HTTPError as err:
        raw = err.read()
        status = err.code
        hdrs = dict(err.headers.items())
    except Exception as err:  # connection refused / timeout
        return {"status": 0, "body": b"", "text": "", "headers": {}, "error": str(err)}
    return {"status": status, "body": raw, "text": raw.decode("utf-8", "replace"),
            "headers": hdrs, "error": ""}


def wait_health(timeout: float = 60.0, cfg: str = "") -> bool:
    deadline = time.time() + timeout
    while time.time() < deadline:
        r = http("GET", BASE + "/health", timeout=2)
        if r["status"] == 200:
            return True
        time.sleep(0.25)
    return False


def switch_config(cfg: str, restart: bool = True) -> None:
    run(["make", "up", f"CONFIG={cfg}"], timeout=600)
    if restart:
        docker("restart", "test-api-gateway")
    if not wait_health(60, cfg):
        raise RuntimeError(f"gateway did not become healthy for config {cfg}")


def network_name() -> str:
    return docker("inspect", "test-api-gateway",
                  "--format", "{{range $k,$v := .NetworkSettings.Networks}}{{$k}}{{end}}")


def direct_mock(path: str) -> dict:
    net = network_name()
    data = docker("run", "--rm", "--network", net, "curlimages/curl:8.10.1",
                  "-sS", "--max-time", "10", "http://mock-server:9090" + path)
    return {"status": 200 if data else 0, "body": data}


def creds_both() -> dict[str, str]:
    return {"X-API-Key": API_KEY, "Authorization": "Bearer " + VALID_JWT}


def record(case_id: str, description: str, cfg: str, request: str,
           expected, actual, passed: bool, notes: str, raw: dict) -> dict:
    row = {
        "id": case_id,
        "description": description,
        "config": cfg,
        "request": request,
        "expected_status": expected,
        "actual_status": actual,
        "pass": "PASS" if passed else "FAIL",
        "notes": notes,
    }
    with open(os.path.join(RAW_DIR, f"{case_id}_{datetime.now().strftime('%Y%m%dT%H%M%S')}.json"),
              "w", encoding="utf-8") as fh:
        json.dump({"timestamp": now_iso(), "row": row, "raw": raw}, fh, indent=2)
    return row


def case_f01() -> dict:
    r = http("GET", BASE + "/api/v1/users", creds_both())
    upstream = direct_mock("/api/v1/users")
    passed = r["status"] == 200 and r["text"].strip() == upstream["body"].strip()
    return record("F01", "Rute valid + kredensial valid (API key + JWT)", "D",
                  "GET /api/v1/users + X-API-Key + Bearer JWT", "200; body == upstream",
                  r["status"], passed,
                  f"body={r['text'].strip()!r}; upstream={upstream['body']!r}",
                  {"response": r["text"], "upstream": upstream["body"]})


def case_f02() -> dict:
    r = http("GET", BASE + "/api/v1/does-not-exist", creds_both())
    passed = r["status"] == 404
    return record("F02", "Rute tidak terdaftar", "D",
                  "GET /api/v1/does-not-exist + kredensial valid", "404", r["status"],
                  passed, f"body={r['text'].strip()!r}", {"response": r["text"]})


def case_f03() -> dict:
    r = http("GET", BASE + "/health")
    upstream = direct_mock("/health")
    passed = r["status"] == 200 and r["text"].strip() == upstream["body"].strip()
    return record("F03", "Rute publik (auth override enabled=false) tanpa kredensial", "D",
                  "GET /health tanpa kredensial", "200; body == upstream", r["status"],
                  passed, f"body={r['text'].strip()!r}", {"response": r["text"]})


def case_f04() -> dict:
    headers = {"X-API-Key": API_KEY}
    r = http("GET", BASE + "/api/v1/users", headers)
    passed = r["status"] == 401
    return record("F04", "API key valid saja pada rute yang butuh JWT", "D",
                  "GET /api/v1/users + X-API-Key (tanpa Authorization)", "401", r["status"],
                  passed, f"body={r['text'].strip()!r}", {"response": r["text"]})


def case_f05() -> dict:
    headers = {"Authorization": "Bearer " + VALID_JWT}
    r = http("GET", BASE + "/api/v1/users", headers)
    passed = r["status"] == 401
    return record("F05", "JWT valid saja pada rute yang butuh API key", "D",
                  "GET /api/v1/users + Bearer JWT (tanpa X-API-Key)", "401/403", r["status"],
                  passed, f"body={r['text'].strip()!r}", {"response": r["text"]})


def case_f06() -> dict:
    r = http("GET", BASE + "/echo", creds_both())
    observed = ""
    try:
        payload = json.loads(r["text"])
        scope = payload.get("headers", {}).get("X-Scope", [])
        observed = scope[0] if isinstance(scope, list) and scope else str(scope)
    except Exception:
        payload = {}
    passed = r["status"] == 200 and observed == "route"
    return record("F06", "Prioritas konfigurasi route > service > global (request-transform)", "F06",
                  "GET /echo + kredensial valid; X-Scope global/service/route", "200; X-Scope=route",
                  r["status"], passed,
                  f"X-Scope teramati={observed!r} (global=global, service=service, route=route)",
                  {"response": payload})


def case_f07() -> dict:
    container_before = docker("inspect", "test-api-gateway", "--format", "{{.Id}}")
    initial = http("GET", BASE + "/api/v1/users")
    start = time.time()
    with open(RUNTIME_CONFIG, encoding="utf-8") as fh:
        text = fh.read()
    mutated = text.replace("      - name: auth\n        enabled: true",
                           "      - name: auth\n        enabled: false", 1)
    if mutated == text:
        return record("F07", "Hot reload tanpa restart", "D", "ubah config runtime", "401 -> 200 -> 401",
                      0, False, "pola plugin auth service tidak ditemukan", {"mutated": False})
    with open(RUNTIME_CONFIG, "w", encoding="utf-8") as fh:
        fh.write(mutated)
    t_open = None
    deadline = time.time() + 15
    while time.time() < deadline:
        rr = http("GET", BASE + "/api/v1/users", timeout=2)
        if rr["status"] == 200:
            t_open = round(time.time() - start, 3)
            break
        time.sleep(0.1)
    shutil.copyfile(os.path.join(COMPOSE_CONFIG, "config.D.yaml"), RUNTIME_CONFIG)
    t_restore = None
    deadline = time.time() + 15
    while time.time() < deadline:
        rr = http("GET", BASE + "/api/v1/users", timeout=2)
        if rr["status"] == 401:
            t_restore = round(time.time() - start, 3)
            break
        time.sleep(0.1)
    container_after = docker("inspect", "test-api-gateway", "--format", "{{.Id}}")
    passed = initial["status"] == 401 and t_open is not None and t_restore is not None and container_before == container_after
    return record("F07", "Perubahan konfigurasi (hot reload) diterapkan tanpa restart", "D",
                  "Ubah plugin auth service lalu pulihkan; request tanpa kredensial", "401 -> 200 -> 401",
                  f"initial={initial['status']}, open@{t_open}s, restore@{t_restore}s", passed,
                  f"container sama={container_before == container_after}",
                  {"open_seconds": t_open, "restore_seconds": t_restore,
                   "container_same": container_before == container_after})


def case_f08() -> dict:
    statuses = []
    final = None
    try:
        docker("stop", "test-mock-server", timeout=30)
        for _ in range(8):
            r = http("GET", BASE + "/api/v1/users", creds_both(), timeout=5)
            statuses.append(r["status"])
            time.sleep(0.05)
        docker("start", "test-mock-server", timeout=30)
        net = network_name()
        deadline = time.time() + 20
        while time.time() < deadline:
            probe = run(["docker", "run", "--rm", "--network", net, "curlimages/curl:8.10.1",
                         "-sS", "--max-time", "2", "http://mock-server:9090/health"],
                        check=False, timeout=20)
            if probe.returncode == 0:
                break
            time.sleep(0.5)
        time.sleep(32)  # breaker cooldown (Open -> HalfOpen)
        final = http("GET", BASE + "/api/v1/users", creds_both(), timeout=5)
    finally:
        run(["docker", "start", "test-mock-server"], check=False, timeout=30)
    passed = 503 in statuses and final is not None and final["status"] == 200
    return record("F08", "Upstream mati -> circuit breaker terbuka; pulih setelah hidup", "D",
                  "Hentikan mock-server, 8 request, hidupkan, tunggu cooldown 30s", "muncul 503 lalu 200",
                  statuses, passed, f"final={None if final is None else final['status']}",
                  {"status_sequence": statuses, "final": None if final is None else final["status"]})


def case_f09() -> dict:
    body = b'{"hello":"world"}'
    headers = {**creds_both(), "Content-Type": "application/json",
               "X-Custom": "thesis", "X-User-Id": "attacker"}
    r = http("POST", BASE + "/echo", headers, body)
    payload = {}
    try:
        payload = json.loads(r["text"])
    except Exception:
        pass
    forwarded = payload.get("headers", {})
    user_id = forwarded.get("X-User-Id", [])
    user_id_value = user_id[0] if isinstance(user_id, list) and user_id else str(user_id)
    custom = forwarded.get("X-Custom", [])
    custom_value = custom[0] if isinstance(custom, list) and custom else str(custom)
    passed = (r["status"] == 200 and payload.get("method") == "POST"
              and payload.get("body") == body.decode()
              and custom_value == "thesis" and user_id_value == "user-123")
    return record("F09", "Header/body diteruskan ke upstream; X-User-Id disanitasi", "D",
                  "POST /echo + X-Custom + body JSON + X-User-Id spoof", "body/header sama; X-User-Id=user-123",
                  r["status"], passed,
                  f"method={payload.get('method')}, X-Custom={custom_value!r}, X-User-Id={user_id_value!r}",
                  {"response": payload})


def write_csv(rows: list[dict]) -> None:
    path = os.path.join(RESULTS, "functional.csv")
    with open(path, "w", newline="", encoding="utf-8") as fh:
        writer = csv.DictWriter(fh, fieldnames=["id", "description", "config", "request",
                                                "expected_status", "actual_status", "pass", "notes"])
        writer.writeheader()
        writer.writerows(rows)


def write_table(rows: list[dict]) -> None:
    lines = [
        "# Tabel Uji Fungsional (F01–F09)",
        "",
        "Dihasilkan otomatis oleh `experiments/scripts/run_functional.py` dari `results/functional.csv`.",
        "",
        "| ID | Skenario | Harapan | Hasil | Status |",
        "|---|---|---|---|---|",
    ]
    for row in rows:
        lines.append(
            f"| {row['id']} | {row['description']} | {row['expected_status']} | "
            f"{row['actual_status']} | {row['pass']} |"
        )
    lines += ["", "Catatan per kasus tersimpan pada kolom `notes` di `results/functional.csv` "
                  "dan data mentah di `results/raw/functional/`.", ""]
    with open(os.path.join(TABLES, "table_functional.md"), "w", encoding="utf-8") as fh:
        fh.write("\n".join(lines))


def main() -> int:
    rows = []
    try:
        switch_config("D")
        rows.append(case_f01())
        rows.append(case_f02())
        rows.append(case_f03())
        rows.append(case_f04())
        rows.append(case_f05())

        switch_config("F06")
        rows.append(case_f06())
    except Exception as err:
        print(f"Fase F01-F06 gagal: {err}", file=sys.stderr)
        row = record("F00", "Setup F01-F06", "-", "-", "semua", 0, False, str(err), {})
        rows.append(row)

    try:
        switch_config("D")
        rows.append(case_f07())
    except Exception as err:
        rows.append(record("F07", "Hot reload", "D", "-", "401 -> 200 -> 401", 0, False, str(err), {}))

    try:
        switch_config("D")
        rows.append(case_f08())
    except Exception as err:
        rows.append(record("F08", "Circuit breaker", "D", "-", "503 lalu 200", 0, False, str(err), {}))

    try:
        switch_config("D")
        rows.append(case_f09())
    except Exception as err:
        rows.append(record("F09", "Header/body forwarding", "D", "-", "200", 0, False, str(err), {}))

    # Leave the environment in the baseline D configuration.
    try:
        switch_config("D")
    except Exception:
        pass

    write_csv(rows)
    write_table(rows)
    failed = [r["id"] for r in rows if r["pass"] != "PASS"]
    print(f"functional: {len(rows) - len(failed)}/{len(rows)} PASS; failed={failed}")
    return 0 if not failed else 1


if __name__ == "__main__":
    sys.exit(main())
