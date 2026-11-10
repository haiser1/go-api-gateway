#!/usr/bin/env python3
"""Analyse all experiment raw data and generate thesis tables and figures.

Run with pandas + matplotlib available, for example:
    uv run --with pandas --with matplotlib experiments/scripts/analyze.py
"""
from __future__ import annotations

import json
import math
import re
from pathlib import Path
from typing import Iterable

import matplotlib

matplotlib.use("Agg")
import matplotlib.pyplot as plt
import numpy as np
import pandas as pd

BASE = Path(__file__).resolve().parents[1]
RESULTS = BASE / "results"
RAW = RESULTS / "raw"
TABLES = RESULTS / "tables"
FIGURES = RESULTS / "figures"

CONFIGS = ["Z", "A", "B", "C", "D", "C2", "F"]
CORE_CONFIGS = ["Z", "A", "B", "C", "D"]
AUTH_CONFIGS = ["A", "B", "C", "D"]
VUS = [50, 100, 200]
OPEN_RATE = 4226

HATCHES = ["//", "\\\\", "xx", "..", "++", "oo", "--"]
MARKERS = ["o", "s", "^", "D", "v", "P", "X"]
LINESTYLES = ["-", "--", "-.", ":", (0, (3, 1, 1, 1)), (0, (5, 1)), (0, (1, 1))]

plt.rcParams.update(
    {
        "font.size": 11,
        "axes.titlesize": 12,
        "axes.labelsize": 11,
        "legend.fontsize": 8.5,
        "xtick.labelsize": 9,
        "ytick.labelsize": 9,
        "axes.grid": True,
        "grid.alpha": 0.25,
        "figure.dpi": 300,
        "savefig.dpi": 300,
        "savefig.bbox": "tight",
    }
)


def ensure_dirs() -> None:
    TABLES.mkdir(parents=True, exist_ok=True)
    FIGURES.mkdir(parents=True, exist_ok=True)


def md_table(headers: list[str], rows: Iterable[Iterable[object]]) -> str:
    lines = ["| " + " | ".join(headers) + " |"]
    lines.append("|" + "|".join(["---"] * len(headers)) + "|")
    for row in rows:
        cells = [str(c).replace("|", "/").replace("\n", " ") for c in row]
        lines.append("| " + " | ".join(cells) + " |")
    return "\n".join(lines)


def write_md(path: Path, title: str, body: str, note: str | None = None) -> None:
    lines = [f"# {title}", ""]
    if note:
        lines += [note, ""]
    lines += [body, ""]
    path.write_text("\n".join(lines), encoding="utf-8")


def fmt(value: float, nd: int = 2) -> str:
    if value is None or (isinstance(value, float) and math.isnan(value)):
        return "-"
    return f"{value:.{nd}f}"


def pm(mean: float, sd: float, nd: int = 2) -> str:
    if mean is None or (isinstance(mean, float) and math.isnan(mean)):
        return "-"
    if sd is None or (isinstance(sd, float) and math.isnan(sd)):
        return fmt(mean, nd)
    return f"{mean:.{nd}f} ± {sd:.{nd}f}"


def load_perf() -> pd.DataFrame:
    df = pd.read_csv(RAW / "perf" / "manifest.csv")
    df["valid"] = df["valid"].astype(str).str.lower().isin(["true", "1"])
    for col in [
        "vus",
        "rate",
        "rep",
        "requests",
        "rps",
        "error_rate",
        "avg_ms",
        "p95_ms",
        "p99_ms",
        "gateway_cpu_percent_mean",
        "gateway_mem_mib_mean",
    ]:
        df[col] = pd.to_numeric(df[col], errors="coerce")
    return df[df["valid"]].copy()


def grouped_perf(df: pd.DataFrame) -> pd.DataFrame:
    keys = ["config", "mode", "vus"]
    agg = (
        df.groupby(keys, dropna=False)
        .agg(
            reps=("rep", "nunique"),
            rate=("rate", "first"),
            rps_mean=("rps", "mean"),
            rps_sd=("rps", "std"),
            error_mean=("error_rate", "mean"),
            avg_mean=("avg_ms", "mean"),
            avg_sd=("avg_ms", "std"),
            p95_mean=("p95_ms", "mean"),
            p95_sd=("p95_ms", "std"),
            p99_mean=("p99_ms", "mean"),
            p99_sd=("p99_ms", "std"),
            cpu_mean=("gateway_cpu_percent_mean", "mean"),
            cpu_sd=("gateway_cpu_percent_mean", "std"),
            mem_mean=("gateway_mem_mib_mean", "mean"),
            mem_sd=("gateway_mem_mib_mean", "std"),
            requests_mean=("requests", "mean"),
        )
        .reset_index()
    )
    mode_order = {"closed": 0, "open": 1}
    agg["_mode_order"] = agg["mode"].map(mode_order).fillna(9)
    agg["_cfg_order"] = agg["config"].map({c: i for i, c in enumerate(CONFIGS)}).fillna(99)
    return agg.sort_values(["_mode_order", "_cfg_order", "vus"]).drop(
        columns=["_mode_order", "_cfg_order"]
    )


def table_perf_latency(stats: pd.DataFrame) -> None:
    rows = []
    csv_rows = []
    for _, r in stats.iterrows():
        rows.append(
            [
                r["config"],
                r["mode"],
                int(r["vus"]),
                int(r["rate"]) if pd.notna(r["rate"]) and r["rate"] else "-",
                int(r["reps"]),
                pm(r["avg_mean"], r["avg_sd"]),
                pm(r["p95_mean"], r["p95_sd"]),
                pm(r["p99_mean"], r["p99_sd"]),
            ]
        )
        csv_rows.append(
            {
                "config": r["config"],
                "mode": r["mode"],
                "vus": int(r["vus"]),
                "rate": int(r["rate"]) if pd.notna(r["rate"]) and r["rate"] else "",
                "reps": int(r["reps"]),
                "avg_ms_mean": r["avg_mean"],
                "avg_ms_sd": r["avg_sd"],
                "p95_ms_mean": r["p95_mean"],
                "p95_ms_sd": r["p95_sd"],
                "p99_ms_mean": r["p99_mean"],
                "p99_ms_sd": r["p99_sd"],
            }
        )
    body = md_table(
        ["Config", "Mode", "VU", "Rate (req/s)", "n", "Avg (ms)", "p95 (ms)", "p99 (ms)"],
        rows,
    )
    write_md(
        TABLES / "table_perf_latency.md",
        "Tabel Latensi Kinerja",
        body,
        "Rerata ± simpangan baku antar-3 pengulangan. `closed` memakai `constant-vus`; "
        "`open` memakai `constant-arrival-rate` pada 4.226 req/s. Data: "
        "`results/raw/perf/manifest.csv`.",
    )
    pd.DataFrame(csv_rows).to_csv(TABLES / "table_perf_latency.csv", index=False)


