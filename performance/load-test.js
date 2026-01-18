import http from "k6/http";
import { check, group } from "k6";

export const options = {
  scenarios: {
    latency_test: {
      executor: "ramping-vus",
      startVUs: 0,
      stages: [
        // TAHAP 1: LIGHT LOAD (20 VU)
        // Mengecek latensi di kondisi sangat ideal.
        { duration: "30s", target: 10 },
        { duration: "30s", target: 20 },

        // TAHAP 2: MEDIUM LOAD (50 VU)
        // Prediksi saya: Ini adalah "Sweet Spot" untuk spec 0.5 CPU.
        // Kita harap di sini p90 masih di bawah 100ms.
        // { duration: "30s", target: 50 },
        // { duration: "1m", target: 50 },

        // TAHAP 3: HIGH LOAD (100 VU)
        // Kita uji batas atas. Jika di sini latensi tembus > 200ms,
        // berarti kapasitas max server (dengan latensi bagus) ada di bawah 100 VU.
        { duration: "30s", target: 50 },
        { duration: "30s", target: 20 },
        // { duration: "1m", target: 100 },

        // TAHAP 4: COOLDOWN
        { duration: "30s", target: 0 },
      ],
      gracefulRampDown: "10s",
    },
  },

  thresholds: {
    // Kriteria Error: Mutlak di bawah 1%
    http_req_failed: ["rate < 0.01"],

    // Kriteria Latensi (Sesuai Permintaan):
    // Jika salah satu ini terlewati, test akan ditandai FAILED (silang merah)
    http_req_duration: [
      "p(99) < 200", // 99% request harus selesai di bawah 200ms
      "p(95) < 150", // 95% request harus selesai di bawah 150ms
      "p(90) < 100", // 90% request harus selesai di bawah 100ms
    ],
  },
};

export default function () {
  const BASE_URL = __ENV.TARGET_URL || "http://localhost:8080";
  const url = `${BASE_URL}/mock`;

  const params = {
    headers: {
      "Content-Type": "application/json",
    },
  };

  const res = http.get(url, params);

  // Debugging log jika ada error
  if (res.status !== 200) {
    console.error(`ERROR: Status ${res.status} | Body: ${res.body ? res.body.substring(0, 50) : ''}`);
  }

  check(res, {
    "status is 200": (r) => r.status === 200,
  });
}