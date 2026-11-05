import http from "k6/http";
import { check } from "k6";
import { Counter, Trend, Rate } from "k6/metrics";
import exec from "k6/execution";

// =====================================================================
// KONFIGURASI (override lewat env: -e NAMA=nilai)
// Contoh rapat di sekitar titik jenuh:
//   k6 run -e RATES=2000,2250,2500,2750,3000 k6-breaking-point.js
// Contoh + soak test 30 menit di 1600 RPS (80% dari kapasitas):
//   k6 run -e SOAK_RATE=1600 -e SOAK_MIN=30 k6-breaking-point.js
// Butuh k6 >= v0.43 (sub-metric thresholds & exec.instance.vusActive).
// =====================================================================
const BASE_URL = __ENV.TARGET_URL || "http://localhost:8080";
const PATH = __ENV.TARGET_PATH || "/mock";

// Optional Bearer token (mis. saat plugin auth aktif di gateway).
// Set nilainya via: -e AUTH_TOKEN=<jwt>
const AUTH_TOKEN = __ENV.AUTH_TOKEN || "";

const RATES = (__ENV.RATES || "500,1000,2000,3000,5000,7000,10000,15000")
  .split(",")
  .map(Number);
const RAMP_S = Number(__ENV.RAMP_S || 15); // naik ke step berikutnya
const HOLD_S = Number(__ENV.HOLD_S || 45); // tahan (fase yang diukur)

// Cool-down ANTAR step: turun ke COOL_RATE supaya backlog habis sebelum
// step berikutnya. SETTLE_S = jeda pengurasan (tidak diukur),
// COOL_S = jendela pengukuran pemulihan (recovery). COOL_S=0 untuk mematikan.
const COOL_RATE = Number(__ENV.COOL_RATE || 100);
const SETTLE_S = Number(__ENV.SETTLE_S || 5);
const COOL_S = Number(__ENV.COOL_S || 30);

// Pool VU. Kebutuhan VU ~= RPS x latency(detik).
const PRE_VUS = Number(__ENV.PRE_VUS || 5000);
const MAX_VUS = Number(__ENV.MAX_VUS || 20000);

// Soak test opsional (aktif jika SOAK_RATE > 0), jalan setelah breaking test
const SOAK_RATE = Number(__ENV.SOAK_RATE || 0);
const SOAK_MIN = Number(__ENV.SOAK_MIN || 30);

const WARMUP_S = 30;
const SOAK_GAP_S = 30; // jeda sebelum soak agar in-flight request selesai
const MAX_DROPPED = Number(__ENV.MAX_DROPPED || 0);

// SLO: sebuah step LULUS jika semua terpenuhi
const SLO = {
  successRate: Number(__ENV.SLO_SUCCESS || 0.99),
  p95: Number(__ENV.SLO_P95_MS || 500),
  p99: Number(__ENV.SLO_P99_MS || 1000),
  minAchieved: Number(__ENV.SLO_MIN_ACHIEVED || 0.95),
};

// Hanya status 200 dianggap sukses -> http_req_failed selaras dengan success_rate
http.setResponseCallback(http.expectedStatuses(200));

// =====================================================================
// CUSTOM METRICS (semua ditag per "step")
// =====================================================================
const latencySuccess = new Trend("latency_success", true); // hanya HTTP 200
const latencyFail = new Trend("latency_fail", true); // hanya yang gagal
const latencyAll = new Trend("latency_all", true); // semua request
const successRate = new Rate("success_rate");
const stepReqs = new Counter("step_reqs");
const vusActive = new Trend("vus_active");

const errTimeout = new Counter("err_timeout");
const errConn = new Counter("err_connection");

// Distribusi status code. Kode di luar daftar masuk ke "other".
const ERR_KEYS = ["429", "500", "502", "503", "504", "other"];
const STATUS_KEYS = ["200"].concat(ERR_KEYS);
const statusCounters = {};
for (const c of STATUS_KEYS) statusCounters[c] = new Counter(`status_${c}`);