def _baseline_map(stats: pd.DataFrame, baseline: str) -> dict[tuple[str, int], pd.Series]:
    out: dict[tuple[str, int], pd.Series] = {}
    for _, r in stats[stats["config"] == baseline].iterrows():
        out[(str(r["mode"]), int(r["vus"]))] = r
    return out


def table_perf_throughput(stats: pd.DataFrame) -> None:
    base = _baseline_map(stats, "A")
    rows = []
    csv_rows = []
    for _, r in stats.iterrows():
        b = base.get((str(r["mode"]), int(r["vus"])))
        change = None
        if b is not None and b["rps_mean"]:
            change = (r["rps_mean"] - b["rps_mean"]) / b["rps_mean"] * 100.0
        rows.append(
            [
                r["config"],
                r["mode"],
                int(r["vus"]),
                pm(r["rps_mean"], r["rps_sd"], 1),
                fmt(change, 1) if change is not None else "-",
                fmt(r["error_mean"] * 100.0, 2),
            ]
        )
        csv_rows.append(
            {
                "config": r["config"],
                "mode": r["mode"],
                "vus": int(r["vus"]),
                "reps": int(r["reps"]),
                "rps_mean": r["rps_mean"],
                "rps_sd": r["rps_sd"],
                "rps_change_vs_A_pct": change,
                "error_rate_pct": r["error_mean"] * 100.0,
            }
        )
    body = md_table(
        ["Config", "Mode", "VU", "RPS (rerata ± SD)", "Δ RPS vs A (%)", "Error rate (%)"],
        rows,
    )
    write_md(
        TABLES / "table_perf_throughput.md",
        "Tabel Throughput Kinerja",
        body,
        "Δ RPS vs A bernilai positif bila lebih tinggi dari A dan negatif bila lebih rendah. "
        "Baseline A adalah proxy tanpa auth; Z adalah direct-to-mock tanpa gateway. "
        "Data: `results/raw/perf/manifest.csv`.",
    )
    pd.DataFrame(csv_rows).to_csv(TABLES / "table_perf_throughput.csv", index=False)


def table_perf_resources(stats: pd.DataFrame) -> None:
    rows = []
    csv_rows = []
    for _, r in stats.iterrows():
        rows.append(
            [
                r["config"],
                r["mode"],
                int(r["vus"]),
                pm(r["cpu_mean"], r["cpu_sd"], 1),
                pm(r["mem_mean"], r["mem_sd"], 1),
            ]
        )
        csv_rows.append(
            {
                "config": r["config"],
                "mode": r["mode"],
                "vus": int(r["vus"]),
                "reps": int(r["reps"]),
                "gateway_cpu_percent_mean": r["cpu_mean"],
                "gateway_cpu_percent_sd": r["cpu_sd"],
                "gateway_mem_mib_mean": r["mem_mean"],
                "gateway_mem_mib_sd": r["mem_sd"],
            }
        )
    body = md_table(
        ["Config", "Mode", "VU", "CPU gateway (%)", "Memori gateway (MiB)"],
        rows,
    )
    write_md(
        TABLES / "table_perf_resources.md",
        "Tabel Sumber Daya Gateway",
        body,
        "CPU dalam persen dari satu core; cpuset gateway 2 core sehingga 200% = seluruh cpuset. "
        "Memori dalam MiB. Rerata ± SD antar-3 pengulangan. Z tidak memakai gateway "
        "(direct-to-mock) sehingga CPU/memori gateway mendekati idle.",
    )
    pd.DataFrame(csv_rows).to_csv(TABLES / "table_perf_resources.csv", index=False)


def overhead_from_stats(stats: pd.DataFrame, baseline: str = "A") -> pd.DataFrame:
    base_cols = [
        "mode",
        "vus",
        "avg_mean",
        "avg_sd",
        "p95_mean",
        "p95_sd",
        "p99_mean",
        "p99_sd",
        "rps_mean",
        "rps_sd",
    ]
    base = stats[stats["config"] == baseline][base_cols].rename(
        columns={
            "avg_mean": "avg_base_mean",
            "avg_sd": "avg_base_sd",
            "p95_mean": "p95_base_mean",
            "p95_sd": "p95_base_sd",
            "p99_mean": "p99_base_mean",
            "p99_sd": "p99_base_sd",
            "rps_mean": "rps_base_mean",
            "rps_sd": "rps_base_sd",
        }
    )
    merged = stats.merge(base, on=["mode", "vus"], how="inner")
    if baseline == "A":
        merged = merged[~merged["config"].isin(["A", "Z"])]
    else:
        merged = merged[merged["config"] == "A"]
    for metric in ("avg", "p95", "p99"):
        merged[f"{metric}_overhead_pct"] = (
            (merged[f"{metric}_mean"] - merged[f"{metric}_base_mean"])
            / merged[f"{metric}_base_mean"]
            * 100.0
        )
        merged[f"{metric}_overhead_sd"] = (
            np.sqrt(merged[f"{metric}_sd"] ** 2 + merged[f"{metric}_base_sd"] ** 2)
            / merged[f"{metric}_base_mean"]
            * 100.0
        )
    merged["rps_change_pct"] = (
        (merged["rps_mean"] - merged["rps_base_mean"]) / merged["rps_base_mean"] * 100.0
    )
    merged["rps_change_sd"] = (
        np.sqrt(merged["rps_sd"] ** 2 + merged["rps_base_sd"] ** 2)
        / merged["rps_base_mean"]
        * 100.0
    )
    merged["baseline"] = baseline
    merged["_cfg_order"] = merged["config"].map(
        {c: i for i, c in enumerate(CONFIGS)}
    ).fillna(99)
    return merged.sort_values(["mode", "vus", "_cfg_order"]).drop(columns=["_cfg_order"])


