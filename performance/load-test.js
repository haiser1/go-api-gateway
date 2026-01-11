import http from "k6/http";
import { check, group } from "k6";

// const DUMMY_JWT =
//   "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiYWRtaW4iOnRydWUsImlhdCI6MTUxNjIzOTAyMn0.EGhYyuF8eeWfnVPbyxvSNgwahgKge8xxNvzJ7PnQ5rw";

/*
 * ==============================================================================
 * OPSI SKENARIO: "BREAKER TEST"
 * ==============================================================================
 * Skenario ini dirancang untuk menemukan titik putus (breaking point)
 * dengan terus menambah beban melebihi 100 VU.
 */
export const options = {
  scenarios: {
    breaker_test: {
      executor: "ramping-vus",
      startVUs: 0,
      stages: [
        // Tahap 1: Ramp-up ke 100 VU (beban yang kita tahu stabil tapi lambat)
        { duration: "30s", target: 100 },
        // Tahap 2: Tahan 100 VU selama 30 detik untuk baseline
        { duration: "30s", target: 100 },
        // Tahap 3: Tambah beban secara agresif ke 500 VU selama 1 menit
        { duration: "1m", target: 500 },
        // Tahap 4: Tahan beban puncak 500 VU selama 1 menit
        { duration: "1m", target: 500 },
        // Tahap 5: Ramp-down (pendinginan)
        { duration: "30s", target: 0 },
      ],
      gracefulRampDown: "10s",
    },
  },

  thresholds: {
    // Ini adalah target KESUKSESAN utama kita.
    // Jika server mulai memunculkan error (5xx, timeout, dll), tes akan GAGAL.
    http_req_failed: ["rate < 0.01"], // Gagal < 1%

    // Kita longgarkan target latensi karena kita MENGHARAPKAN server lambat.
    // Kita hanya ingin tahu apakah server 'hank' atau tidak.
    http_req_duration: ["p(95) < 100"], // p95 < 100ms
  },
};

/*
 * ==============================================================================
 * FUNGSI UTAMA (Tanpa sleep)
 * ==============================================================================
 */
export default function () {
  const url = "http://localhost:8080/mock";

  const params = {
    headers: {
      // Authorization: `Bearer ${DUMMY_JWT}`,
      "Content-Type": "application/json",
    },
  };

  group("Public Proxy Endpoints", () => {
    const res = http.get(url, params);
    check(res, {
      // PERBAIKAN: Nama check disesuaikan dengan URL
      "GET /mock: status 200": (r) => r.status === 200,
    });
  });

  // TIDAK ADA sleep();
}
