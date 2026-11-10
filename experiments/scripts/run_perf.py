#!/usr/bin/env python3
import argparse
import csv
import json
import os
import random
import subprocess
import sys
import time
import urllib.request
from datetime import datetime, timezone
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
EXP = REPO / "experiments"
COMPOSE = EXP / "compose" / "docker-compose.test.yml"
RUNTIME = EXP / "runtime"
PERF_JS = "/workdir/experiments/k6/perf.js"
RAW = EXP / "results" / "raw" / "perf"
FIXTURES = EXP / "fixtures"

CREDS = {
    "Z": {},
    "A": {},
    "B": {"API_KEY": None, "API_KEY_HEADER": "X-API-Key"},
    "C": {"JWT": None},
    "D": {"API_KEY": None, "API_KEY_HEADER": "X-API-Key", "JWT": None},
    "C2": {"JWT": None},
    "F": {"API_KEY": None, "API_KEY_HEADER": "X-API-Key", "JWT": None},
}

TREND_FIELDS = ["avg", "min", "med", "p(90)", "p(95)", "p(99)", "max"]


def now():
    return datetime.now(timezone.utc).isoformat()


def log(message):
    print(f"[{now()}] {message}", flush=True)


def compose(*args):
    return ["docker", "compose", "-f", str(COMPOSE), *args]


def compose_env():
    env = os.environ.copy()
    env["CONFIG_DIR"] = str(RUNTIME)
    return env


def load_credentials():
    tokens = json.loads((FIXTURES / "tokens.json").read_text())["tokens"]
    keys = json.loads((FIXTURES / "api_keys.json").read_text())
    resolved = {}
    for config, creds in CREDS.items():
        item = dict(creds)
        if "API_KEY" in item:
            item["API_KEY"] = keys["valid"]["value"]
        if "JWT" in item:
            item["JWT"] = tokens["valid_long_rs256"]["token"] if config == "C2" else tokens["valid_long"]["token"]
        resolved[config] = item
    return resolved


def run_command(args, log_path=None):
    if log_path is None:
        return subprocess.run(args, env=compose_env(), check=True).returncode
    log_path.parent.mkdir(parents=True, exist_ok=True)
    with log_path.open("wb") as handle:
        return subprocess.run(
            args,
            env=compose_env(),
            stdout=handle,
            stderr=subprocess.STDOUT,
            check=False,
        ).returncode


def wait_health(timeout=90):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        try:
            with urllib.request.urlopen("http://localhost:8080/health", timeout=2) as response:
                if response.status == 200:
                    return
        except Exception:
            pass
        time.sleep(0.5)
    raise RuntimeError("gateway health check failed")


def prepare_config(config):
    subprocess.run(
        [str(EXP / "scripts" / "prepare_config.sh"), config],
        check=True,
        env=compose_env(),
    )


def start_stack():
    prepare_config("A")
    run_command(compose("build", "gateway", "mock-server"))
    run_command(compose("up", "-d", "--force-recreate", "gateway", "mock-server"))
    wait_health()


def switch_gateway(config):
    prepare_config(config)
    run_command(compose("up", "-d", "--no-deps", "--force-recreate", "gateway"))
    wait_health()


def run_k6(config, mode, vus, rate, duration, outdir, measured, creds):
    env = {
        "BASE_URL": "http://mock-server:9090" if config == "Z" else "http://gateway:8080",
        "PATH_UNDER_TEST": "/api/v1/users",
        "MODE": mode,
        "VUS": str(vus),
        "RATE": str(rate),
        "DURATION": duration,
    }
    env.update(creds)
    if measured:
        env["SUMMARY_PATH"] = f"/workdir/experiments/results/raw/perf/{outdir.name}/summary.json"

    args = compose(
        "--profile", "loadtest", "run", "--rm", "-T", "--no-deps",
    )
    for key, value in env.items():
        args.extend(["-e", f"{key}={value}"])
    args.extend(["k6", "run", "--quiet"])
    if not measured:
        args.extend(["--summary-mode", "disabled"])
    args.append(PERF_JS)

    return run_command(args, outdir / ("k6.log" if measured else "warmup.log"))


def start_sampler(stats_path):
    stats_path.parent.mkdir(parents=True, exist_ok=True)
    return subprocess.Popen(
        ["bash", str(EXP / "scripts" / "sample_stats.sh"), str(stats_path), "--interval", "1"],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )


def stop_sampler(proc):
    proc.terminate()
    try:
        proc.wait(timeout=10)
    except subprocess.TimeoutExpired:
        proc.kill()
        proc.wait()


def read_metric(metrics, name, field="rate"):
    values = metrics.get(name, {}).get("values", {})
    return values.get(field)


def summarize_run(outdir, config, mode, vus, rate, rep, rc, started, finished):
    summary_path = outdir / "summary.json"
    stats_path = outdir / "stats.csv"
    summary = json.loads(summary_path.read_text()) if summary_path.exists() else {}
    metrics = summary.get("metrics", {})
    duration = metrics.get("http_req_duration", {}).get("values", {})
    error_rate = read_metric(metrics, "http_req_failed", "rate")
    checks = read_metric(metrics, "checks", "rate")
    request_count = read_metric(metrics, "http_reqs", "count")
    rps = read_metric(metrics, "http_reqs", "rate")

    gateway_rows = []
    if stats_path.exists():
        with stats_path.open(newline="") as handle:
            for row in csv.DictReader(handle):
                if row.get("container") == "test-api-gateway":
                    gateway_rows.append(row)

    cpu_values = [float(row["cpu_percent"]) for row in gateway_rows if row.get("cpu_percent")]
    mem_values = [float(row["mem_usage_mib"]) for row in gateway_rows if row.get("mem_usage_mib")]
    cpu_mean = round(sum(cpu_values) / len(cpu_values), 3) if cpu_values else None
    mem_mean = round(sum(mem_values) / len(mem_values), 3) if mem_values else None
    mem_max = round(max(mem_values), 3) if mem_values else None

    invalid_reason = ""
    if rc != 0:
        invalid_reason = f"k6 exit code {rc}"
    elif not summary_path.exists():
        invalid_reason = "summary.json missing"
    elif error_rate is None:
        invalid_reason = "http_req_failed metric missing"
    elif error_rate > 0.01:
        invalid_reason = f"error rate {error_rate:.4f} > 0.01"

    record = {
        "run_id": outdir.name,
        "config": config,
        "mode": mode,
        "vus": vus,
        "rate": rate,
        "rep": rep,
        "started_at": started,
        "finished_at": finished,
        "k6_exit_code": rc,
        "valid": invalid_reason == "",
        "invalid_reason": invalid_reason,
        "metrics": {
            "requests": request_count,
            "rps": rps,
            "error_rate": error_rate,
            "checks_rate": checks,
            "latency_ms": {field: duration.get(field) for field in TREND_FIELDS},
        },
        "resources": {
            "gateway_cpu_percent_mean": cpu_mean,
            "gateway_mem_mib_mean": mem_mean,
            "gateway_mem_mib_max": mem_max,
            "samples": len(gateway_rows),
        },
        "files": {
            "summary": "summary.json",
            "stats": "stats.csv",
            "k6_log": "k6.log",
            "warmup_log": "warmup.log",
        },
    }
    (outdir / "run.json").write_text(json.dumps(record, indent=2) + "\n")
    return record


def run_one(config, mode, vus, rate, rep, creds, args):
    run_id = f"{config}_vu{vus}_{mode}_r{rep}"
    outdir = RAW / run_id
    outdir.mkdir(parents=True, exist_ok=True)
    summary_path = outdir / "summary.json"
    stats_path = outdir / "stats.csv"

    if not args.force and summary_path.exists() and stats_path.exists() and (outdir / "run.json").exists():
        log(f"skip existing {run_id}")
        return

    log(f"start {run_id} target={'mock' if config == 'Z' else 'gateway'} rate={rate}")
    started = now()
    if not args.skip_warmup:
        rc = run_k6(config, mode, vus, rate, args.warmup_seconds, outdir, False, creds)
        if rc != 0:
            log(f"warmup failed for {run_id} (rc={rc})")

    sampler = start_sampler(stats_path)
    time.sleep(1)
    try:
        rc = run_k6(config, mode, vus, rate, args.duration, outdir, True, creds)
    finally:
        stop_sampler(sampler)
    finished = now()

    if summary_path.exists():
        record = summarize_run(outdir, config, mode, vus, rate, rep, rc, started, finished)
        status = "ok" if record["valid"] else f"INVALID: {record['invalid_reason']}"
        log(f"done {run_id} {status} rps={record['metrics']['rps']} p95={record['metrics']['latency_ms']['p(95)']}")
    else:
        log(f"done {run_id} without summary (rc={rc})")
        if rc != 0:
            raise RuntimeError(f"k6 failed for {run_id}; see {outdir / 'k6.log'}")

    if args.sleep > 0:
        time.sleep(args.sleep)