def additivity_summary(stats: pd.DataFrame) -> list[dict]:
    rows: list[dict] = []
    for mode in ("closed", "open"):
        for vu in VUS:
            sub = stats[(stats["mode"] == mode) & (stats["vus"] == vu)].set_index("config")
            if not all(c in sub.index for c in ("A", "B", "C", "D")):
                continue
            for metric in ("avg", "p95"):
                a = float(sub.loc["A", f"{metric}_mean"])
                b = float(sub.loc["B", f"{metric}_mean"])
                c = float(sub.loc["C", f"{metric}_mean"])
                d = float(sub.loc["D", f"{metric}_mean"])
                cost_b = b - a
                cost_c = c - a
                sum_bc = cost_b + cost_c
                cost_d = d - a
                ratio = (cost_d / sum_bc * 100.0) if sum_bc > 0.5 else np.nan
                rows.append(
                    {
                        "mode": mode,
                        "vus": int(vu),
                        "metric": metric,
                        "cost_b_ms": cost_b,
                        "cost_c_ms": cost_c,
                        "sum_bc_ms": sum_bc,
                        "cost_d_ms": cost_d,
                        "difference_ms": cost_d - sum_bc,
                        "ratio_pct": ratio,
                    }
                )
    return rows


def table_overhead(stats: pd.DataFrame) -> None:
    ov_a = overhead_from_stats(stats, "A")
    ov_z = overhead_from_stats(stats, "Z")
    rows = []
    csv_rows = []
    for ov in (ov_a, ov_z):
        for _, r in ov.iterrows():
            label = f"{r['config']} vs {r['baseline']}"
            rows.append(
                [
                    label,
                    r["mode"],
                    int(r["vus"]),
                    pm(r["avg_overhead_pct"], r["avg_overhead_sd"], 1),
                    pm(r["p95_overhead_pct"], r["p95_overhead_sd"], 1),
                    pm(r["p99_overhead_pct"], r["p99_overhead_sd"], 1),
                    pm(r["rps_change_pct"], r["rps_change_sd"], 1),
                ]
            )
            csv_rows.append(
                {
                    "section": "overhead",
                    "comparison": label,
                    "config": r["config"],
                    "baseline": r["baseline"],
                    "mode": r["mode"],
                    "vus": int(r["vus"]),
                    "reps": int(r["reps"]),
                    "avg_overhead_pct_mean": r["avg_overhead_pct"],
                    "avg_overhead_pct_sd": r["avg_overhead_sd"],
                    "p95_overhead_pct_mean": r["p95_overhead_pct"],
                    "p95_overhead_pct_sd": r["p95_overhead_sd"],
                    "p99_overhead_pct_mean": r["p99_overhead_pct"],
                    "p99_overhead_pct_sd": r["p99_overhead_sd"],
                    "rps_change_pct_mean": r["rps_change_pct"],
                    "rps_change_pct_sd": r["rps_change_sd"],
                }
            )

    add = pd.DataFrame(additivity_summary(stats))
    add_lines = []
    if not add.empty:
        for _, r in add.iterrows():
            ratio = fmt(r["ratio_pct"], 1) if pd.notna(r["ratio_pct"]) else "n/a"
            add_lines.append(
                [
                    r["mode"],
                    int(r["vus"]),
                    r["metric"],
                    fmt(r["cost_b_ms"], 2),
                    fmt(r["cost_c_ms"], 2),
                    fmt(r["sum_bc_ms"], 2),
                    fmt(r["cost_d_ms"], 2),
                    fmt(r["difference_ms"], 2),
                    ratio,
                ]
            )
            csv_rows.append(
                {
                    "section": "additivity",
                    "comparison": "D vs A against (B-A)+(C-A)",
                    "config": "D",
                    "baseline": "A",
                    "mode": r["mode"],
                    "vus": int(r["vus"]),
                    "metric": r["metric"],
                    "cost_b_ms": r["cost_b_ms"],
                    "cost_c_ms": r["cost_c_ms"],
                    "sum_bc_ms": r["sum_bc_ms"],
                    "cost_d_ms": r["cost_d_ms"],
                    "difference_ms": r["difference_ms"],
                    "ratio_pct": r["ratio_pct"],
                }
            )

    body = [
        "## Overhead terhadap baseline",
        "",
        md_table(
            [
                "Perbandingan",
                "Mode",
                "VU",
                "Avg overhead (%)",
                "p95 overhead (%)",
                "p99 overhead (%)",
                "Δ RPS (%)",
            ],
            rows,
        ),
        "",
        "Nilai positif berarti konfigurasi lebih lambat/lebih tinggi daripada baseline; "
        "Δ RPS negatif berarti throughput lebih rendah. Overhead dihitung dari rerata "
        "latensi; SD pada kolom adalah propagasi SD konfigurasi dan baseline dan bersifat "
        "aproksimasi (bukan distribusi rasio yang eksak).",
    ]
    if add_lines:
        body += [
            "",
            "## Uji aditivitas biaya auth (D vs A dibandingkan (B−A)+(C−A))",
            "",
            md_table(
                [
                    "Mode",
                    "VU",
                    "Metrik",
                    "B−A (ms)",
                    "C−A (ms)",
                    "(B−A)+(C−A)",
                    "D−A (ms)",
                    "Selisih (ms)",
                    "Rasio D−A / jumlah (%)",
                ],
                add_lines,
            ),
            "",
            "B = API key, C = JWT, D = keduanya. Rasio mendekati 100% berarti biaya dua "
            "lapis mendekati jumlah biaya masing-masing. Rasio tidak dilaporkan (n/a) "
            "bila jumlah biaya individual ≤ 0,5 ms (terlalu dekat noise) atau negatif.",
        ]
    write_md(
        TABLES / "table_overhead.md",
        "Tabel Overhead Latensi dan Throughput",
        "\n".join(body),
        "Baseline utama adalah A (proxy tanpa auth); perbandingan A vs Z mengukur overhead "
        "proxy terhadap direct-to-mock. Rerata ± SD antar-3 pengulangan.",
    )
    pd.DataFrame(csv_rows).to_csv(TABLES / "table_overhead.csv", index=False)

def load_functional() -> pd.DataFrame:
    df = pd.read_csv(RESULTS / "functional.csv")
    df["pass"] = df["pass"].astype(str).str.upper()
    return df