// =====================================================================
// TIMELINE: stage arrival-rate + label step berdasarkan waktu
// =====================================================================
const stages = [];
const timeline = [];
let t = 0;
function addStage(durationS, target, label) {
  stages.push({ duration: `${durationS}s`, target: target });
  timeline.push({ from: t, to: t + durationS * 1000, label: label });
  t += durationS * 1000;
}

RATES.forEach((rate, i) => {
  if (i > 0) addStage(RAMP_S, rate, "ramp");
  addStage(HOLD_S, rate, `rps_${rate}`);
  if (COOL_S > 0) {
    addStage(SETTLE_S, COOL_RATE, "ramp");
    addStage(COOL_S, COOL_RATE, `cool_${rate}`);
  }
});
const BREAKING_TOTAL_S = Math.ceil(t / 1000);

function currentStep() {
  const elapsed = Date.now() - exec.scenario.startTime;
  for (const s of timeline) {
    if (elapsed >= s.from && elapsed < s.to) return s.label;
  }
  return "ramp";
}

// =====================================================================
// THRESHOLDS per step. abortOnFail=false -> semua step tetap terukur.
// Threshold "selalu lulus" hanya agar sub-metric muncul di handleSummary.
// =====================================================================
const thresholds = {};

function sloThresholds(k) {
  thresholds[`success_rate{step:${k}}`] = [
    { threshold: `rate>${SLO.successRate}`, abortOnFail: false },
  ];
  thresholds[`http_req_failed{step:${k}}`] = [
    { threshold: `rate<${(1 - SLO.successRate).toFixed(4)}`, abortOnFail: false },
  ];
  thresholds[`latency_success{step:${k}}`] = [
    { threshold: `p(95)<${SLO.p95}`, abortOnFail: false },
    { threshold: `p(99)<${SLO.p99}`, abortOnFail: false },
  ];
  thresholds[`step_reqs{step:${k}}`] = ["count>=0"];
  thresholds[`vus_active{step:${k}}`] = ["max>=0"];
}

for (const r of RATES) {
  const k = `rps_${r}`;
  sloThresholds(k);
  thresholds[`latency_all{step:${k}}`] = ["p(95)>=0"];
  thresholds[`latency_fail{step:${k}}`] = ["p(95)>=0"];
  thresholds[`err_timeout{step:${k}}`] = ["count>=0"];
  thresholds[`err_connection{step:${k}}`] = ["count>=0"];
  for (const c of ERR_KEYS) thresholds[`status_${c}{step:${k}}`] = ["count>=0"];

  if (COOL_S > 0) {
    const c = `cool_${r}`;
    thresholds[`success_rate{step:${c}}`] = ["rate>=0"];
    thresholds[`latency_success{step:${c}}`] = ["p(95)>=0"];
    thresholds[`step_reqs{step:${c}}`] = ["count>=0"];
  }
}
if (SOAK_RATE > 0) sloThresholds("soak");

// Iterasi di-drop = generator beban kehabisan VU -> beban nominal tidak tercapai
thresholds["dropped_iterations"] = [
  { threshold: `count<=${MAX_DROPPED}`, abortOnFail: false },
];

// =====================================================================
// SCENARIOS
// =====================================================================
const scenarios = {
  warmup: {
    executor: "ramping-vus",
    exec: "warmupTest",
    startVUs: 0,
    stages: [
      { duration: "10s", target: 100 },
      { duration: "20s", target: 100 },
    ],
    gracefulRampDown: "5s",
  },
  breaking_point: {
    executor: "ramping-arrival-rate",
    exec: "breakingTest",
    startRate: RATES[0],
    timeUnit: "1s",
    preAllocatedVUs: PRE_VUS,
    maxVUs: MAX_VUS,
    stages: stages,
    startTime: `${WARMUP_S}s`,
  },
};

if (SOAK_RATE > 0) {
  scenarios.soak = {
    executor: "constant-arrival-rate",
    exec: "soakTest",
    rate: SOAK_RATE,
    timeUnit: "1s",
    duration: `${SOAK_MIN}m`,
    preAllocatedVUs: Math.max(100, Math.ceil(SOAK_RATE * 0.2)),
    maxVUs: MAX_VUS,
    startTime: `${WARMUP_S + BREAKING_TOTAL_S + SOAK_GAP_S}s`,
  };
}

