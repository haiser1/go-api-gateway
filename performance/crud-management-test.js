import http from "k6/http";
import { check, sleep, group } from "k6";

// URL dasar untuk Management API Anda
const BASE_URL = "http://localhost:8080/api/1";

/*
 * ==============================================================================
 * OPSI SKENARIO (SKENARIO UJI)
 * ==============================================================================
 * Skenario ini akan berjalan selama 1 menit (60 detik).
 * Ini menggunakan 'ramping-vus' untuk mensimulasikan "tangga menurun" (staircase down).
 *
 * - 10 detik pertama: Ramp up ke 100 VU dan tahan.
 * - 10 detik kedua: Ramp down ke 80 VU.
 * - 10 detik ketiga: Ramp down ke 60 VU.
 * - 10 detik keempat: Ramp down ke 40 VU.
 * - 10 detik kelima: Ramp down ke 20 VU.
 * - 10 detik terakhir: Ramp down ke 0 VU.
 */
export const options = {
  scenarios: {
    ramping_down_load: {
      executor: "ramping-vus",
      startVUs: 0,
      stages: [
        { duration: "5s", target: 100 }, // Ramp up cepat ke 100
        { duration: "5s", target: 100 }, // Tahan
        { duration: "10s", target: 80 }, // Turun ke 80
        { duration: "10s", target: 60 }, // Turun ke 60
        { duration: "10s", target: 40 }, // Turun ke 40
        { duration: "10s", target: 20 }, // Turun ke 20
        { duration: "10s", target: 0 }, // Ramp down ke 0
      ],
      gracefulRampDown: "5s", // Waktu ekstra untuk VU menyelesaikan iterasi
    },
  },

  /*
   * ==============================================================================
   * THRESHOLDS (BATAS KEGAGALAN)
   * ==============================================================================
   * Di sinilah kita menetapkan target performa Anda.
   * Saya memisahkan threshold untuk operasi READ (GET) dan WRITE (POST, PUT, DELETE)
   * karena performa keduanya SANGAT berbeda.
   */
  thresholds: {
    http_req_failed: ["rate < 0.01"], // Gagal < 1% dari semua request

    // Target untuk READ (GET): Harusnya cepat (dari memori)
    "http_req_duration{op:read}": ["p(95) < 10"], // p95 < 10ms (Sesuai permintaan Anda)

    // Target untuk WRITE (POST/DELETE): PASTI LEBIH LAMBAT (karena file I/O)
    // Target 10ms di sini TIDAK REALISTIS. Saya set ke 200ms.
    "http_req_duration{op:write}": ["p(95) < 200"],
  },
};

// Parameter k6 untuk memberi tag pada request
const readParams = { tags: { op: "read" } };
const writeParams = (payload) => ({
  headers: { "Content-Type": "application/json" },
  tags: { op: "write" },
  body: JSON.stringify(payload),
});
const deleteParams = { tags: { op: "write" } };

/*
 * ==============================================================================
 * FUNGSI UTAMA (Apa yang dilakukan setiap VU)
 * ==============================================================================
 * Setiap Virtual User (VU) akan menjalankan siklus ini berulang kali.
 * 1. Menjalankan operasi READ (GET)
 * 2. Menjalankan operasi WRITE (POST/DELETE) untuk simulasi CRUD
 */
export default function () {
  // --- GRUP 1: Operasi READ (GET) ---
  // Ini seharusnya sangat cepat karena config manager mengambil dari memori.
  group("Read Endpoints", () => {
    // 1. Get all services
    const servicesRes = http.get(`${BASE_URL}/services`, readParams);
    check(servicesRes, {
      "GET /services: status 200": (r) => r.status === 200,
    });

    // 2. Get all routes
    const routesRes = http.get(`${BASE_URL}/routes`, readParams);
    check(routesRes, {
      "GET /routes: status 200": (r) => r.status === 200,
    });

    // 3. Get all global plugins
    const pluginsRes = http.get(`${BASE_URL}/global-plugins`, readParams);
    check(pluginsRes, {
      "GET /global-plugins: status 200": (r) => r.status === 200,
    });
  });

  sleep(1); // Tunggu 1 detik sebelum iterasi berikutnya

  // --- GRUP 2: Operasi WRITE (POST / DELETE) ---
  // Ini akan lebih lambat karena memicu file I/O (saveAndReload)
  group("Write Endpoints (CRUD Flow)", () => {
    // Buat nama unik untuk service baru agar tidak konflik
    const serviceName = `k6-test-service-${__VU}-${__ITER}`;

    const servicePayload = {
      name: serviceName,
      protocols: ["http"],
      host: "k6.test.host",
      port: 80,
    };

    // 1. Buat Service baru
    const createRes = http.post(
      `${BASE_URL}/services`,
      writeParams(servicePayload).body,
      { headers: writeParams().headers, tags: writeParams().tags },
    );

    check(createRes, {
      "POST /services: status 201": (r) => r.status === 201,
    });

    let newServiceId = null;
    try {
      if (createRes.body) {
        newServiceId = createRes.json("data.id");
      }
    } catch (e) {
      console.error("Gagal parse JSON dari create response:", createRes.body);
    }

    // Hanya lanjut jika service berhasil dibuat
    if (newServiceId) {
      // 2. Hapus Service yang baru dibuat
      const deleteRes = http.del(
        `${BASE_URL}/services/${newServiceId}`,
        null,
        deleteParams,
      );
      check(deleteRes, {
        "DELETE /services/:id: status 204": (r) => r.status === 204,
      });
    }
  });

  sleep(1); // Tunggu 1 detik lagi
}