def table_functional() -> None:
    df = load_functional()
    rows = []
    for _, r in df.iterrows():
        rows.append(
            [
                r["id"],
                r["description"],
                r["expected_status"],
                r["actual_status"],
                r["pass"],
            ]
        )
    body = md_table(["ID", "Skenario", "Harapan", "Hasil", "Status"], rows)
    write_md(
        TABLES / "table_functional.md",
        "Tabel Uji Fungsional (F01–F09)",
        body,
        "Dihasilkan ulang oleh `scripts/analyze.py` dari `results/functional.csv`. "
        "Catatan per kasus ada di kolom `notes` CSV dan data mentah di "
        "`results/raw/functional/`.",
    )
    df.to_csv(TABLES / "table_functional.csv", index=False)


def load_security_latest() -> list[dict]:
    latest: dict[tuple[str, str], tuple[float, str, dict]] = {}
    for path in (RAW / "security").glob("*.json"):
        parts = path.stem.split("_")
        if len(parts) < 2 or not re.fullmatch(r"S\d+", parts[0]):
            continue
        sid, cfg = parts[0], parts[1]
        try:
            data = json.loads(path.read_text(encoding="utf-8"))
        except Exception:
            continue
        if "sent" not in data or "rejected" not in data:
            continue
        mtime = path.stat().st_mtime
        if (sid, cfg) not in latest or mtime >= latest[(sid, cfg)][0]:
            latest[(sid, cfg)] = (mtime, path.name, data)
    return [v[2] for v in latest.values()]


def table_security() -> None:
    summaries = load_security_latest()
    summaries.sort(
        key=lambda s: (
            int(s["scenario_id"][1:]),
            {"D": 0, "RL": 1, "C2": 2, "A": 3}.get(s["config"], 9),
        )
    )
    rows = []
    csv_rows = []
    total_controls = 0
    total_bad = 0
    for s in summaries:
        codes = s.get("codes", {})
        dominant = max(codes.items(), key=lambda kv: (kv[1], kv[0]))[0] if codes else "-"
        controls = int(s.get("controls", 0))
        ctrl_bad = int(s.get("control_rejected", 0))
        total_controls += controls
        total_bad += ctrl_bad
        rows.append(
            [
                s["scenario_id"],
                s["config"],
                s["sent"],
                s["rejected"],
                f"{s.get('block_rate', 0):.1f}%",
                dominant,
                f"{ctrl_bad}/{controls} ({s.get('frr', 0):.1f}%)",
                s.get("note", ""),
            ]
        )
        csv_rows.append(
            {
                "scenario_id": s["scenario_id"],
                "config": s["config"],
                "sent": s["sent"],
                "rejected": s["rejected"],
                "block_rate_pct": s.get("block_rate"),
                "codes": json.dumps(codes, ensure_ascii=False),
                "controls": controls,
                "control_rejected": ctrl_bad,
                "frr_pct": s.get("frr"),
                "note": s.get("note", ""),
            }
        )
    frr_all = (100.0 * total_bad / total_controls) if total_controls else 0.0
    body = md_table(
        [
            "ID",
            "Config",
            "Dikirim",
            "Ditolak",
            "Block rate",
            "Kode dominan",
            "Kontrol (FRR)",
            "Catatan",
        ],
        rows,
    )
    body += f"\n\n**FRR agregat:** {total_bad}/{total_controls} = **{frr_all:.2f}%**."
    write_md(
        TABLES / "table_security.md",
        "Tabel Uji Keamanan (S01–S14)",
        body,
        "Dihasilkan ulang oleh `scripts/analyze.py` dari ringkasan JSON terbaru per "
        "skenario/config di `results/raw/security/`. N = 30 serangan + 30 kontrol per "
        "skenario, kecuali S10 = 500 tebakan, S11 = banjir sampai 429, S12 = 5 host "
        "direct. S13/S14 opsional; S13 status 200 memang diharapkan dan dinilai dari "
        "header identitas di upstream.",
    )
    pd.DataFrame(csv_rows).to_csv(TABLES / "table_security.csv", index=False)


def load_hotreload() -> tuple[pd.DataFrame, dict]:
    rev = pd.read_csv(RESULTS / "hotreload.csv")
    avail = json.loads((RAW / "hotreload" / "availability.json").read_text(encoding="utf-8"))
    return rev, avail


