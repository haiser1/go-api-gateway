import http from "k6/http";
import { check } from "k6";
import { Counter, Trend, Rate } from "k6/metrics";

// === Custom Metrics ===
const errorCount = new Counter("custom_errors");
const latencyTrend = new Trend("latency_trend");
const successRate = new Rate("success_rate");

export const options = {
  scenarios: {
    // =====================================================
    // PHASE 1: WARM-UP
    // =====================================================
    warmup: {
      executor: "ramping-vus",
      startVUs: 0,
      stages: [
        { duration: "10s", target: 100 },
        { duration: "20s", target: 100 },
      ],
      gracefulRampDown: "5s",
    },

    // =====================================================
    // PHASE 2: FIND THE BREAKING POINT
    //
    // Menggunakan ramping-arrival-rate: k6 MEMAKSA kirim
    // X request/detik. VU pool besar (preAllocated: 5000,
    // max: 10000) agar k6 punya cukup goroutine untuk
    // menampung request yang antri saat gateway melambat.
    //
    // preAllocatedVUs harus > (target_RPS × avg_latency_s)
    // Contoh: 10000 RPS × 1s avg = 10000 VU dibutuhkan
    // =====================================================
    breaking_point: {
      executor: "ramping-arrival-rate",
      startRate: 500,
      timeUnit: "1s",
      preAllocatedVUs: 5000,
      maxVUs: 10000,

      stages: [
        // --- Warm zone (sudah terbukti handle) ---
        { duration: "20s", target: 1000 },

        // --- Escalation ---
        { duration: "20s", target: 2000 },
        { duration: "30s", target: 2000 },  // Hold — observe

        // --- Push harder ---
        { duration: "20s", target: 3000 },
        { duration: "30s", target: 3000 },  // Hold

        // --- Heavy ---
        { duration: "20s", target: 5000 },
        { duration: "30s", target: 5000 },  // Hold

        // --- Extreme ---
        { duration: "20s", target: 7000 },
        { duration: "30s", target: 7000 },  // Hold

        // --- Max push ---
        { duration: "20s", target: 10000 },
        { duration: "1m", target: 10000 },  // Hold long — final test

        // --- Beyond ---
        { duration: "20s", target: 15000 },
        { duration: "30s", target: 15000 }, // Hold

        // --- Cooldown & recovery test ---
        { duration: "20s", target: 1000 },
        { duration: "10s", target: 0 },
      ],

      startTime: "30s",
    },
  },

  thresholds: {
    // Threshold sangat longgar — kita cari breaking point
    http_req_failed: [
      { threshold: "rate < 0.80", abortOnFail: false },
    ],
    http_req_duration: [
      { threshold: "p(95) < 30000", abortOnFail: false },
    ],
    success_rate: [
      { threshold: "rate > 0.20", abortOnFail: false },
    ],
  },
};

export default function () {
  const BASE_URL = __ENV.TARGET_URL || "http://localhost:8080";
  const url = `${BASE_URL}/mock`;

  const params = {
    headers: { "Content-Type": "application/json" },
    timeout: "15s",
  };

  const res = http.get(url, params);

  latencyTrend.add(res.timings.duration);

  const passed = check(res, {
    "status is 200": (r) => r.status === 200,
    "latency < 100ms": (r) => r.timings.duration < 100,
    "latency < 500ms": (r) => r.timings.duration < 500,
    "latency < 1s": (r) => r.timings.duration < 1000,
    "latency < 3s": (r) => r.timings.duration < 3000,
    "latency < 5s": (r) => r.timings.duration < 5000,
  });

  successRate.add(res.status === 200);

  if (!passed) {
    errorCount.add(1);
    if (res.status !== 200) {
      console.error(
        `[VU=${__VU}] Status=${res.status} Duration=${res.timings.duration.toFixed(0)}ms`
      );
    }
  }
}