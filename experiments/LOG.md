# Experiments Logbook — Go API Gateway Testing (Bab IV)

> Berkas ini mencatat seluruh aktivitas, eksekusi fase, ringkasan hasil, dan anomali selama pengujian Go API Gateway untuk kebutuhan data skripsi.

---

## [2026-11-08] FASE 0 — Rekonstruksi Repositori (Read-Only)

### 1. Aktivitas yang Dilakukan
- Melakukan penelusuran menyeluruh terhadap seluruh struktur kode repositori `go-api-gateway`, mencakup:
  - Konfigurasi: `configs/config.yaml`, `internal/config/*`
  - Autentikasi: `internal/auth/*`, `internal/secrets/*`
  - Router & Proxy: `internal/gateway/*` (Radix tree, server, proxy, routing, circuit breaker, transport pool)
  - Middleware: `internal/middleware/*` (metrics, cors, logger, ip-restriction, ratelimit, request-transform)
  - Handlers: `internal/handler/*` (management API, auth update)
  - Upstream & Mock: `mock-server/main.go`, `docker-compose.yml`, `Makefile`
- Menyusun laporan rekonstruksi lengkap di [`experiments/REPO_FINDINGS.md`](file:///home/mark/Development/go-api-gateway/experiments/REPO_FINDINGS.md).

### 2. Ringkasan Temuan Kunci
1. **Validasi API Key:**
   - Header default: `X-API-Key` (dapat dikonfigurasi).
   - Kredensial tidak disimpan di `config.yaml`, melainkan di secret provider (`configs/.env`).
   - Format kanonikal disimpan dalam bentuk digest SHA-256 (`sha256:...`). Perbandingan dilakukan dalam *constant time* menggunakan `subtle.ConstantTimeCompare`.
   - Gateway mengelola satu active machine API key hash. Pencabutan dilakukan dengan merotasi key via Admin API `PUT /api/config/auth/api-key` atau mengubah `.env`.
2. **Validasi JWT:**
   - Mendukung algoritma simetris `HS256` dan asimetris `RS256` (RSA 2048-bit).
   - Secret HS256 (min 32 byte) dan Public Key RS256 diambil dari secret provider (`.env`).
   - Klaim yang divalidasi: `exp`, `nbf`, `iss`, `aud`, dan `kid`.
   - Algoritma **di-pin secara ketat** (`jwt.WithValidMethods` dan validasi eksplisit di `keyFunc`). Token `alg: "none"` maupun serangan *algorithm confusion* (HS256 dengan RSA public key) ditolak langsung dengan status 401.
   - Toleransi clock skew (*leeway*) dapat dikonfigurasi via `leeway` (default: 0).
3. **Kombinasi API Key & JWT:**
   - Mode `"both"` didukung penuh.
   - Urutan validasi: API key divalidasi terlebih dahulu; jika lolos, dilanjutkan validasi JWT. Jika salah satu hilang atau salah, mengembalikan HTTP 401.
4. **Urutan Middleware & Rate Limiter:**
   - Urutan prioritas eksekusi: `metrics (0)` -> `cors (1)` -> `logger (2)` -> `ip-restriction (3)` -> `rate-limiting (4)` -> `auth (5)` -> `request-transform (6)` -> `upstream/proxy`.
   - **Penting untuk S10:** `rate-limiting` berjalan **sebelum** `auth` dan menghitung setiap request masuk (`requests[ip]++`). Oleh karena itu, kegagalan autentikasi ikut dihitung dan akan memicu HTTP 429 jika melampaui ambang batas.
5. **Mekanisme Hot Reload:**
   - Perubahan `config.yaml` dan `.env` dipantau oleh `fsnotify` dengan debouncing 100ms.
   - Bersifat atomik melalui kandidat runtime validation, atomic file rewrite (`tmp` + `os.Rename`), dan memori pointer swap (`p.snapshot.Swap(snap)`).
   - Request yang sedang berjalan (*in-flight*) aman dan tidak terputus karena memegang referensi snapshot aktif hingga selesai (*draining*).

### 3. Evaluasi Kelayakan Skenario
- **F-Series (F01–F09):** Seluruh 9 skenario fungsional berstatus **Didukung**.
- **S-Series (S01–S12) + S13–S15:** Seluruh skenario keamanan berstatus **Didukung**.

### 4. Celah & Keterbatasan yang Diidentifikasi (Tanpa Ubah Kode)
1. Single machine API key per gateway (belum multi-tenant API key management).
2. Blok `auth:` di level teratas `config.yaml` berlaku mutlak untuk semua rute tanpa opsi bypass rute publik (kecuali jika auth dipasang per-rute).
3. Header identitas seperti `X-User-Id` tidak disanitasi secara default ke upstream (potensi header spoofing jika upstream percaya tanpa verifikasi).
4. Tidak ada mekanisme *token blacklist* (JWT bersifat murni stateless, tidak dapat dicabut sebelum `exp` habis tanpa rotasi secret).
5. Rate limiting berbasis memori lokal dan IP `RemoteAddr` (belum terdistribusi via Redis di binary utama).

### 5. Checklist "Selesai bila" (Fase 0)
- [x] `experiments/REPO_FINDINGS.md` lengkap dengan bukti file & baris kode.
- [x] Tabel kelayakan skenario (F01–F09, S01–S12, S13–S15) terisi lengkap.

### 6. Status Checkpoint
- **[CHECKPOINT 0] KEPUTUSAN PEMILIK SKRIPSI:**
  - Celah No 2 (Scoping Auth): Direfaktor agar level auth hanya berada di tingkat Service dan Route saja, serta menghapus level globalnya.
  - Celah No 3 (Identity Header Spoofing): Diperbaiki agar header `X-User-Id` disanitasi (dihapus pada rute unauthenticated, ditimpa dengan identitas terverifikasi pada rute authenticated).
  - Celah No 1 (Multi-tenant API Key): Ditunda untuk masa depan bersamaan dengan Redis rate limiting dan pencabutan JWT.

---

## [2026-11-09] Perbaikan Celah No 2 & No 3 Pasca-Checkpoint 0

### 1. Refactor Scoping Auth (Service & Route Only, Hapus Level Global)
- **Penghapusan Global Plugin Auth:**
  - Menghapus deklarasi `auth` dari `global_plugins` pada [`configs/config.yaml`](file:///home/mark/Development/go-api-gateway/configs/config.yaml).
  - Menambahkan aturan validasi semantik pada [`internal/config/validator/semantic.go`](file:///home/mark/Development/go-api-gateway/internal/config/validator/semantic.go) dan [`internal/config/auth.go`](file:///home/mark/Development/go-api-gateway/internal/config/auth.go) yang secara eksplisit menolak keberadaan plugin `auth` atau `authorization` di `GlobalPlugins`.
- **Penghapusan Injeksi Global Otomatis:**
  - Menghapus blok `if cfg.Auth != nil { resolvedPlugins = append(...) }` pada [`internal/gateway/routing.go`](file:///home/mark/Development/go-api-gateway/internal/gateway/routing.go).
- **Hasil Scoping:**
  - Rute di bawah Service dengan plugin `auth` aktif akan mewarisi proteksi otentikasi.
  - Rute di bawah Service berotentikasi dapat menimpa dengan `plugins: [{name: "auth", enabled: false}]` agar menjadi rute publik (misal `/health`).
  - Rute di bawah Service tanpa otentikasi dapat mengaktifkan `auth` secara independen di tingkat Route.
  - Rute tanpa plugin `auth` pada Route maupun Service menjadi rute publik (mengembalikan 200).

### 2. Perbaikan Identity Header Spoofing (Sanitasi & Propagasi X-User-Id)
- Pada fungsi `buildMiddlewareChain` di [`internal/gateway/routing.go`](file:///home/mark/Development/go-api-gateway/internal/gateway/routing.go):
  - Jika request **tidak terotentikasi** (rute publik): header `X-User-Id` dari klien dihapus (`r.Header.Del("X-User-Id")`), mencegah spoofing identitas ke upstream.
  - Jika request **terotentikasi via JWT**: header `X-User-Id` ditimpa dengan subject klaim token yang sah (`r.Header.Set("X-User-Id", authCtx.User.Subject)`).
  - Jika request **terotentikasi via API Key**: header `X-User-Id` ditimpa dengan machine identity ID (`r.Header.Set("X-User-Id", authCtx.Machine.APIKeyID)`).

### 3. Verifikasi Pengujian & Benchmarks
- Menambahkan unit test integrasi baru di [`internal/gateway/auth_integration_test.go`](file:///home/mark/Development/go-api-gateway/internal/gateway/auth_integration_test.go):
  - `TestProxy_ServiceLevelAuthInheritanceAndRouteOverride`: Memverifikasi pewarisan auth tingkat service dan override tingkat rute.
  - `TestProxy_IdentityHeaderSanitizationAndPropagation`: Memverifikasi sanitasi header `X-User-Id` saat unauthenticated dan propagasi saat authenticated.
- Seluruh test suite (`go test -race ./...`) lulus 100% tanpa race condition.
- Radix tree & gateway lookup benchmarks tetap **0 allocs/op** pada hot path.
- Binary produksi berhasil dikompilasi dengan `make build`.

---

## [2026-11-09] FASE 1 — Lingkungan Uji

### 1. Aktivitas
- Meninjau ulang `experiments/REPO_FINDINGS.md` terhadap kode pasca-refactor `dccb182`; memperbarui bagian top-level auth (kini hanya kebijakan; penegakan via plugin Service/Route), status celah #2/#3 (selesai diperbaiki), dan bukti S13 (sanitasi `X-User-Id`).
- Menyempurnakan `experiments/compose/docker-compose.test.yml`:
  - Image k6 dipin ke `grafana/k6:2.3.0` (bukan `latest`).
  - `/configs` di-mount dari `${CONFIG_DIR:-../runtime}` sehingga konfigurasi A–D dapat dipilih per run dan tetap writable untuk uji hot reload/pencabutan.
  - `mock-server` tetap **tidak** mem-publish port 9090 ke host (kebutuhan S12).
  - Batas resource aktual: gateway `cpuset=0,1`/512 MiB, mock-server `cpuset=2`/256 MiB, k6 `cpuset=3,4`/2048 MiB (host 8 thread, 5762 MiB).
- Menambahkan `experiments/scripts/prepare_config.sh` (menyalin config terpilih + `.env` fixture ke `experiments/runtime/`).
- Menambahkan target Makefile: `make up CONFIG=A|B|C|D|C2|Z`, `make down`, `make test-ps`, `make test-logs`.
- Memperbarui `collect_env.sh` agar memakai image k6 yang dipin; `experiments/runtime/` di-ignore dari git.

### 2. Verifikasi Checklist "Selesai bila"
- [x] `make up CONFIG=A` menyalakan `test-api-gateway` dan `test-mock-server` tanpa error.
- [x] `curl http://localhost:8080/mock` dan `/api/v1/users` → **200** `{"status":"mock_ok"}`.
- [x] `curl http://localhost:9090/mock` dari host → **connection refused** (sesuai S12).
- [x] Dari jaringan Docker (`compose_test-net`) `http://mock-server:9090/mock` → **200** (membuktikan kontrol bersifat jaringan).
- [x] `experiments/results/env.json` terisi (commit `dccb182`, host, versi software, batas kontainer).

### 3. Catatan
- `env.json` saat ini mencatat `dirty_files=8` karena perubahan dokumen/script fase ini belum di-commit; angka final akan diperbarui pada run final.
- Tidak ada perubahan kode inti gateway pada fase ini.

---

## [2026-11-09] FASE 2 — Konfigurasi A–D dan Fixture

### 1. Konfigurasi Gateway
Dibuat di `experiments/compose/`:
- `config.A.yaml` — baseline tanpa autentikasi.
- `config.B.yaml` — API key saja (`mode: api_key`, `secret_ref: API_KEY_HASH`).
- `config.C.yaml` — JWT HS256 (`iss=thesis-test`, `aud=go-api-gateway`, `leeway=0s`).
- `config.C2.yaml` — JWT RS256 (karena RS256 didukung).
- `config.D.yaml` — API key + JWT HS256 (`mode: both`, usulan skripsi).
- `config.F.yaml` — D + rate limit ambang sangat tinggi (`1000000000.0/menit`).
- `config.F06.yaml` — D + `request-transform` di global/service/route (khusus uji prioritas F06).
- `config.RL.yaml` — D + rate limit rendah 60/menit pada `/api/v1/users` (khusus S10/S11).
Semua konfigurasi memakai upstream, service, dan himpunan rute yang sama; perbedaan hanya pada plugin autentikasi/rate-limit. Rute `/health` memakai override `auth.enabled=false` sebagai rute publik (F03). Tidak ada auth di `global_plugins` (ditolak validator).

### 2. Fixture
- `experiments/scripts/gen_tokens/main.go` menghasilkan `experiments/fixtures/tokens.json`, `api_keys.json`, `rsa_private.pem`, `rsa_public.pem`, dan `.env` (dummy).
- API key: `valid`, `revoked`, `unknown`; valid disimpan sebagai `API_KEY_HASH` (`sha256:...`).
- Token: `valid_long` (72 jam), `valid_long_rs256`, `expired` (exp = now−60 s), `tampered_payload`, `alg_none_with_dot`, `alg_none_no_dot`, `wrong_iss`, `wrong_aud`, `nbf_future`, `alg_confusion` (HS256 dengan public key RSA), `malformed_*` (4 varian), `oversized_valid` (~24 KB), `oversized_garbage` (~40 KB).
- Secret HS256 64 byte acak; RSA 2048-bit.
- `experiments/runtime/` diisi ulang oleh `prepare_config.sh` (config + `.env`) setiap pergantian konfigurasi.

### 3. Perubahan Pendukung Non-Inti
- `mock-server/main.go` ditambah rute `/echo` yang mengembalikan method/path/query/headers/body yang diteruskan gateway (dibutuhkan F06/F09/S13). Rute kinerja (`/api/v1/users`) tetap respons konstan 21 byte.
- `docker-compose.test.yml` menambahkan `GATEWAY_ADMIN_TOKEN=test-admin-token` (dummy) agar S03 dapat mencabut key via Admin API.

### 4. Verifikasi Checklist "Selesai bila"
- [x] `make up CONFIG=A` → `/api/v1/users` 200.
- [x] `make up CONFIG=B` → key valid 200, tanpa key 401, `/health` 200.
- [x] `make up CONFIG=C` → JWT HS256 valid 200, tanpa kredensial 401.
- [x] `make up CONFIG=C2` → JWT RS256 valid 200.
- [x] `make up CONFIG=D` → API key + JWT valid 200; tanpa kredensial 401; `/health` publik 200.
- [x] Fixture lengkap dan `tokens.json` memuat metadata.

### 5. Temuan Bug/Keterbatasan (belum diperbaiki, menunggu izin)
1. **Tipe `requests_per_minute`:** `internal/middleware/ratelimit.go` hanya membaca `float64`; nilai integer YAML (mis. `100`) jatuh ke default 60 tanpa peringatan. Usulan minimal: helper parsing `int/float64/string`.
2. **Rate limit per koneksi, bukan per IP:** key memakai `r.RemoteAddr` utuh (termasuk port). Klien yang membuka koneksi baru tiap request tidak pernah kena limit. Usulan minimal: `net.SplitHostPort(r.RemoteAddr)` (atau dukung `trust_forwarded_for`).
3. **Rate limit in-memory:** tidak terdistribusi; hanya berlaku per instance gateway (sudah tercatat sebagai gap Fase 0).

---

## [2026-11-09] FASE 3 — Pengujian Fungsional (F01–F09)

### 1. Aktivitas
- Menulis `experiments/scripts/run_functional.py` (Python stdlib + PyYAML) yang:
  - mengganti konfigurasi via `make up CONFIG=...` + restart gateway;
  - menjalankan F01–F09, menulis satu baris per kasus ke `results/functional.csv`;
  - menyimpan observasi mentah per kasus (timestamp, respons, status sequence) di `results/raw/functional/`;
  - menghasilkan `results/tables/table_functional.md` dari CSV.
- F06 memakai `config.F06.yaml` (D + request-transform global/service/route). F07 mengubah `experiments/runtime/config.yaml` secara langsung lalu memulihkannya; ID kontainer dibandingkan sebelum/sesudah. F08 menghentikan `test-mock-server`, mengirim 8 request, menyalakannya kembali, menunggu cooldown breaker 30 s, lalu memverifikasi pemulihan.

### 2. Hasil Ringkas (9/9 PASS)
| ID | Hasil |
|---|---|
| F01 | 200, body identik dengan upstream (`{"status":"mock_ok"}`) |
| F02 | 404 |
| F03 | 200 pada `/health` tanpa kredensial (override auth route) |
| F04 | 401 (`Authorization header required`) |
| F05 | 401 (`API key required`) |
| F06 | 200, `X-Scope=route` (route menimpa service & global) |
| F07 | 401→200 dalam 0.106 s, 200→401 dalam 0.210 s, ID kontainer tidak berubah |
| F08 | 5×502 lalu 3×503 saat upstream mati; 200 setelah upstream hidup + cooldown |
| F09 | Body & `X-Custom` diteruskan; `X-User-Id` spoofing ditimpa `user-123` |

### 3. Checklist "Selesai bila"
- [x] `results/functional.csv` terisi 9 kasus.
- [x] `results/tables/table_functional.md` terbentuk dari CSV.
- [x] Data mentah tersimpan di `results/raw/functional/` (termasuk run F08 yang gagal karena bug skrip; run ulang juga disimpan, tidak ada cherry-pick).

### 4. Catatan
- F08 menunjukkan respons 502 sebelum breaker mencapai ambang 5 kegagalan (perilaku normal: 5 attempt gagal, lalu 503).
- Hot reload efektif < 0.25 s pada beban idle; pengukuran di bawah beban dilakukan pada Fase 6.
- Tidak ada perubahan kode inti gateway.

---

## [2026-11-09] FASE 4 — Pengujian Keamanan (S01–S14)

### 1. Aktivitas
- Menulis/menyempurnakan `experiments/scripts/run_security.py` (Python stdlib + PyYAML):
  - S01–S09, S13, S14 memakai config D (API key + JWT HS256); S08 memakai config C2 (RS256 + token alg-confusion); S10/S11 memakai config D dan RL (rate limit 60/menit); S12 memakai config A.
  - N = 30 serangan + 30 kontrol sah per skenario standar; S10 = 500 tebakan pada satu koneksi keep-alive; S11 = banjir sampai 429 lalu probe sampai jendela 1 menit pulih.
  - Setiap request dicatat ke `results/security_raw.csv`; ringkasan per skenario ke `results/raw/security/`; tabel ke `results/tables/table_security.md`.
  - Dua bug runner diperbaiki sebelum run final: (a) modul `http.client` tertutup fungsi lokal `http()` → diganti alias `httpclient`; (b) restore `runtime/config.yaml` gagal karena file milik root setelah ditulis Admin API → diganti `atomic_replace()` (temp + `os.replace`).
- S03 mencabut API key lewat Admin API (`PUT /api/config/auth/api-key`), menguji 30 request, lalu memulihkan key.
- S12 membandingkan akses langsung dari host (gagal) dengan akses dari dalam jaringan Docker (200).
- S13 menilai sanitasi `X-User-Id` dari body `/echo`, bukan status HTTP. S14 memisahkan token malformed, oversized garbage, dan oversized valid.
- Run diulang 3×; CSV tiap run diarsipkan di `results/raw/security/security_raw_run{2,3}_*.csv` dan run parsial di `security_raw_partial_*.csv` (tidak ada cherry-pick).

### 2. Hasil Ringkas
| ID | Config | Dikirim | Ditolak | Block rate | FRR | Catatan |
|---|---|---|---|---|---|---|
| S01 | D | 30 | 30 | 100% | 0/30 | tanpa kredensial → 401 |
| S02 | D | 30 | 30 | 100% | 0/30 | API key acak + JWT valid → 401 |
| S03 | D | 30 | 30 | 100% | 0/30 | key dicabut via Admin API; 401; t_first_reject 1.1–2.1 ms; admin_http=200 |
| S04 | D | 30 | 30 | 100% | 0/30 | API key valid tanpa JWT → 401 |
| S05 | D | 30 | 30 | 100% | 0/30 | JWT expired → 401 |
| S06 | D | 30 | 30 | 100% | 0/30 | payload tampered → 401 |
| S07 | D | 30 | 30 | 100% | 0/30 | alg=none (dengan/tanpa titik) → 401 |
| S08 | C2 | 30 | 30 | 100% | 0/30 | alg confusion HS256/public-key → 401 |
| S09 | D | 30 | 30 | 100% | 0/30 | wrong iss/aud/nbf → 401 |
| S10 | D | 500 | 500 | 100% | 0/30 | tanpa rate limit: 500×401, tidak ada 429 |
| S10 | RL | 500 | 500 | 100% | 0/30 | 60×401 lalu 440×429; 429 pertama pada iterasi 61 |
| S11 | RL | 64 | 4 | 6,25% | 0/30 | 60×200 lalu 429 pada iterasi 61; pulih 200 setelah 60,04 s; 30 probe setelah reset ikut tercatat sebagai `after_reset_probe` |
| S12 | A | 5 | 5 | 100% | 0/1 | host direct connection refused 5/5; dari jaringan Docker 200 |
| S13 | D | 30 | 30 | 100%* | 0/30 | status 200 by design; spoof `X-User-Id` dihormati 0/30 (dinilai dari header upstream) |
| S14 | D | 30 | 25 | 83,33% | 0/30 | malformed 20/20 401; oversized_garbage 5/5 401; oversized_valid 5/5 200 (signature valid); `/health` tetap 200 |

\*S13 tidak mengukur status 4xx; angka 100% berarti tidak ada spoof identitas yang lolos.

**FRR agregat:** 0/421 kontrol sah ditolak = **0,0%**. Tidak ada serangan yang lolos pada S01–S10; satu-satunya 200 pada S14 adalah token oversized yang memang valid secara kriptografis.

### 3. Checklist "Selesai bila"
- [x] `results/security_raw.csv` lengkap (1.851 baris data; S01–S14, attack/control/probe).
- [x] `results/tables/table_security.md` memuat dikirim, ditolak, block rate, kode dominan, FRR, dan catatan.
- [x] Ringkasan FRR keseluruhan tersedia (0,0%).
- [x] Data mentah semua run tersimpan di `results/raw/security/` (termasuk 2 CSV run sebelumnya dan run parsial).

### 4. Temuan
1. **Rate limit per koneksi, bukan per IP** (dikonfirmasi ulang): S10 memakai satu koneksi keep-alive agar limit terpicu; klien dengan koneksi baru dapat melewati limiter. Usulan minimal: `net.SplitHostPort(r.RemoteAddr)`.
2. **Tipe `requests_per_minute`:** integer YAML diam-diam menjadi default 60; config RL memakai `60.0`. Usulan minimal: parsing `int`/`float64`.
3. **Rate limit in-memory:** tidak terdistribusi; hanya per instance (gap arsitektur, bukan regresi).
4. **Tidak ada perubahan kode inti gateway pada fase ini.**

**[CHECKPOINT 1]** Berhenti menunggu keputusan pemilik skripsi sebelum Fase 5–7.

---

## [2026-11-09] PASCA-CHECKPOINT 1 — Perbaikan Plugin Rate Limiting

### 1. Perubahan Kode Inti (disetujui pemilik skripsi)
- `internal/middleware/ratelimit.go` ditulis ulang:
  - Algoritma: `fixed_window` (default), `sliding_window` (weighted two-window counter), `token_bucket`.
  - Config: `algorithm`, `requests_per_window` / `requests_per_minute`, `window_seconds`, `burst`, `key_by`, `user_claim`, `trust_forwarded_for`.
  - Default key: IP dari `RemoteAddr` setelah `net.SplitHostPort` (memperbaiki bug per-koneksi); `key_by: user` memakai `sub` JWT, lalu klaim `user_id`/`uid`, atau `user_claim` kustom; fallback ke IP bila tidak ada identitas.
  - Header respons: `X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-RateLimit-Reset`, dan `Retry-After` pada 429.
  - Pembersihan state periodik tanpa goroutine ticker (tidak ada kebocoran goroutine per reload).
- `internal/gateway/routing.go`: prioritas plugin dinamis. Rate limit berbasis IP tetap sebelum auth (melindungi brute force); rate limit berbasis user dijalankan setelah auth agar membaca identitas JWT terverifikasi. Aturan override 3 level (global → service → route) tidak berubah dan diuji.
- `internal/config/validator/semantic.go`: kontrak validasi baru untuk algoritma, key, window, burst, claim, dan `trust_forwarded_for`.
- `configs/config.yaml`: contoh plugin diperbarui ke skema baru (`fixed_window`, 100/60 s, `key_by: ip`).
- Unit test baru: `internal/middleware/ratelimit_test.go` dan `internal/gateway/routing_plugin_test.go`.

### 2. Verifikasi
- `GOCACHE=/tmp/go-build go test -count=1 -race ./...` → semua paket lulus.
- `GOCACHE=/tmp/go-build go vet ./...` → bersih.
- `GOCACHE=/tmp/go-build make build` → sukses.
- Benchmark tetap berjalan (`BenchmarkMiddlewareChain`, `BenchmarkRadixTree_RouteSweep`); tidak ada regresi alokasi statis (1 alloc/op pada benchmark lookup).

### 3. Uji Keamanan Ulang (setelah perbaikan)
- Suite S01–S14 dijalankan ulang dengan fixture baru. Semua S01–S10 100% blok; FRR agregat 0/421 = 0,0%.
- S10 RL: 60×401 lalu 440×429; 429 pertama iterasi 61; kontrol sah menunggu reset window 28,04 s.
- S11 RL: 60×200 lalu 429 pada iterasi 61; pulih 200 setelah 34,03 s (fixed window sekarang selaras epoch, bukan ticker 60 s).
- Tabel terbaru: `experiments/results/tables/table_security.md`; CSV: `experiments/results/security_raw.csv`.

### 4. Catatan Pengujian
- Ditemukan fixture `nbf_future` menjadi valid setelah 1 jam (run S09 sempat 66,67%). Generator diubah ke `now+24h`, dan runner sekarang gagal cepat bila fixture sudah stale. Run invalid diarsipkan sebagai `raw/security/*INVALID_stale_fixture*`.
- Data sebelum perbaikan diarsipkan sebagai `raw/security/security_raw_pre_ratelimit_fix_*.csv` dan `table_security_pre_ratelimit_fix_*.md`; data setelah perbaikan ada di `security_raw.csv` / `table_security.md`.
- Rate limiter tetap in-memory per instance gateway (belum terdistribusi); gap Redis tetap sesuai spec terpisah.

---

## [2026-11-09] FASE 5 — Pengujian Kinerja

### 1. Aktivitas
- Menulis `experiments/k6/perf.js`: mode `closed` (`constant-vus`) dan `open` (`constant-arrival-rate`); metrik `avg`, `min`, `med`, `p(90)`, `p(95)`, `p(99)`, `max`; `handleSummary` menulis `summary.json`.
- Menulis `experiments/scripts/run_perf.py`: orkestrasi matriks, warmup, pengukuran, jeda, restart gateway, dan manifest. Setiap run menghasilkan `summary.json`, `stats.csv`, `k6.log`, `warmup.log`, dan `run.json`.
- Menulis `experiments/scripts/sample_stats.py` + wrapper `sample_stats.sh`: sampling CPU/memori tiap 1 detik lewat Docker API (`test-api-gateway`, `test-mock-server`, container k6).
- Metodologi: warmup 10 s (dibuang), ukur 60 s, jeda 30 s antar-run; restart gateway setiap pergantian konfigurasi; urutan konfigurasi diacak per pengulangan (seed 20261109); kredensial valid (`valid` API key + `valid_long` JWT); Z diarahkan langsung ke `mock-server:9090`.
- Matriks wajib: `{Z,A,B,C,D} × VU {50,100,200} × closed × 3 reps` = 45 run; open model `{A,B,C,D}` pada 4.226 RPS (50% RPS maksimum A, 8.452 RPS) × 3 reps = 12 run. Opsional dijalankan: `{C2,F} × VU {50,100,200} × closed × 3 reps` = 18 run.
- VU 500 tidak dijalankan: pada VU 200 gateway sudah jenuh (CPU 182–190% dari batas cpuset 2 core = 200%; throughput justru turun). Keputusan ini dicatat sebagai batas matriks, bukan run yang gagal.
- Data: `results/raw/perf/<config>_vu<VUS>_<mode>_r<rep>/`; manifest `results/raw/perf/manifest.csv` dan `manifest.json`.

### 2. Checklist "Selesai bila"
- [x] 75/75 run memiliki `summary.json`, `stats.csv`, `k6.log`, `warmup.log`, dan `run.json`.
- [x] Tidak ada run invalid; seluruh `error_rate` = 0 dan `checks` = 100% untuk request 200.
- [x] Statistik CPU/memori gateway tersedia; k6 CPU diperiksa terpisah pada VU 200: rata-rata 126% (63% dari cpuset 2 core), puncak 162,5% (81%); gateway 191% (96% dari cpuset).
- [x] `results/raw/perf/manifest.csv` menjadi sumber agregasi Fase 7.

### 3. Hasil Ringkas (rerata 3 repetisi)
| Config | VU | RPS closed | p95 closed (ms) | CPU gateway (%) | RPS open | p95 open (ms) |
|---|---:|---:|---:|---:|---:|---:|
| Z | 50 / 100 / 200 | 10.903,6 / 11.447,1 / 11.594,6 | 11,18 / 19,74 / 33,83 | ~0 | - | - |
| A | 50 / 100 / 200 | 7.157,7 / 7.255,4 / 5.090,3 | 16,37 / 29,47 / 88,25 | 159,4 / 169,6 / 182,8 | 4.200,8 | 15,05 |
| B | 50 / 100 / 200 | 6.698,9 / 6.897,8 / 4.949,1 | 17,64 / 31,13 / 88,84 | 156,1 / 168,5 / 184,4 | 4.214,4 | 11,88 |
| C | 50 / 100 / 200 | 6.382,4 / 6.812,3 / 5.120,5 | 17,75 / 31,67 / 80,91 | 168,0 / 173,1 / 186,5 | 4.208,9 | 13,72 |
| D | 50 / 100 / 200 | 6.139,5 / 6.295,2 / 4.641,6 | 18,39 / 33,57 / 91,86 | 168,1 / 176,0 / 182,6 | 4.195,4 | 22,53 |
| C2 (ops.) | 50 / 100 / 200 | 6.122,3 / 5.954,1 / 5.621,0 | 16,68 / 33,23 / 64,50 | 181,6 / 187,0 / 189,3 | - | - |
| F (ops.) | 50 / 100 / 200 | 6.986,5 / 7.235,9 / 5.130,0 | 15,85 / 28,38 / 82,99 | 166,3 / 174,4 / 186,4 | - | - |

### 4. Temuan
1. **Proxy + auth menambah biaya:** Z (langsung ke mock) mencapai 10,9–11,6 ribu RPS; A (proxy tanpa auth) 5,1–7,3 ribu RPS pada closed model. Pada open model 4.226 RPS, D memiliki p95 22,53 ms versus A 15,05 ms (+49,7%).
2. **Closed model jenuh pada VU 200:** p95 naik menjadi ~81–92 ms dan throughput turun; CPU gateway ~183–190% dari batas 2 core. VU 500 tidak diperlukan.
3. **Biaya dua lapis auth (D):** pada VU 50/100/200, D vs B = +4,3% / +7,8% / +3,4% p95; D vs C = +3,6% / +6,0% / +13,5% p95. Angka ini dipakai sebagai biaya inkremental awal; analisis simpangan baku dilakukan Fase 7.
4. **Open model lebih stabil** (SD RPS 7,6–32,8) daripada closed model (SD sampai 1.417,8 RPS); variasi closed kemungkinan berasal dari host bersama dan pengaruh thermal/noisy neighbor, bukan error aplikasi.
5. **Rate limiter ambang tinggi (F)** tidak menunjukkan penalti throughput yang jelas dibanding D (7,0 vs 6,1 ribu RPS pada VU 50; 5,1 vs 4,6 ribu pada VU 200); perbedaan masih dalam variasi antar-repetisi.
6. **Memori gateway** 14–61 MiB, jauh di bawah `mem_limit` 512 MiB. k6 bukan bottleneck utama pada VU 200 (rata-rata 63% cpuset; gateway 96%).

### 5. Catatan
- `stats.csv` untuk matriks utama tidak memuat baris k6 karena filter sampler awal hanya mengenali nama `test-k6*`, sedangkan container `docker compose run` bernama `compose-k6-run-*`; filter sudah diperbaiki. Pemeriksaan CPU k6 pada VU 200 dilakukan terpisah dan hasilnya dicatat di atas.
- Variasi RPS closed model cukup besar; jangan menyimpulkan perbedaan kecil antar-konfigurasi tanpa interval kepercayaan/simpangan baku (dihitung Fase 7).
- Data mentah tetap disimpan seluruhnya; tidak ada run yang dibuang atau di-cherry-pick.

---

## [2026-11-09] FASE 6 — Hot Reload dan Pencabutan

### 1. Aktivitas
- Menulis `experiments/scripts/run_hotreload.py`: memindahkan gateway ke config D, mengukur 10 repetisi pencabutan API key lewat `PUT /api/config/auth/api-key`, lalu menjalankan k6 50 VU selama 60 s pada rute `/health` sambil melakukan 5 reload admin dan probe independen tiap 50 ms.
- Dua percobaan awal gagal sebelum menghasilkan data karena `http.client.HTTPConnection` tidak mendukung context manager pada Python 3.14; skrip diperbaiki memakai `try/finally` + `close()` dan alamat `127.0.0.1`. Run final berhasil.
- Output: `results/hotreload.csv`, `results/raw/hotreload/revocation_r*.{csv,json}`, `availability.{json,csv}`, `availability_probe.csv`, `availability_reloads.csv`, `availability_k6.log`, dan `summary.md`.

### 2. Checklist "Selesai bila"
- [x] 10 repetisi pencabutan tercatat di `hotreload.csv` dengan `t_change` dan `t_first_reject_ms`.
- [x] Ringkasan rerata/min/maks tersedia.
- [x] Uji ketersediaan 50 VU/60 s pada rute tidak terdampak (`/health`) dengan 5 reload selesai.
- [x] Container gateway tetap sama selama uji (tanpa restart).

### 3. Hasil
- Pencabutan API key: rerata **50,759 ms**, minimum **50,438 ms**, maksimum **51,208 ms**; **10** request in-flight (1 per repetisi) masih mendapat 200 karena sudah lolos autentikasi sebelum swap.
- Probe pencabutan memakai interval 50 ms, sehingga hasil tersebut adalah **batas atas dengan resolusi 50 ms**, bukan latensi internal gateway. Pengukuran S03 dengan request langsung setelah perubahan mencatat t_first_reject ~1,4 ms; jadi pencabutan efektif jauh di bawah batas atas probe.
- Ketersediaan: k6 50 VU mencapai **7.582,7 req/s** pada `/health`, **error rate 0**, p95 **15,53 ms**; probe independen **0 non-200 dari 2.956 request**, median **1,56 ms**, maksimum **26,54 ms**.
- Reload admin selesai dalam **14,6–17,3 ms**; tiap window 1 detik di sekitar reload memiliki 0 non-200 dan max latency 8,9–13,5 ms. Tidak ada bukti downtime atau penolakan request sah.
- Catatan: `/health` adalah rute ringan; RPS-nya tidak boleh dibandingkan langsung dengan throughput rute proxy pada Fase 5. Uji ini hanya mengukur ketersediaan dan bukan kapasitas.

---

## [2026-11-09] FASE 7 — Analisis, Tabel, dan Grafik

### 1. Aktivitas
- Menulis `experiments/scripts/analyze.py` (pandas + matplotlib) yang membaca manifest/raw kinerja, ringkasan security terbaru, `functional.csv`, dan data hot reload.
- Karena Python sistem tidak memiliki pip/pandas, analisis dijalankan dengan venv sementara di `/tmp/opencode/thesis-venv`:
  `uv venv ... && uv pip install pandas matplotlib && /tmp/opencode/thesis-venv/bin/python experiments/scripts/analyze.py`.
- Output Markdown + CSV: `table_functional`, `table_security`, `table_perf_latency`, `table_perf_throughput`, `table_perf_resources`, `table_overhead`, `table_hotreload`.
- Output PNG 300 dpi: `fig1_latency_avg_closed`, `fig2_latency_p95_p99_closed`, `fig3_throughput`, `fig4_cpu_mem`, `fig5_overhead_p95`, `fig6_block_rate`, `fig7_open_latency_distribution`.
- `results/RESULTS_SUMMARY.md` dihasilkan dari perhitungan yang sama.

### 2. Checklist "Selesai bila"
- [x] Tabel Markdown + CSV untuk fungsional, keamanan, latensi, throughput, sumber daya, overhead, dan hot reload.
- [x] Tujuh grafik PNG 300 dpi dengan sumbu berlabel, satuan, dan pola/marker hitam-putih; error bar = simpangan baku.
- [x] Perhitungan rerata/SD per konfigurasi/VU, overhead terhadap A, proxy A vs Z, penurunan throughput, biaya incremental, block rate/FRR, dan aditivitas auth.
- [x] `RESULTS_SUMMARY.md` memuat angka kunci, temuan, anomali, keterbatasan, dan run yang dikecualikan.
- [x] `go test -count=1 -race ./...`, `go vet ./...`, dan `make build` lulus setelah perbaikan `routing.go`.

### 3. Temuan Analisis
- Overhead p95 D vs A (closed): VU 50 **+12,4%**, VU 100 **+13,9%**, VU 200 **+4,1%**; arah rerata konsisten tetapi SD antar-run 27–32% sehingga perbedaan belum dapat disebut signifikan dari 3 repetisi.
- Open model 4.226 req/s: rerata p95 D **22,53 ms** vs A **15,05 ms** (+49,7%), tetapi SD besar; ini indikasi tren, bukan klaim signifikansi.
- A vs Z (closed, p95): +46,4% (VU 50), +49,3% (VU 100), +160,9% (VU 200); biaya proxy mendominasi biaya auth.
- Aditivitas auth hanya dapat dievaluasi pada VU 50/100: p95 D−A = 2,02/4,10 ms vs (B−A)+(C−A) = 2,65/3,86 ms (rasio 76%/106%). Pada VU 200 dan open model komponen individual negatif/dekat nol, sehingga rasio ditandai `n/a` dan tidak disimpulkan.
- Throughput Z jauh lebih tinggi dari A (11.595 vs 5.090 RPS pada VU 200); seluruh konfigurasi gateway jenuh pada VU 200 (CPU 182,6–186,5% dari satu core; cpuset 2 core).
- Security: FRR agregat 0/421; S01–S10/S12 100% blokir; S11 = uji ambang rate limit; S14 opsional 25/30 karena 5 token besar dengan signature valid memang diterima.
- Anomali utama: variasi closed model besar (SD RPS sampai ±1.417,8), A kadang lebih rendah dari B/C pada VU 200; jangan menafsirkan perbedaan kecil sebagai efek konfigurasi tanpa mempertimbangkan SD.

### 4. Catatan
- Tabel security memakai ringkasan JSON **terbaru** per skenario/config karena `security_raw.csv` append-only memuat riwayat beberapa run; data mentah lama tetap disimpan.
- Data kinerja berasal dari working tree `dccb182` dengan 8 file kotor. Sebelum penyerahan final, commit perubahan yang disetujui lalu regenerate `env.json` dari commit bersih; jangan mengubah data mentah.
- Definition of Done yang belum tertutup: commit bersih + `env.json` final. Seluruh output eksperimen lain sudah tersedia untuk CHECKPOINT 2.