export const options = {
  summaryTrendStats: ["avg", "min", "med", "p(90)", "p(95)", "p(99)", "max"],
  scenarios: scenarios,
  thresholds: thresholds,
};

// =====================================================================
// REQUEST
// =====================================================================
function doRequest(step) {
  const headers = { "Content-Type": "application/json" };
  if (AUTH_TOKEN) headers.Authorization = `Bearer ${AUTH_TOKEN}`;
  const res = http.get(`${BASE_URL}${PATH}`, {
    headers: headers,
    timeout: "15s",
    tags: { step: step },
  });

  const tags = { step: step };
  const ok = res.status === 200;
  const dur = res.timings.duration;

  successRate.add(ok, tags);
  stepReqs.add(1, tags);
  vusActive.add(exec.instance.vusActive, tags);
  latencyAll.add(dur, tags); // termasuk request gagal (anti survivorship bias)

  // Distribusi status code per step (status 0 = timeout/koneksi, ditangani di bawah)
  if (res.status !== 0) {
    let key = String(res.status);
    if (!statusCounters[key]) key = "other";
    statusCounters[key].add(1, tags);
  }

  if (ok) {
    latencySuccess.add(dur, tags);
    check(
      res,
      {
        "ok: latency < 100ms": (r) => r.timings.duration < 100,
        "ok: latency < 500ms": (r) => r.timings.duration < 500,
        "ok: latency < 1s": (r) => r.timings.duration < 1000,
        "ok: latency < 3s": (r) => r.timings.duration < 3000,
      },
      tags
    );
  } else {
    latencyFail.add(dur, tags);
    if (res.status === 0) {
      if (res.error_code === 1050) errTimeout.add(1, tags);
      else errConn.add(1, tags);
    }
  }
}

export function warmupTest() {
  doRequest("warmup");
}

export function breakingTest() {
  doRequest(currentStep());
}

export function soakTest() {
  doRequest("soak");
}

// =====================================================================
// SUMMARY
// =====================================================================
function val(m, name, key) {
  const x = m[name];
  if (!x || !x.values || x.values[key] === undefined) return null;
  return x.values[key];
}
function cnt(m, name) {
  const v = val(m, name, "count");
  return v === null ? 0 : v;
}
const f = (x, d) => (x === null || x === undefined ? "-" : x.toFixed(d || 0));
const pad = (s, n) => String(s).padStart(n, " ");

function evaluate(sr, p95, p99, achieved, target) {
  const reasons = [];
  if (sr === null || sr < SLO.successRate) reasons.push("success rate");
  if (p95 === null || p95 > SLO.p95) reasons.push("p95");
  if (p99 === null || p99 > SLO.p99) reasons.push("p99");
  if (achieved !== undefined && (achieved === null || achieved < target * SLO.minAchieved))
    reasons.push("RPS tidak tercapai");
  return reasons;
}