def compute_open_rate(args):
    if args.open_rate:
        return args.open_rate
    rates = []
    for run_json in RAW.glob("A_vu*_closed_r*/run.json"):
        data = json.loads(run_json.read_text())
        value = data.get("metrics", {}).get("rps")
        if value:
            rates.append(float(value))
    if not rates:
        raise RuntimeError("no closed A runs found; pass --open-rate explicitly")
    return max(1, int(max(rates) * 0.5))


def write_manifest():
    rows = []
    for run_json in sorted(RAW.glob("*/run.json")):
        data = json.loads(run_json.read_text())
        rows.append({
            "run_id": data["run_id"],
            "config": data["config"],
            "mode": data["mode"],
            "vus": data["vus"],
            "rate": data["rate"],
            "rep": data["rep"],
            "valid": data["valid"],
            "invalid_reason": data["invalid_reason"],
            "requests": data["metrics"]["requests"],
            "rps": data["metrics"]["rps"],
            "error_rate": data["metrics"]["error_rate"],
            "avg_ms": data["metrics"]["latency_ms"]["avg"],
            "p95_ms": data["metrics"]["latency_ms"]["p(95)"],
            "p99_ms": data["metrics"]["latency_ms"]["p(99)"],
            "gateway_cpu_percent_mean": data["resources"]["gateway_cpu_percent_mean"],
            "gateway_mem_mib_mean": data["resources"]["gateway_mem_mib_mean"],
        })
    if not rows:
        return
    with (RAW / "manifest.csv").open("w", newline="") as handle:
        writer = csv.DictWriter(handle, fieldnames=list(rows[0].keys()))
        writer.writeheader()
        writer.writerows(rows)
    (RAW / "manifest.json").write_text(json.dumps(rows, indent=2) + "\n")


def parse_args():
    parser = argparse.ArgumentParser()
    parser.add_argument("--configs", default="Z,A,B,C,D")
    parser.add_argument("--closed-vus", default="50,100,200")
    parser.add_argument("--reps", type=int, default=3)
    parser.add_argument("--modes", default="closed,open")
    parser.add_argument("--open-configs", default="A,B,C,D")
    parser.add_argument("--open-vus", type=int, default=200)
    parser.add_argument("--open-rate", type=int)
    parser.add_argument("--duration", default="60s")
    parser.add_argument("--warmup-seconds", default="10s")
    parser.add_argument("--sleep", type=float, default=30)
    parser.add_argument("--seed", type=int, default=20261109)
    parser.add_argument("--force", action="store_true")
    parser.add_argument("--skip-warmup", action="store_true")
    return parser.parse_args()


def main():
    args = parse_args()
    random.seed(args.seed)
    RAW.mkdir(parents=True, exist_ok=True)
    creds = load_credentials()
    configs = [item.strip() for item in args.configs.split(",") if item.strip()]
    vus_values = [int(item) for item in args.closed_vus.split(",") if item.strip()]
    modes = [item.strip() for item in args.modes.split(",") if item.strip()]

    missing = [config for config in configs if config not in creds]
    if missing:
        raise SystemExit(f"unknown configs: {missing}")

    start_stack()

    if "closed" in modes:
        for rep in range(1, args.reps + 1):
            order = configs.copy()
            random.shuffle(order)
            log(f"closed repetition {rep} order={order}")
            for config in order:
                if config != "Z":
                    switch_gateway(config)
                for vus in vus_values:
                    run_one(config, "closed", vus, 0, rep, creds[config], args)
                write_manifest()

    if "open" in modes:
        rate = compute_open_rate(args)
        open_configs = [item.strip() for item in args.open_configs.split(",") if item.strip()]
        for rep in range(1, args.reps + 1):
            order = open_configs.copy()
            random.shuffle(order)
            log(f"open repetition {rep} rate={rate} order={order}")
            for config in order:
                switch_gateway(config)
                run_one(config, "open", args.open_vus, rate, rep, creds[config], args)
                write_manifest()

    write_manifest()
    log("performance matrix complete")


if __name__ == "__main__":
    main()