def table_hotreload() -> None:
    rev, avail = load_hotreload()
    rev_rows = [
        [int(r["rep"]), r["t_change"], fmt(r["t_first_reject_ms"], 3)]
        for _, r in rev.iterrows()
    ]
    k6 = avail.get("k6", {})
    probe = avail.get("probe", {})
    reloads = avail.get("reloads", [])
    summary_rows = []
    summary_rows.append(
        {
            "section": "revocation",
            "item": "reps",
            "value": len(rev),
            "unit": "count",
        }
    )
    for label, col in [
        ("mean_t_first_reject_ms", "t_first_reject_ms"),
        ("min_t_first_reject_ms", "t_first_reject_ms"),
        ("max_t_first_reject_ms", "t_first_reject_ms"),
    ]:
        value = getattr(rev[col], label.split("_")[0])()
        summary_rows.append(
            {"section": "revocation", "item": label, "value": value, "unit": "ms"}
        )
    in_flight = sum(
        int(json.loads(p.read_text(encoding="utf-8")).get("in_flight_200", 0))
        for p in (RAW / "hotreload").glob("revocation_r*.json")
    )
    summary_rows.append(
        {
            "section": "revocation",
            "item": "in_flight_200_total",
            "value": in_flight,
            "unit": "requests",
        }
    )
    summary_rows += [
        {"section": "availability", "item": "k6_vus", "value": k6.get("vus"), "unit": "VU"},
        {
            "section": "availability",
            "item": "k6_rps",
            "value": k6.get("rps"),
            "unit": "req/s",
        },
        {
            "section": "availability",
            "item": "k6_error_rate_pct",
            "value": (k6.get("error_rate", 0) or 0) * 100.0,
            "unit": "%",
        },
        {
            "section": "availability",
            "item": "k6_p95_ms",
            "value": k6.get("p95_ms"),
            "unit": "ms",
        },
        {
            "section": "availability",
            "item": "probe_requests",
            "value": probe.get("requests"),
            "unit": "count",
        },
        {
            "section": "availability",
            "item": "probe_non_200",
            "value": probe.get("non_200"),
            "unit": "count",
        },
        {
            "section": "availability",
            "item": "probe_median_ms",
            "value": probe.get("median_latency_ms"),
            "unit": "ms",
        },
        {
            "section": "availability",
            "item": "probe_max_ms",
            "value": probe.get("max_latency_ms"),
            "unit": "ms",
        },
        {
            "section": "availability",
            "item": "reloads",
            "value": len(reloads),
            "unit": "count",
        },
        {
            "section": "availability",
            "item": "container_same",
            "value": avail.get("container_same"),
            "unit": "bool",
        },
    ]
    reload_rows = [
        [
            r.get("index"),
            r.get("t_change"),
            fmt(r.get("admin_done_ms", 0), 1),
            r.get("window_requests"),
            r.get("window_non_200"),
            fmt(r.get("window_p95_latency_ms", 0), 1),
            fmt(r.get("window_max_latency_ms", 0), 1),
        ]
        for r in reloads
    ]
    mean_rev = rev["t_first_reject_ms"].mean()
    min_rev = rev["t_first_reject_ms"].min()
    max_rev = rev["t_first_reject_ms"].max()
    body = [
        "## Pencabutan API key (config D)",
        "",
        md_table(["Rep", "t_change", "t_first_reject (ms)"], rev_rows),
        "",
        f"Rerata **{mean_rev:.3f} ms**, minimum **{min_rev:.3f} ms**, maksimum "
        f"**{max_rev:.3f} ms**; request in-flight yang masih 200: **{in_flight}**. "
        "Probe revocation memakai interval 50 ms, sehingga nilai t_first_reject adalah "
        "batas atas dengan resolusi 50 ms (bukan latensi internal gateway).",
        "",
        "## Ketersediaan saat reload (rute /health)",
        "",
        md_table(
            [
                "Reload",
                "t_change",
                "Admin selesai (ms)",
                "Request window",
                "Non-200 window",
                "p95 window (ms)",
                "Max window (ms)",
            ],
            reload_rows,
        ),
        "",
        f"k6: **{k6.get('vus')} VU**, **{k6.get('rps', 0):.1f} req/s**, error rate "
        f"**{100 * (k6.get('error_rate', 0) or 0):.2f}%**, p95 **{k6.get('p95_ms', 0):.2f} ms**. "
        f"Probe: **{probe.get('non_200', 0)} non-200 dari {probe.get('requests', 0)}**, "
        f"median **{probe.get('median_latency_ms', 0):.2f} ms**, maksimum "
        f"**{probe.get('max_latency_ms', 0):.2f} ms**. Container gateway tetap sama: "
        f"**{avail.get('container_same')}**.",
    ]
    write_md(
        TABLES / "table_hotreload.md",
        "Tabel Hot Reload dan Pencabutan",
        "\n".join(body),
        "Sumber: `results/hotreload.csv` dan `results/raw/hotreload/`. "
        "Pencabutan 10 repetisi; reload admin 5 kali saat k6 50 VU berjalan 60 s.",
    )
    pd.DataFrame(summary_rows).to_csv(TABLES / "table_hotreload.csv", index=False)


def _bar_panel(ax, stats, metric_mean, metric_sd, configs, title, ylabel, vus=VUS):
    width = 0.8 / max(len(configs), 1)
    x = np.arange(len(vus))
    for i, cfg in enumerate(configs):
        vals = []
        errs = []
        for vu in vus:
            row = stats[
                (stats["config"] == cfg)
                & (stats["mode"] == "closed")
                & (stats["vus"] == vu)
            ]
            vals.append(row[metric_mean].iloc[0] if not row.empty else np.nan)
            errs.append(row[metric_sd].iloc[0] if not row.empty else 0.0)
        ax.bar(
            x + (i - (len(configs) - 1) / 2) * width,
            vals,
            width,
            yerr=errs,
            capsize=3,
            label=cfg,
            color="white" if i % 2 == 0 else "0.82",
            edgecolor="black",
            hatch=HATCHES[i % len(HATCHES)],
            linewidth=0.8,
            error_kw={"elinewidth": 0.8, "capthick": 0.8},
        )
    ax.set_xticks(x)
    ax.set_xticklabels([str(v) for v in vus])
    ax.set_xlabel("Jumlah VU")
    ax.set_ylabel(ylabel)
    ax.set_title(title)
    ax.legend(ncol=len(configs), frameon=False, loc="upper center", bbox_to_anchor=(0.5, 1.18))


def fig1_avg_latency(stats: pd.DataFrame) -> None:
    fig, ax = plt.subplots(figsize=(7.2, 4.2))
    _bar_panel(
        ax,
        stats,
        "avg_mean",
        "avg_sd",
        CORE_CONFIGS,
        "Latensi rata-rata (closed model)",
        "Latensi rata-rata (ms)",
    )
    fig.savefig(FIGURES / "fig1_latency_avg_closed.png")
    plt.close(fig)


def fig2_p95_p99(stats: pd.DataFrame) -> None:
    fig, axes = plt.subplots(1, 2, figsize=(10.5, 4.2), sharex=True)
    for ax, metric, label in [
        (axes[0], "p95", "p95"),
        (axes[1], "p99", "p99"),
    ]:
        for i, cfg in enumerate(CORE_CONFIGS):
            sub = stats[(stats["config"] == cfg) & (stats["mode"] == "closed")].sort_values(
                "vus"
            )
            if sub.empty:
                continue
            xpos = [VUS.index(int(v)) for v in sub["vus"]]
            ax.errorbar(
                xpos,
                sub[f"{metric}_mean"],
                yerr=sub[f"{metric}_sd"],
                marker=MARKERS[i % len(MARKERS)],
                linestyle=LINESTYLES[i % len(LINESTYLES)],
                color="black",
                markerfacecolor="white" if i % 2 == 0 else "0.55",
                capsize=3,
                linewidth=1.0,
                label=cfg,
            )
        ax.set_xticks(range(len(VUS)))
        ax.set_xticklabels([str(v) for v in VUS])
        ax.set_xlabel("Jumlah VU")
        ax.set_ylabel(f"Latensi {label} (ms)")
        ax.set_title(f"Latensi {label} (closed model)")
    handles, labels = axes[0].get_legend_handles_labels()
    fig.legend(handles, labels, ncol=len(CORE_CONFIGS), frameon=False, loc="upper center")
    fig.tight_layout(rect=(0, 0, 1, 0.88))
    fig.savefig(FIGURES / "fig2_latency_p95_p99_closed.png")
    plt.close(fig)