export function handleSummary(data) {
  // Lokasi file hasil. Path relatif disimpan ke CWD proses k6:
  //   - k6 di host            -> muncul di working dir saat k6 dijalankan
  //   - k6 di Docker (make k6-test) -> working_dir compose = /workdir, yang
  //     di-mount ke ROOT repo, sehingga kedua file ini muncul di working
  //     dir host (bukan tertinggal di dalam container).
  //   - Override: -e SUMMARY_PATH=/path.json -e BREAKING_PATH=/path.json
  const summaryPath = __ENV.SUMMARY_PATH || "summary.json";
  const breakingPath = __ENV.BREAKING_PATH || "breaking_point.json";

  const m = data.metrics;
  const rows = [];

  for (const rate of RATES) {
    const k = `rps_${rate}`;
    const sr = val(m, `success_rate{step:${k}}`, "rate");
    const fails = val(m, `success_rate{step:${k}}`, "fails") || 0;
    const med = val(m, `latency_success{step:${k}}`, "med");
    const p95 = val(m, `latency_success{step:${k}}`, "p(95)");
    const p99 = val(m, `latency_success{step:${k}}`, "p(99)");
    const allP95 = val(m, `latency_all{step:${k}}`, "p(95)");
    const failMed = fails > 0 ? val(m, `latency_fail{step:${k}}`, "med") : null;
    const reqs = val(m, `step_reqs{step:${k}}`, "count");
    const vusMax = val(m, `vus_active{step:${k}}`, "max");
    const achieved = reqs === null ? null : reqs / HOLD_S;

    const errs = { timeout: cnt(m, `err_timeout{step:${k}}`), conn: cnt(m, `err_connection{step:${k}}`) };
    for (const c of ERR_KEYS) errs[c] = cnt(m, `status_${c}{step:${k}}`);

    // Recovery: setelah step ini, apakah sistem pulih di COOL_RATE?
    let recovered = null;
    let coolSr = null;
    let coolP95 = null;
    if (COOL_S > 0) {
      const c = `cool_${rate}`;
      coolSr = val(m, `success_rate{step:${c}}`, "rate");
      coolP95 = val(m, `latency_success{step:${c}}`, "p(95)");
      const creqs = val(m, `step_reqs{step:${c}}`, "count");
      recovered =
        creqs > 0 && coolSr !== null && coolSr >= SLO.successRate &&
        coolP95 !== null && coolP95 <= SLO.p95;
    }

    const reasons = evaluate(sr, p95, p99, achieved, rate);
    rows.push({
      rate, sr, med, p95, p99, allP95, failMed, achieved, vusMax,
      errs, recovered, coolSr, coolP95, pass: reasons.length === 0, reasons,
    });
  }

  const firstFail = rows.findIndex((r) => !r.pass);
  const breaking = firstFail >= 0 ? rows[firstFail] : null;
  const lastStable =
    firstFail < 0 ? rows[rows.length - 1] : firstFail > 0 ? rows[firstFail - 1] : null;
  const passAfterBreak = firstFail >= 0 ? rows.slice(firstFail + 1).filter((r) => r.pass) : [];

  let out = "\n=== HASIL BREAKING POINT (per step, fase HOLD saja) ===\n";
  out += `Output : summary -> ${summaryPath}\n         breaking_point -> ${breakingPath}\n`;
  out += `SLO: success >= ${(SLO.successRate * 100).toFixed(1)}% | p95 <= ${SLO.p95}ms | p99 <= ${SLO.p99}ms | RPS tercapai >= ${SLO.minAchieved * 100}% target\n`;
  out += "p50/p95/p99 = hanya request sukses | p95all = semua request (termasuk gagal)\n\n";
  out +=
    pad("Target", 8) + pad("Real RPS", 10) + pad("Success%", 10) +
    pad("p50 ms", 9) + pad("p95 ms", 9) + pad("p99 ms", 9) + pad("p95all", 9) +
    pad("VU max", 8) + pad("Recover", 9) + "  Status\n";
  out += "-".repeat(95) + "\n";
  for (const r of rows) {
    out +=
      pad(r.rate, 8) + pad(f(r.achieved, 0), 10) +
      pad(r.sr === null ? "-" : f(r.sr * 100, 2), 10) +
      pad(f(r.med, 0), 9) + pad(f(r.p95, 0), 9) + pad(f(r.p99, 0), 9) +
      pad(f(r.allP95, 0), 9) + pad(f(r.vusMax, 0), 8) +
      pad(r.recovered === null ? "-" : r.recovered ? "ya" : "TIDAK", 9) + "  " +
      (r.pass ? "LULUS" : "GAGAL (" + r.reasons.join(", ") + ")") + "\n";
  }

  out += "\n=== DISTRIBUSI ERROR PER STEP (jumlah request) ===\n";
  out +=
    pad("Target", 8) + pad("429", 8) + pad("500", 8) + pad("502", 8) + pad("503", 8) +
    pad("504", 8) + pad("timeout", 9) + pad("conn", 7) + pad("other", 8) +
    pad("fail p50ms", 12) + "\n";
  out += "-".repeat(84) + "\n";
  for (const r of rows) {
    const e = r.errs;
    out +=
      pad(r.rate, 8) + pad(e["429"], 8) + pad(e["500"], 8) + pad(e["502"], 8) +
      pad(e["503"], 8) + pad(e["504"], 8) + pad(e.timeout, 9) + pad(e.conn, 7) +
      pad(e.other, 8) + pad(f(r.failMed, 0), 12) + "\n";
  }
  out += "(fail p50 kecil = fail-fast / load shedding; besar = request menumpuk lalu timeout)\n";

  out += "\n--- KESIMPULAN ---\n";
  if (lastStable) {
    out +=
      `Kapasitas stabil terakhir : target ${lastStable.rate} RPS (real ${f(lastStable.achieved, 0)} RPS)\n` +
      `  VU aktif (max)          : ${f(lastStable.vusMax, 0)}\n` +
      `  Latency sukses          : p50=${f(lastStable.med, 0)}ms p95=${f(lastStable.p95, 0)}ms p99=${f(lastStable.p99, 0)}ms\n`;
  } else {
    out += "Tidak ada step yang lulus SLO. Turunkan RATES awal.\n";
  }
  if (breaking) {
    out +=
      `BREAKING POINT            : target ${breaking.rate} RPS (real ${f(breaking.achieved, 0)} RPS)\n` +
      `  VU aktif (max)          : ${f(breaking.vusMax, 0)}\n` +
      `  Penyebab                : ${breaking.reasons.join(", ")}\n`;
  } else {
    out += "Semua step lulus SLO. Naikkan RATES untuk menemukan breaking point.\n";
  }

  if (passAfterBreak.length > 0) {
    out +=
      `\nPERINGATAN (non-monotonik): step ${passAfterBreak.map((r) => r.rate).join(", ")} RPS lulus SLO ` +
      "SETELAH breaking point. Curigai load shedding/rate limiter, autoscaling terlambat, atau beban nominal tidak tercapai (cek Real RPS & dropped iterations).\n";
  }
  const notRecovered = rows.filter((r) => r.recovered === false);
  if (notRecovered.length > 0) {
    out +=
      `\nPERINGATAN (recovery): setelah step ${notRecovered.map((r) => r.rate).join(", ")} RPS sistem TIDAK pulih ` +
      `pada ${COOL_RATE} RPS. Step berikutnya bisa terkontaminasi sisa backlog.\n`;
  }

  const dropped = val(m, "dropped_iterations", "count");
  if (dropped) {
    out += `\nPERINGATAN: ${dropped} iterasi di-drop (VU habis / k6 tidak sanggup kirim). Naikkan MAX_VUS atau cek CPU/memori/jaringan mesin k6 (atau jalankan k6 terdistribusi).\n`;
  }

  let soak = null;
  if (SOAK_RATE > 0) {
    const sr = val(m, "success_rate{step:soak}", "rate");
    const p95 = val(m, "latency_success{step:soak}", "p(95)");
    const p99 = val(m, "latency_success{step:soak}", "p(99)");
    const reqs = val(m, "step_reqs{step:soak}", "count");
    const reasons = evaluate(sr, p95, p99);
    soak = { rate: SOAK_RATE, minutes: SOAK_MIN, sr, p95, p99, reqs, pass: reasons.length === 0, reasons };
    out +=
      `\n--- SOAK ${SOAK_RATE} RPS x ${SOAK_MIN} menit ---\n` +
      `success=${sr === null ? "-" : f(sr * 100, 2)}% p95=${f(p95, 0)}ms p99=${f(p99, 0)}ms requests=${f(reqs, 0)} -> ` +
      (soak.pass ? "LULUS" : "GAGAL (" + reasons.join(", ") + ")") + "\n";
  }

  return {
    stdout: out,
    [summaryPath]: JSON.stringify(data, null, 2),
    [breakingPath]: JSON.stringify({ slo: SLO, lastStable, breaking, rows, soak }, null, 2),
  };
}
