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