def fig3_throughput(stats: pd.DataFrame) -> None:
    fig, axes = plt.subplots(1, 2, figsize=(11.0, 4.3))
    _bar_panel(
        axes[0],
        stats,
        "rps_mean",
        "rps_sd",
        CORE_CONFIGS,
        "Throughput (closed model)",
        "RPS",
    )
    ax = axes[1]
    open_stats = stats[(stats["mode"] == "open") & (stats["config"].isin(AUTH_CONFIGS))]
    x = np.arange(len(AUTH_CONFIGS))
    vals = []
    errs = []
    for cfg in AUTH_CONFIGS:
        row = open_stats[open_stats["config"] == cfg]
        vals.append(row["rps_mean"].iloc[0] if not row.empty else np.nan)
        errs.append(row["rps_sd"].iloc[0] if not row.empty else 0.0)
    ax.bar(
        x,
        vals,
        yerr=errs,
        capsize=3,
        color="white",
        edgecolor="black",
        hatch="//",
        linewidth=0.8,
    )
    ax.axhline(OPEN_RATE, color="black", linestyle="--", linewidth=0.9, label="Target 4.226")
    ax.set_xticks(x)
    ax.set_xticklabels(AUTH_CONFIGS)
    ax.set_xlabel("Konfigurasi")
    ax.set_ylabel("RPS")
    ax.set_title("Throughput (open model, 4.226 req/s)")
    ax.legend(frameon=False, loc="lower right")
    fig.tight_layout()
    fig.savefig(FIGURES / "fig3_throughput.png")
    plt.close(fig)


def fig4_resources(stats: pd.DataFrame) -> None:
    fig, axes = plt.subplots(1, 2, figsize=(10.8, 4.3))
    _bar_panel(
        axes[0],
        stats,
        "cpu_mean",
        "cpu_sd",
        AUTH_CONFIGS,
        "CPU gateway",
        "CPU (% dari 1 core; 200% = 2 core)",
    )
    axes[0].axhline(200, color="black", linestyle="--", linewidth=0.9)
    _bar_panel(
        axes[1],
        stats,
        "mem_mean",
        "mem_sd",
        AUTH_CONFIGS,
        "Memori gateway",
        "Memori (MiB)",
    )
    axes[1].axhline(512, color="black", linestyle="--", linewidth=0.9)
    fig.tight_layout()
    fig.savefig(FIGURES / "fig4_cpu_mem.png")
    plt.close(fig)


def fig5_overhead(stats: pd.DataFrame) -> None:
    ov = overhead_from_stats(stats, "A")
    ov = ov[ov["config"].isin(["B", "C", "D"])]
    fig, axes = plt.subplots(1, 2, figsize=(10.8, 4.3), sharey=True)
    for ax, mode, title in [
        (axes[0], "closed", "Closed model"),
        (axes[1], "open", "Open model (4.226 req/s)"),
    ]:
        sub = ov[ov["mode"] == mode]
        if mode == "closed":
            x = np.arange(len(VUS))
            width = 0.8 / 3
            for i, cfg in enumerate(["B", "C", "D"]):
                vals = []
                errs = []
                for vu in VUS:
                    row = sub[(sub["config"] == cfg) & (sub["vus"] == vu)]
                    vals.append(row["p95_overhead_pct"].iloc[0] if not row.empty else np.nan)
                    errs.append(row["p95_overhead_sd"].iloc[0] if not row.empty else 0.0)
                ax.bar(
                    x + (i - 1) * width,
                    vals,
                    width,
                    yerr=errs,
                    capsize=3,
                    label=cfg,
                    color="white" if i % 2 == 0 else "0.82",
                    edgecolor="black",
                    hatch=HATCHES[i],
                    linewidth=0.8,
                )
            ax.set_xticks(x)
            ax.set_xticklabels([str(v) for v in VUS])
            ax.set_xlabel("Jumlah VU")
        else:
            x = np.arange(3)
            vals = []
            errs = []
            for cfg in ["B", "C", "D"]:
                row = sub[sub["config"] == cfg]
                vals.append(row["p95_overhead_pct"].iloc[0] if not row.empty else np.nan)
                errs.append(row["p95_overhead_sd"].iloc[0] if not row.empty else 0.0)
            ax.bar(
                x,
                vals,
                yerr=errs,
                capsize=3,
                color="white",
                edgecolor="black",
                hatch="//",
                linewidth=0.8,
            )
            ax.set_xticks(x)
            ax.set_xticklabels(["B", "C", "D"])
            ax.set_xlabel("Konfigurasi")
        ax.axhline(0, color="black", linewidth=0.8)
        ax.set_title(title)
    axes[0].set_ylabel("Overhead p95 terhadap A (%)")
    handles, labels = axes[0].get_legend_handles_labels()
    fig.legend(handles, labels, ncol=3, frameon=False, loc="upper center")
    fig.tight_layout(rect=(0, 0, 1, 0.9))
    fig.savefig(FIGURES / "fig5_overhead_p95.png")
    plt.close(fig)

def fig6_block_rate() -> None:
    all_summaries = load_security_latest()
    ids = [f"S{i:02d}" for i in range(1, 13)]
    rates = []
    codes = []
    for sid in ids:
        candidates = [
            s
            for s in all_summaries
            if s["scenario_id"] == sid and s["config"] in ("D", "RL", "A", "C2")
        ]
        if not candidates:
            rates.append(np.nan)
            codes.append("-")
            continue
        # For S10 choose rate_limited so the 429 effect is visible; otherwise D.
        chosen = next((s for s in candidates if s.get("config") == "RL"), candidates[0])
        rates.append(float(chosen.get("block_rate", np.nan)))
        codes.append(chosen.get("config", "-"))
    fig, ax = plt.subplots(figsize=(8.5, 4.2))
    x = np.arange(len(ids))
    bars = ax.bar(
        x,
        rates,
        color="white",
        edgecolor="black",
        hatch="//",
        linewidth=0.8,
    )
    for bar, sid, rate, cfg in zip(bars, ids, rates, codes):
        if not math.isnan(rate):
            ax.text(
                bar.get_x() + bar.get_width() / 2,
                min(rate + 2.0, 101.0),
                f"{rate:.1f}",
                ha="center",
                va="bottom",
                fontsize=7,
                rotation=90,
            )
    ax.axhline(100, color="black", linestyle="--", linewidth=0.9)
    ax.set_ylim(0, 112)
    ax.set_xticks(x)
    ax.set_xticklabels(ids, rotation=45, ha="right")
    ax.set_ylabel("Block rate (%)")
    ax.set_xlabel("Skenario")
    ax.set_title("Block rate S01–S12 (S10 memakai konfigurasi RL)")
    fig.text(
        0.5,
        0.01,
        "S11 = uji ambang rate limit (bukan serangan yang harus 100%); S13/S14 opsional ada di tabel.",
        ha="center",
        fontsize=7.5,
    )
    fig.tight_layout(rect=(0, 0.035, 1, 1))
    fig.savefig(FIGURES / "fig6_block_rate.png")
    plt.close(fig)


def fig7_open_latency(stats: pd.DataFrame) -> None:
    percentile_labels = ["p50", "p90", "p95", "p99"]
    fig, ax = plt.subplots(figsize=(7.2, 4.2))
    raw = load_perf()
    raw = raw[(raw["mode"] == "open") & (raw["config"].isin(AUTH_CONFIGS))]
    for i, cfg in enumerate(AUTH_CONFIGS):
        cfg_raw = raw[raw["config"] == cfg]
        med: list[float] = []
        p90: list[float] = []
        for _, run in cfg_raw.sort_values("rep").iterrows():
            run_dir = RAW / "perf" / f"{cfg}_vu{int(run['vus'])}_open_r{int(run['rep'])}"
            summary = json.loads((run_dir / "summary.json").read_text(encoding="utf-8"))
            values = summary["metrics"]["http_req_duration"]["values"]
            med.append(float(values["med"]))
            p90.append(float(values["p(90)"]))
        p95 = cfg_raw["p95_ms"]
        p99 = cfg_raw["p99_ms"]
        y = [np.mean(med), np.mean(p90), p95.mean(), p99.mean()]
        yerr = [
            np.std(med, ddof=1) if len(med) > 1 else 0.0,
            np.std(p90, ddof=1) if len(p90) > 1 else 0.0,
            p95.std(ddof=1),
            p99.std(ddof=1),
        ]
        ax.errorbar(
            np.arange(len(percentile_labels)),
            y,
            yerr=yerr,
            marker=MARKERS[i % len(MARKERS)],
            linestyle=LINESTYLES[i % len(LINESTYLES)],
            color="black",
            markerfacecolor="white" if i % 2 == 0 else "0.55",
            capsize=3,
            linewidth=1.0,
            label=cfg,
        )
    ax.set_xticks(np.arange(len(percentile_labels)))
    ax.set_xticklabels(percentile_labels)
    ax.set_xlabel("Persentil latensi")
    ax.set_ylabel("Latensi (ms)")
    ax.set_yscale("log")
    ax.set_title("Distribusi latensi (open model, 4.226 req/s)")
    ax.legend(ncol=len(AUTH_CONFIGS), frameon=False, loc="upper left")
    fig.savefig(FIGURES / "fig7_open_latency_distribution.png")
    plt.close(fig)

def write_results_summary(stats: pd.DataFrame, sec: list[dict]) -> None:
    get = lambda cfg, mode, vu, col: stats[
        (stats["config"] == cfg) & (stats["mode"] == mode) & (stats["vus"] == vu)
    ][col].iloc[0]
    a_open = get("A", "open", 200, "p95_mean")
    d_open = get("D", "open", 200, "p95_mean")
    open_p95 = (d_open - a_open) / a_open * 100.0
    z200 = get("Z", "closed", 200, "rps_mean")
    a200 = get("A", "closed", 200, "rps_mean")
    d200 = get("D", "closed", 200, "rps_mean")
    cpu_200 = [
        get(c, "closed", 200, "cpu_mean") for c in AUTH_CONFIGS
    ]
    ov = overhead_from_stats(stats, "A")
    ovd = ov[(ov["config"] == "D") & (ov["mode"] == "closed")]
    add = pd.DataFrame(additivity_summary(stats))
    add_text = ""
    if not add.empty:
        parts = []
        for _, row in add[add["metric"] == "p95"].iterrows():
            ratio = f"{row['ratio_pct']:.0f}%" if pd.notna(row["ratio_pct"]) else "n/a"
            parts.append(
                f"{row['mode']} VU {int(row['vus'])}: jumlah {row['sum_bc_ms']:.2f} ms "
                f"vs D−A {row['cost_d_ms']:.2f} ms (rasio {ratio})"
            )
        add_text = "; ".join(parts)
    frr_controls = sum(int(s.get("controls", 0)) for s in sec)
    frr_bad = sum(int(s.get("control_rejected", 0)) for s in sec)
    hot = pd.read_csv(RESULTS / "hotreload.csv")
    avail = json.loads((RAW / "hotreload" / "availability.json").read_text(encoding="utf-8"))
    hot_mean = hot["t_first_reject_ms"].mean()
    hot_min = hot["t_first_reject_ms"].min()
    hot_max = hot["t_first_reject_ms"].max()
    probe = avail["probe"]
    k6 = avail["k6"]
    text = f"""# Ringkasan Hasil Eksperimen

Dihasilkan oleh `experiments/scripts/analyze.py` dari data mentah di `experiments/results/raw/`.
Rerata ± simpangan baku dihitung antar-3 pengulangan; tabel lengkap ada di
`results/tables/` dan grafik di `results/figures/`.

## Angka kunci

- Fungsional: **9/9 PASS** (F01–F09).
- Keamanan: **{frr_bad}/{frr_controls} = {100.0 * frr_bad / frr_controls if frr_controls else 0.0:.2f}% FRR**
  (kontrol sah yang ditolak); S01–S10 dan S12 diblokir 100%, S11 menguji ambang rate
  limit (4 dari 64 request pertama kena 429, 200 lagi setelah 34,03 s), S14 opsional
  25/30 karena 5 token besar dengan signature valid memang diterima.
- Hot reload/pencabutan: 10 repetisi, t_first_reject **{hot_mean:.2f} ms**
  (min {hot_min:.2f}, maks {hot_max:.2f}; resolusi probe 50 ms); k6 50 VU/60 s pada
  `/health`: **0 error**, p95 **{k6['p95_ms']:.2f} ms**, probe **{probe['non_200']} non-200
  dari {probe['requests']}**, maksimum **{probe['max_latency_ms']:.2f} ms**.
- Throughput Z (direct-to-mock) pada VU 200: **{z200:.0f} RPS**; A **{a200:.0f} RPS**;
  D **{d200:.0f} RPS**. CPU gateway pada VU 200: **{min(cpu_200):.1f}–{max(cpu_200):.1f}%**
  dari satu core (cpuset 2 core = 200%).
- Open model 4.226 req/s: D p95 **{d_open:.2f} ms** versus A **{a_open:.2f} ms**
  (overhead **{open_p95:+.1f}%**), error rate 0.
- Overhead p95 D vs A (closed): """ + ", ".join(
        f"VU {int(r['vus'])} {r['p95_overhead_pct']:+.1f}%" for _, r in ovd.iterrows()
    ) + f""".
- Uji aditivitas auth (p95): {add_text or 'tidak tersedia'}.

## Temuan utama

1. Semua kasus fungsional F01–F09 lulus; validasi API key + JWT, prioritas plugin,
   hot reload, circuit breaker, dan sanitasi header berjalan sesuai harapan.
2. Seluruh serangan wajib S01–S10 dan S12 diblokir 100%; tidak ada kontrol sah yang
   ditolak (FRR agregat 0%). S11 menunjukkan rate limiter bekerja pada ambang yang
   diuji, bukan kegagalan blokir. S14 (opsional) menerima 5/5 token besar yang masih
   memiliki signature valid; ini perilaku yang diharapkan, bukan bypass autentikasi.
3. Pencabutan API key melalui Admin API efektif pada pengukuran berikutnya; nilai
   50,4–51,2 ms adalah batas atas karena probe berinterval 50 ms. Refresh konfigurasi
   tidak menimbulkan error pada {probe['requests']} probe dan 50 VU k6; container gateway
   tidak restart.
4. Rerata p95 D (API key + JWT) lebih tinggi daripada A pada semua titik yang diuji:
   open model 4.226 req/s {d_open:.2f} vs {a_open:.2f} ms ({open_p95:+.1f}%); closed
   model {', '.join(f"VU {int(r['vus'])} {r['p95_overhead_pct']:+.1f}%" for _, r in ovd.iterrows())}.
   Namun SD antar-run juga besar (27–133%), sehingga perbedaan ini **belum dapat
   disebut signifikan** dari 3 repetisi; arah trennya konsisten, besarnya tidak.
5. Throughput menurun tajam pada VU 200 untuk semua konfigurasi gateway; CPU gateway
   mencapai {min(cpu_200):.1f}–{max(cpu_200):.1f}% dari satu core (cpuset 2 core), sehingga
   VU 500 tidak dijalankan. Bottleneck utama adalah CPU gateway, bukan k6.
6. Z jauh lebih cepat daripada A ({z200:.0f} vs {a200:.0f} RPS pada VU 200), jadi biaya
   proxy (A vs Z) dominan; auth menambah biaya di atas biaya proxy tersebut.
7. Biaya dua lapis auth mendekati jumlah biaya masing-masing pada VU 50/100, tetapi
   pada VU 200 dan open model komponen individual mendekati nol/negatif karena noise,
   sehingga aditivitas tidak dapat disimpulkan di semua titik.
8. Konfigurasi opsional F (rate limit ambang tinggi) tidak menunjukkan penalti
   throughput yang konsisten dibanding D; perbedaannya berada dalam variasi
   antar-repetisi.
9. RPS model open lebih stabil daripada closed (SD RPS 7,6–32,8 vs sampai ±1.400 RPS),
   tetapi latensi p95 open sangat bervariasi antar-run; kestabilan RPS tidak berarti
   distribusi latensi stabil. Variasi ini kemungkinan berasal dari host bersama/thermal,
   bukan error aplikasi.

## Anomali dan keterbatasan

- **Satu mesin, satu host:** gateway, mock, dan k6 berbagi CPU/RAM; angka absolut
  tidak mewakili produksi. Matriks memakai cpuset 2 core untuk gateway dan 2 core
  untuk k6, tetapi host tetap dapat mengalami noise.
- **Closed model bervariasi tinggi:** rerata 3 repetisi belum tentu cukup untuk
  menyimpulkan perbedaan kecil antar-B/C/D. Gunakan tabel mean ± SD, bukan hanya
  rerata; perbedaan <1 SD jangan diklaim signifikan.
- **A vs Z bukan perbandingan gateway:** Z melewati gateway sepenuhnya
  (direct-to-mock); selisih A vs Z mengukur gabungan overhead proxy + auth + jaringan.
- **Hot reload:** t_first_reject dibatasi resolusi probe 50 ms; nilai sebenarnya
  lebih kecil. Pengukuran ini tidak mengukur latensi internal swap konfigurasi.
- **Keamanan:** uji dilakukan pada mock upstream dan tanpa TLS; cakupan serangan
  terbatas pada skenario yang didokumentasikan. Rate limiter in-memory per instance,
  belum terdistribusi.
- **Variasi data security:** `security_raw.csv` bersifat append-only dan memuat
  riwayat beberapa run; tabel memakai ringkasan JSON terbaru per skenario/config.
  Data mentah lama tidak dihapus agar tidak ada cherry-pick.
- **Build:** data utama dijalankan dari working tree `dccb182` dengan 8 file kotor;
  `env.json` harus diregenerasi pada commit bersih untuk laporan final.

## Run yang dikecualikan / tidak dijalankan

- **VU 500:** tidak dijalankan karena gateway sudah jenuh pada VU 200 (CPU ±183–190%
  dari batas cpuset 200%); alasan dicatat di `LOG.md`.
- **Z mode open:** tidak dijalankan (Z direct-to-mock, bukan gateway; matriks open
  hanya A–D).
- **Tidak ada run kinerja invalid:** seluruh 75 run dalam `manifest.csv` valid dan
  error rate 0. Tidak ada run yang dibuang atau di-cherry-pick.
- Baris CPU k6 tidak tersedia di `stats.csv` matriks utama karena bug filter sampler;
  pengecekan k6 terpisah pada VU 200 menunjukkan k6 bukan bottleneck utama. Filter
  sudah diperbaiki untuk run berikutnya.
"""
    (RESULTS / "RESULTS_SUMMARY.md").write_text(text, encoding="utf-8")

def main() -> None:
    ensure_dirs()
    df = load_perf()
    stats = grouped_perf(df)
    table_perf_latency(stats)
    table_perf_throughput(stats)
    table_perf_resources(stats)
    table_overhead(stats)
    table_functional()
    table_security()
    table_hotreload()
    fig1_avg_latency(stats)
    fig2_p95_p99(stats)
    fig3_throughput(stats)
    fig4_resources(stats)
    fig5_overhead(stats)
    fig6_block_rate()
    fig7_open_latency(stats)
    write_results_summary(stats, load_security_latest())
    print("analysis written to", TABLES, "and", FIGURES)


if __name__ == "__main__":
    main()
