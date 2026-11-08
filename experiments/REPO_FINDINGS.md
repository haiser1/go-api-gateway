# Temuan Rekonstruksi Repositori (Repo Findings) — Go API Gateway

> Dokumen ini disusun untuk memenuhi **FASE 0 — Rekonstruksi repo (read-only)** pada dokumen `implementation-plan-test.md`.
> Seluruh analisis dan bukti baris kode didasarkan pada audit langsung terhadap branch `main` pada commit saat ini.

---

## 1. Analisis Validasi API Key

### a. Nama Header
* **Ketetapan / Konfigurasi:** Nama header bawaan (*default*) adalah `X-API-Key`, didefinisikan sebagai konstanta `DefaultAPIKeyHeader` di [`internal/config/auth.go#L29`](file:///home/mark/Development/go-api-gateway/internal/config/auth.go#L29).
* **Kustomisasi:** Header dapat disesuaikan melalui field konfigurasi `api_key.header` di struct `APIKeyAuthConfig` ([`internal/config/auth.go#L44`](file:///home/mark/Development/go-api-gateway/internal/config/auth.go#L44)). Jika dikosongkan, fungsi `ApplyDefaults()` mengisi nilai default `X-API-Key` ([`internal/config/auth.go#L101-L104`](file:///home/mark/Development/go-api-gateway/internal/config/auth.go#L101-L104)).

### b. Tempat Penyimpanan
* **Penyimpanan Kredensial:** Kredensial API key **tidak disimpan** di dalam file konfigurasi `config.yaml`. Jika pengguna mencoba menuliskan API key mentah (plaintext) di `config.yaml`, parser konfigurasi langsung menolaknya dengan error (`"plaintext api_key is not allowed in config.yaml; use api_key.secret_ref"`) di [`internal/config/auth.go#L230-L233`](file:///home/mark/Development/go-api-gateway/internal/config/auth.go#L230-L233).
* **Secret Provider:** Nilai kredensial disimpan pada provider rahasia (*secret provider*), secara default berbasis file lingkungan (*dotenv*) di path `configs/.env` atau path yang ditunjuk oleh environment variable `GATEWAY_ENV_FILE` ([`internal/secrets/env_file.go#L18-L32`](file:///home/mark/Development/go-api-gateway/internal/secrets/env_file.go#L18-L32)).
* **Kunci Referensi:** Nilai secret API key diakses menggunakan referensi environment key `API_KEY_HASH` (didefinisikan di [`internal/secrets/keys.go#L5`](file:///home/mark/Development/go-api-gateway/internal/secrets/keys.go#L5)) atau legacy reference `API_KEY_SECRET` ([`internal/config/auth.go#L26`](file:///home/mark/Development/go-api-gateway/internal/config/auth.go#L26)).

### c. Mekanisme Hashing dan Komparasi
* **Format Hash:** Kredensial API key disimpan dalam bentuk digest SHA-256 kanonikal berawalan prefix `sha256:` ([`internal/secrets/hash.go#L11-L19`](file:///home/mark/Development/go-api-gateway/internal/secrets/hash.go#L11-L19)).
* **Komparasi Constant Time:** Saat inisialisasi verifier `NewAPIKeyVerifier(stored, header)`, jika nilai stored berupa hash kanonikal maka di-parse ke byte array `[32]byte`; jika masih berupa plaintext (legacy), nilainya di-hash sekali pada saat konstruksi ([`internal/auth/api_key.go#L29-L40`](file:///home/mark/Development/go-api-gateway/internal/auth/api_key.go#L29-L40)).
* Pada saat verifikasi per-request (`Verify(raw)`), raw API key dari request di-hash menggunakan SHA-256 lalu dicocokkan dengan expected hash menggunakan `subtle.ConstantTimeCompare` ([`internal/auth/api_key.go#L49-L54`](file:///home/mark/Development/go-api-gateway/internal/auth/api_key.go#L49-L54) dan [`internal/secrets/hash.go#L43-L48`](file:///home/mark/Development/go-api-gateway/internal/secrets/hash.go#L43-L48)), sehingga terlindung dari serangan *timing side-channel*.

### d. Mekanisme Penonaktifan dan Pencabutan (Revocation)
* **Rotasi / Pencabutan via Admin API:** Admin dapat melakukan pembaruan/rotasi API key melalui Management API endpoint `PUT /api/config/auth/api-key` ([`internal/handler/auth_handler.go#L33-L59`](file:///home/mark/Development/go-api-gateway/internal/handler/auth_handler.go#L33-L59)). Handler ini menerima secret baru, menghitung SHA-256 (`secrets.HashSecret`), menyimpannya ke secret provider, dan memicu reload runtime gateway secara otomatis.
* **Penonaktifan via File / Config:** Nilai `API_KEY_HASH` pada file `.env` dapat diubah atau dihapus via `DeleteMany` ([`internal/secrets/env_file.go#L287-L322`](file:///home/mark/Development/go-api-gateway/internal/secrets/env_file.go#L287-L322)), atau dengan menyetel `api_key.enabled: false` di `config.yaml`.
* **Cakupan Multi-Key:** Gateway saat ini mendukung **1 active machine API key** per konfigurasi gateway. Tidak ada tabel multitenant (banyak pengguna dengan key ID berbeda) dalam memori gateway saat ini.

### e. Kombinasi dengan JWT
* **Dukungan Mode:** Konfigurasi auth mendukung tiga mode: `"api_key"`, `"jwt"`, dan `"both"` ([`internal/config/auth.go#L11-L16`](file:///home/mark/Development/go-api-gateway/internal/config/auth.go#L11-L16)).
* **Kombinasi pada Mode "both":** Pada mode `"both"`, API key dan JWT dapat dan wajib dikombinasikan pada rute yang sama ([`internal/auth/runtime.go#L38-L50`](file:///home/mark/Development/go-api-gateway/internal/auth/runtime.go#L38-L50)).
* **Urutan Validasi:** Pada fungsi `Authenticate(req)` di [`internal/auth/runtime.go#L122-L157`](file:///home/mark/Development/go-api-gateway/internal/auth/runtime.go#L122-L157):
  1. API key diperiksa pertama kali. Jika header tidak ada, mengembalikan `ErrAPIKeyRequired` (401). Jika tidak cocok, mengembalikan `ErrInvalidAPIKey` (401).
  2. Hanya jika API key valid, validasi dilanjutkan ke JWT. Jika header Authorization tidak ada, mengembalikan `ErrAuthorizationHeaderRequired` (401). Jika format bukan Bearer, mengembalikan `ErrInvalidAuthorizationHeader` (401). Jika token tidak valid / kedaluwarsa, mengembalikan `ErrInvalidToken` (401).

---

## 2. Analisis Validasi JWT

### a. Algoritma yang Didukung
* Mendukung algoritma simetris `HS256` dan asimetris `RS256` ([`internal/config/auth.go#L19-L22`](file:///home/mark/Development/go-api-gateway/internal/config/auth.go#L19-L22), [`internal/auth/jwt.go#L36-L74`](file:///home/mark/Development/go-api-gateway/internal/auth/jwt.go#L36-L74)).

### b. Sumber Secret dan Kunci Publik
* **HS256:** Secret simetris minimal 32 byte ([`internal/auth/jwt.go#L37-L39`](file:///home/mark/Development/go-api-gateway/internal/auth/jwt.go#L37-L39)), diambil dari secret provider melalui referensi `hs256_secret_ref` (konstanta `JWTHS256SecretKey = "JWT_HS256_SECRET"` di [`internal/secrets/keys.go#L6`](file:///home/mark/Development/go-api-gateway/internal/secrets/keys.go#L6) atau default `JWT_SECRET` di [`internal/config/auth.go#L27`](file:///home/mark/Development/go-api-gateway/internal/config/auth.go#L27)).
* **RS256:** Kunci publik RSA minimal 2048-bit ([`internal/auth/jwt.go#L59-L61`](file:///home/mark/Development/go-api-gateway/internal/auth/jwt.go#L59-L61)), berformat PEM atau Base64-encoded PEM (mendukung PKCS#1 dan PKIX `PUBLIC KEY`) yang di-parse oleh `ParseRSAPublicKey` ([`internal/auth/jwt.go#L132-L179`](file:///home/mark/Development/go-api-gateway/internal/auth/jwt.go#L132-L179)). Diambil melalui referensi `rs256_public_key_ref` (konstanta `JWTRS256PublicKeyKey = "JWT_RS256_PUBLIC_KEY"` di [`internal/secrets/keys.go#L7`](file:///home/mark/Development/go-api-gateway/internal/secrets/keys.go#L7) atau default `JWT_RSA_PUBLIC_KEY` di [`internal/config/auth.go#L28`](file:///home/mark/Development/go-api-gateway/internal/config/auth.go#L28)).

### c. Klaim yang Diperiksa
Pada `JWTVerifier.Verify(tokenString)` ([`internal/auth/jwt.go#L85-L128`](file:///home/mark/Development/go-api-gateway/internal/auth/jwt.go#L85-L128)):
* **`exp` (Expiration Time):** Divalidasi secara standar oleh library `github.com/golang-jwt/jwt/v5` melalui `jwt.ParseWithClaims`. Token kedaluwarsa menghasilkan error `ErrInvalidToken`.
* **`nbf` (Not Before):** Divalidasi otomatis oleh library `golang-jwt`. Jika waktu saat ini sebelum `nbf`, token ditolak.
* **`iss` (Issuer):** Jika `opts.Issuer` dikonfigurasi, parser menambahkan opsi `jwt.WithIssuer(v.opts.Issuer)` ([`internal/auth/jwt.go#L97-L99`](file:///home/mark/Development/go-api-gateway/internal/auth/jwt.go#L97-L99)).
* **`aud` (Audience):** Jika `opts.Audience` dikonfigurasi, parser menambahkan opsi `jwt.WithAudience(v.opts.Audience)` ([`internal/auth/jwt.go#L100-L102`](file:///home/mark/Development/go-api-gateway/internal/auth/jwt.go#L100-L102)).
* **`kid` (Key ID):** Jika `opts.KeyID` dikonfigurasi, `v.keyFunc` memeriksa kesesuaian header `kid` ([`internal/auth/jwt.go#L46-L48, L68-L70`](file:///home/mark/Development/go-api-gateway/internal/auth/jwt.go#L46-L48)).
* **Ekstraksi Hasil:** Setelah verifikasi lolos, nilai klaim (`Subject`, `Issuer`, `Audiences`, `Claims`, `ExpiresAt`, `NotBefore`) dikemas ke dalam `UserIdentity` ([`internal/auth/jwt.go#L109-L127`](file:///home/mark/Development/go-api-gateway/internal/auth/jwt.go#L109-L127)).

### d. Algoritma Di-Pin (*Algorithm Pinning*)
* **Status: DI-PIN KETAT.**
* Pada pembuatan parser options:
  ```go
  options := []jwt.ParserOption{
      jwt.WithValidMethods([]string{v.opts.Algorithm}),
  }
  ```
  ([`internal/auth/jwt.go#L91-L93`](file:///home/mark/Development/go-api-gateway/internal/auth/jwt.go#L91-L93)).
* Pada callback `keyFunc`:
  ```go
  // Pada HS256:
  if token.Method.Alg() != "HS256" {
      return nil, ErrInvalidToken
  }
  // Pada RS256:
  if token.Method.Alg() != "RS256" {
      return nil, ErrInvalidToken
  }
  ```
  ([`internal/auth/jwt.go#L42-L45`](file:///home/mark/Development/go-api-gateway/internal/auth/jwt.go#L42-L45) dan [`internal/auth/jwt.go#L64-L67`](file:///home/mark/Development/go-api-gateway/internal/auth/jwt.go#L64-L67)).
* **Ketahanan:** Token dengan `alg: "none"` langsung ditolak. Serangan *algorithm confusion* (misalnya penyerang menandatangani token HS256 menggunakan public key RSA) ditolak karena parser options maupun `keyFunc` hanya menerima metode yang sesuai dengan konfigurasi aktif.

### e. Toleransi Clock Skew (*Leeway*)
* Parameter `leeway` dikonfigurasi pada `JWTAuthConfig.Leeway` (string format durasi Go, contoh: `"5s"` atau `"1m"`) ([`internal/config/auth.go#L58`](file:///home/mark/Development/go-api-gateway/internal/config/auth.go#L58)).
* Di-parse melalui `LeewayDuration()` ([`internal/config/auth.go#L139-L151`](file:///home/mark/Development/go-api-gateway/internal/config/auth.go#L139-L151)) dan diterapkan ke parser `jwt.WithLeeway(v.opts.Leeway)` ([`internal/auth/jwt.go#L94-L96`](file:///home/mark/Development/go-api-gateway/internal/auth/jwt.go#L94-L96)).
* Jika kosong/tidak disetel, nilainya adalah 0 (tanpa toleransi).

---

## 3. Urutan Middleware Aktual dan Kode Respons Kegagalan

Urutan eksekusi disusun pada saat kompilasi rute (`buildMiddlewareChain` dan `sortPluginChain`) di [`internal/gateway/routing.go#L322-L356`](file:///home/mark/Development/go-api-gateway/internal/gateway/routing.go#L322-L356) dan [`internal/gateway/routing.go#L480-L507`](file:///home/mark/Development/go-api-gateway/internal/gateway/routing.go#L480-L507).

Urutan eksekusi permintaan masuk dari terluar hingga terdalam adalah sebagai berikut:

| Urutan | Komponen / Plugin | Prioritas | Kode Status & Pesan Respons saat Gagal | Lokasi Kode |
|---|---|---|---|---|
| 0 | Routing Lookup (Radix Tree) | N/A (Core) | `404 Not Found` (`{"error":"Not Found"}`)<br>`405 Method Not Allowed` (`{"error":"Method Not Allowed"}`) | [`internal/gateway/proxy.go#L114-L126`](file:///home/mark/Development/go-api-gateway/internal/gateway/proxy.go#L114-L126) |
| 1 | `metrics` / `prometheus` | 0 | Pasif (mencatat metrik durasi, status, request aktif) | [`internal/middleware/metrics.go#L54-L74`](file:///home/mark/Development/go-api-gateway/internal/middleware/metrics.go#L54-L74) |
| 2 | `cors` | 1 | Jika OPTIONS Preflight: `204 No Content` (langsung return) | [`internal/middleware/cors.go#L128-L139`](file:///home/mark/Development/go-api-gateway/internal/middleware/cors.go#L128-L139) |
| 3 | `logger` / `logging` | 2 | Pasif (mencatat log akses JSON Zerolog) | [`internal/middleware/logger.go#L32-L53`](file:///home/mark/Development/go-api-gateway/internal/middleware/logger.go#L32-L53) |
| 4 | `ip-restriction` / `bot-detection` | 3 | `403 Forbidden` (`{"error":"Forbidden: Client IP or User-Agent is restricted"}`) | [`internal/middleware/ip_restriction.go#L135-L169`](file:///home/mark/Development/go-api-gateway/internal/middleware/ip_restriction.go#L135-L169) |
| 5 | `rate-limiting` | 4 | `429 Too Many Requests` (`{"error":"Rate limit exceeded"}`) | [`internal/middleware/ratelimit.go#L41-L60`](file:///home/mark/Development/go-api-gateway/internal/middleware/ratelimit.go#L41-L60) |
| 6 | `auth` / `authorization` | 5 | `401 Unauthorized` (kredensial hilang / salah / format rusak)<br>`503 Service Unavailable` (jika plugin auth aktif tapi runtime auth nil) | [`internal/auth/middleware.go#L30-L48`](file:///home/mark/Development/go-api-gateway/internal/auth/middleware.go#L30-L48)<br>[`internal/gateway/routing.go#L490-L493`](file:///home/mark/Development/go-api-gateway/internal/gateway/routing.go#L490-L493) |
| 7 | `request-transform` | 6 | Pasif / Mutasi header, query params, response headers | [`internal/middleware/request_transform.go#L121-L164`](file:///home/mark/Development/go-api-gateway/internal/middleware/request_transform.go#L121-L164) |
| 8 | ReverseProxy & Upstream / Circuit Breaker | N/A (Core) | `503 Service Unavailable` (Circuit breaker OPEN)<br>`504 Gateway Timeout` (Upstream timeout / deadline)<br>`502 Bad Gateway` (Upstream connection refused / down) | [`internal/gateway/routing.go#L209-L230`](file:///home/mark/Development/go-api-gateway/internal/gateway/routing.go#L209-L230)<br>[`internal/gateway/httperror.go#L45-L74`](file:///home/mark/Development/go-api-gateway/internal/gateway/httperror.go#L45-L74) |

---

## 4. Analisis Rate Limiter terhadap Kegagalan Autentikasi

* **Pertanyaan:** Apakah kegagalan autentikasi ikut dihitung oleh rate limiter (penting untuk skenario S10)?
* **Temuan:** **YA, IKUT DIHITUNG.**
* **Bukti Teknis:**
  1. Pada `sortPluginChain` ([`internal/gateway/routing.go#L325-L338`](file:///home/mark/Development/go-api-gateway/internal/gateway/routing.go#L325-L338)), `rate-limiting` memiliki prioritas **4**, sedangkan `auth` / `authorization` memiliki prioritas **5**.
  2. Saat rantai handler dibungkus dalam urutan terbalik ([`internal/gateway/routing.go#L480-L507`](file:///home/mark/Development/go-api-gateway/internal/gateway/routing.go#L480-L507)), `rate-limiting` membungkus `auth`. Artinya, setiap request yang tiba dieksekusi oleh `rate-limiting` terlebih dahulu sebelum mencapai `auth`.
  3. Di dalam handler `rate-limiting` ([`internal/middleware/ratelimit.go#L48-L51`](file:///home/mark/Development/go-api-gateway/internal/middleware/ratelimit.go#L48-L51)):
     ```go
     mu.Lock()
     requests[ip]++
     count := requests[ip]
     mu.Unlock()
     ```
     Penghitungan counter dilakukan **sebelum** pemanggilan `next.ServeHTTP(w, r)`.
* **Implikasi Pengujian (S10):** Permintaan yang gagal otentikasi (misal 500 tebakan API key salah dari satu IP) akan menaikkan counter rate limit. Begitu limit tercapai, gateway langsung menolak request berikutnya dengan status **HTTP 429 Too Many Requests**, bahkan sebelum verifikasi API key atau JWT dieksekusi.

---

## 5. Pengaturan Plugin di Tingkat Global / Service / Route dan Prioritasnya

### a. Struktur Hierarki
Gateway mendukung pendaftaran plugin pada 3 tingkatan ([`internal/config/config.go`](file:///home/mark/Development/go-api-gateway/internal/config/config.go)):
1. **Global Plugins:** `Config.GlobalPlugins` ([`config.go#L238`](file:///home/mark/Development/go-api-gateway/internal/config/config.go#L238)) — berlaku default untuk semua service & rute.
2. **Service Plugins:** `Service.Plugins` ([`config.go#L149`](file:///home/mark/Development/go-api-gateway/internal/config/config.go#L149)) — berlaku untuk semua rute di bawah service tersebut.
3. **Route Plugins:** `Route.Plugins` ([`config.go#L205`](file:///home/mark/Development/go-api-gateway/internal/config/config.go#L205)) — berlaku spesifik untuk rute tersebut.

### b. Aturan Penggabungan (*Merging & Override*)
Fungsi `resolvePluginChain` ([`internal/gateway/routing.go#L301-L320`](file:///home/mark/Development/go-api-gateway/internal/gateway/routing.go#L301-L320)) menggabungkan plugin berdasarkan nama (`PluginConfig.Name`):
```go
func resolvePluginChain(route config.Route, service config.Service, globalPlugins []config.PluginConfig) []config.PluginConfig {
    pluginMap := make(map[string]config.PluginConfig)
    for _, p := range globalPlugins { pluginMap[p.Name] = p }
    for _, p := range service.Plugins { pluginMap[p.Name] = p }
    for _, p := range route.Plugins { pluginMap[p.Name] = p }
    ...
}
```
* **Prioritas Penimpaan Konfigurasi:** **Route > Service > Global**. Jika plugin dengan nama yang sama didefinisikan di Global dan Route, konfigurasi di tingkat Route yang digunakan.
* **Perilaku Top-Level Auth:** Jika blok konfigurasi `auth:` didefinisikan di root konfigurasi `config.yaml` (`Config.Auth != nil`), runtime gateway secara otomatis menyuntikkan plugin auth ke seluruh rute ([`internal/gateway/routing.go#L153-L158`](file:///home/mark/Development/go-api-gateway/internal/gateway/routing.go#L153-L158)). Jika konfigurasi auth dispesifikasikan via plugin per-rute (format plugin), rute yang tidak memasang plugin auth akan berfungsi sebagai rute publik (tanpa otentikasi).

---

## 6. Mekanisme Hot Reload

### a. Pemicu Reload
1. **File `config.yaml`:** Dipantau menggunakan `fsnotify` pada direktori file konfigurasi dengan debouncing 100ms ([`internal/config/loader.go#L44, L163-L220`](file:///home/mark/Development/go-api-gateway/internal/config/loader.go#L44)). Begitu file berubah dan hash SHA-256 konten berbeda, reload dipicu via channel `m.Reload`.
2. **File Secret (`.env`):** Dipantau terpisah menggunakan `fsnotify` dengan debouncing 100ms di [`internal/gateway/server.go#L161-L225`](file:///home/mark/Development/go-api-gateway/internal/gateway/server.go#L161-L225).
3. **Management API:** Mutasi via endpoint Admin API (`PUT /api/config/...`, `POST /api/...`, dll.) memanggil `commitConfigLocked` ([`internal/config/loader.go#L307-L336`](file:///home/mark/Development/go-api-gateway/internal/config/loader.go#L307-L336)).

### b. Sifat Atomik (*Atomic Swap*)
* **Validasi Sebelum Commit:** Konfigurasi baru divalidasi dan dibangun terlebih dahulu menjadi kandidat runtime (`validateAndBuildLocked`, [`internal/config/loader.go#L308`](file:///home/mark/Development/go-api-gateway/internal/config/loader.go#L308)). Jika kandidat tidak valid, pembaruan dibatalkan tanpa menyentuh disk atau runtime aktif.
* **Penulisan File Atomik:** Penulisan ke file `config.yaml` menggunakan file temporary (`config.yaml.tmp-*`), di-flush dengan `Sync()`, lalu diganti menggunakan `os.Rename` atomik ([`internal/config/loader.go#L245-L274`](file:///home/mark/Development/go-api-gateway/internal/config/loader.go#L245-L274)).
* **Publikasi Memori Atomik:** Publikasi tabel routing baru ke proxy dilakukan menggunakan pointer atomic swap: `p.snapshot.Swap(snap)` ([`internal/gateway/proxy.go#L60`](file:///home/mark/Development/go-api-gateway/internal/gateway/proxy.go#L60)).

### c. Dampak terhadap Request yang Sedang Berjalan (*In-Flight Requests*)
* **Zero Disruption:** Request yang sedang dieksekusi **tidak terputus**. Setiap request memegang referensi ke snapshot yang sedang aktif saat request dimulai (`snap.trackRequest()` dan `defer snap.releaseRequest()` di [`internal/gateway/proxy.go#L106-L107`](file:///home/mark/Development/go-api-gateway/internal/gateway/proxy.go#L106-L107)).
* **Pelepasan Sumber Daya Bertahap (*Draining*):** Snapshot lama di-retire dan sumber dayanya (koneksi idle transport) baru dibersihkan setelah in-flight counter mencapai nol (`p.retireSnapshot` dan `p.drainRuntime` di [`internal/gateway/proxy.go#L69-L94`](file:///home/mark/Development/go-api-gateway/internal/gateway/proxy.go#L69-L94)).

---

## 7. Port, Format Log, dan Target Makefile Relevan

### a. Port Layanan
* **Data Plane (Proxy Server):** Port `8080` (didefinisikan di `configs/config.yaml:2`, default fallback di [`internal/gateway/server.go#L71-L74`](file:///home/mark/Development/go-api-gateway/internal/gateway/server.go#L71-L74)).
* **Control Plane (Management Admin Server):** Port `8081` (didefinisikan di `configs/config.yaml:3`, default fallback di [`internal/gateway/server.go#L89-L92`](file:///home/mark/Development/go-api-gateway/internal/gateway/server.go#L89-L92)).
* **Mock Server:** Port `9090` (didefinisikan di `mock-server/main.go:43` dan `docker-compose.yml:31`).

### b. Format Log
* Menggunakan structured JSON logging via Zerolog ke `os.Stderr` ([`cmd/api-gateway/main.go#L34-L36`](file:///home/mark/Development/go-api-gateway/cmd/api-gateway/main.go#L34-L36)).
* Setiap permintaan masuk dicatat oleh plugin `logger` ([`internal/middleware/logger.go#L44-L50`](file:///home/mark/Development/go-api-gateway/internal/middleware/logger.go#L44-L50)) dengan field:
  `method`, `uri`, `status`, `duration`, `protocol`, dan `message: "Request processed"`.

### c. Target Makefile Relevan
* `make build`: Kompilasi binary produksi ke `bin/api-gateway` ([`Makefile#L20-L22`](file:///home/mark/Development/go-api-gateway/Makefile#L20-L22)).
* `make test`: Menjalankan suite pengujian Go test ([`Makefile#L24-L26`](file:///home/mark/Development/go-api-gateway/Makefile#L24-L26)).
* `make docker-up` / `docker-down`: Manajemen kontainer Docker Compose ([`Makefile#L33-L40`](file:///home/mark/Development/go-api-gateway/Makefile#L33-L40)).
* `make k6-test`: Menjalankan k6 load test container ([`Makefile#L64-L67`](file:///home/mark/Development/go-api-gateway/Makefile#L64-L67)).
* `make docker-stats`: Memantau penggunaan CPU, Memori, dan Network I/O kontainer secara berkala ([`Makefile#L74-L75`](file:///home/mark/Development/go-api-gateway/Makefile#L74-L75)).
* `make log-analytics`: Agregasi ringkasan level log kontainer ([`Makefile#L77-L80`](file:///home/mark/Development/go-api-gateway/Makefile#L77-L80)).

---

## 8. Tabel Kelayakan Skenario

### a. Pengujian Fungsional (F01–F09)

| ID | Kasus Pengujian | Konfigurasi | Ekspektasi | Status Kelayakan | Bukti & Catatan Teknis |
|---|---|---|---|---|---|
| **F01** | Rute valid, kredensial valid | D | 200, body sama dengan upstream | **Didukung** | Config D (`both`) memvalidasi API key dan JWT sukses; upstream mock mengembalikan 200 ([`mock-server/main.go#L23-L27`](file:///home/mark/Development/go-api-gateway/mock-server/main.go#L23-L27)). |
| **F02** | Rute tidak terdaftar | D | 404 Not Found | **Didukung** | Radix tree mengembalikan `nil`, proxy menulis `404 Not Found` ([`internal/gateway/proxy.go#L124`](file:///home/mark/Development/go-api-gateway/internal/gateway/proxy.go#L124)). |
| **F03** | Rute publik (tanpa plugin auth) tetap bisa diakses | Campuran | 200 OK | **Didukung** | Menggunakan konfigurasi plugin auth per-rute (bukan top-level `auth:`) memungkinkan rute tanpa auth diakses tanpa kredensial ([`internal/gateway/routing.go#L481-L497`](file:///home/mark/Development/go-api-gateway/internal/gateway/routing.go#L481-L497)). |
| **F04** | API key valid saja pada rute yang butuh JWT | D | 401 Unauthorized | **Didukung** | Header Authorization tidak ada -> `ErrAuthorizationHeaderRequired` -> HTTP 401 ([`internal/auth/runtime.go#L143`](file:///home/mark/Development/go-api-gateway/internal/auth/runtime.go#L143)). |
| **F05** | JWT valid saja pada rute yang butuh API key | D | 401 Unauthorized | **Didukung** | Header API key tidak ada -> `ErrAPIKeyRequired` -> HTTP 401 ([`internal/auth/runtime.go#L132`](file:///home/mark/Development/go-api-gateway/internal/auth/runtime.go#L132)). |
| **F06** | Prioritas konfigurasi route > service > global | D | Nilai route menimpa global | **Didukung** | Diuji dengan plugin transform/CORS/ratelimit. `resolvePluginChain` menimpa service dan global ([`internal/gateway/routing.go#L301-L320`](file:///home/mark/Development/go-api-gateway/internal/gateway/routing.go#L301-L320)). |
| **F07** | Hot reload diterapkan tanpa restart | D | Perilaku baru aktif tanpa restart | **Didukung** | Perubahan file `config.yaml` atau `.env` memicu `fsnotify` debounce dan atomic snapshot reload ([`internal/config/loader.go#L163-L220`](file:///home/mark/Development/go-api-gateway/internal/config/loader.go#L163-L220)). |
| **F08** | Upstream mati → circuit breaker terbuka | D | 503 setelah 5 kali gagal, pulih setelah upstream hidup | **Didukung** | `failureThreshold = 5`. Kegagalan 5x berturut-turut membuka circuit breaker -> HTTP 503 ([`internal/gateway/circuit_breaker.go#L25, L154-L162`](file:///home/mark/Development/go-api-gateway/internal/gateway/circuit_breaker.go#L25)). |
| **F09** | Header & body diteruskan ke upstream dengan benar | D | Sama dengan yang dikirim | **Didukung** | ReverseProxy meneruskan header dan request body ([`internal/gateway/routing.go#L390-L478`](file:///home/mark/Development/go-api-gateway/internal/gateway/routing.go#L390-L478)). |

---

### b. Pengujian Keamanan (S01–S12) dan Tambahan (S13–S15)

| ID | Skenario Serangan / Uji Keamanan | Harapan | Status Kelayakan | Bukti & Catatan Teknis |
|---|---|---|---|---|
| **S01** | Tanpa kredensial (tanpa API key dan JWT) | Ditolak (401) | **Didukung** | API key dievaluasi pertama kali; missing header -> `401 API key required` ([`internal/auth/runtime.go#L131-L133`](file:///home/mark/Development/go-api-gateway/internal/auth/runtime.go#L131-L133)). |
| **S02** | API key tidak terdaftar (acak) | Ditolak (401) | **Didukung** | `Verify(raw)` membandingkan hash dan mengembalikan false -> `401 Unauthorized: Invalid API key` ([`internal/auth/runtime.go#L135`](file:///home/mark/Development/go-api-gateway/internal/auth/runtime.go#L135)). |
| **S03** | API key dicabut (rotasi kredensial) | Ditolak (401) | **Didukung** | Key lama dicabut melalui rotasi key via Admin API `PUT /api/config/auth/api-key` atau perubahan `.env`. Key lama langsung ditolak 401 ([`internal/handler/auth_handler.go#L33-L59`](file:///home/mark/Development/go-api-gateway/internal/handler/auth_handler.go#L33-L59)). |
| **S04** | JWT tidak ada (API key valid saja) | Ditolak (401) | **Didukung** | API key valid, namun JWT header kosong -> `401 Authorization header required` ([`internal/auth/runtime.go#L143`](file:///home/mark/Development/go-api-gateway/internal/auth/runtime.go#L143)). |
| **S05** | JWT kedaluwarsa (`exp` telah lewat) | Ditolak (401) | **Didukung** | Library `golang-jwt` memvalidasi klaim `exp` -> `ErrInvalidToken` -> `401 Unauthorized: Invalid token` ([`internal/auth/jwt.go#L104-L107`](file:///home/mark/Development/go-api-gateway/internal/auth/jwt.go#L104-L107)). |
| **S06** | JWT dimodifikasi (`tampered_payload`) | Ditolak (401) | **Didukung** | Verifikasi cryptographic signature gagal -> `ErrInvalidToken` -> `401 Unauthorized: Invalid token` ([`internal/auth/jwt.go#L105`](file:///home/mark/Development/go-api-gateway/internal/auth/jwt.go#L105)). |
| **S07** | `alg=none` (tanpa signature) | Ditolak (401) | **Didukung** | `jwt.WithValidMethods` dan pengecekan eksplisit di `keyFunc` menolak `none` -> `401 Unauthorized: Invalid token` ([`internal/auth/jwt.go#L43, L92`](file:///home/mark/Development/go-api-gateway/internal/auth/jwt.go#L43)). |
| **S08** | Algorithm confusion (HS256 dengan RSA public key) | Ditolak (401) | **Didukung** | Saat verifier dikonfigurasi RS256, token HS256 ditolak oleh parser option `WithValidMethods` dan `keyFunc` -> 401 ([`internal/auth/jwt.go#L65-L67`](file:///home/mark/Development/go-api-gateway/internal/auth/jwt.go#L65-L67)). |
| **S09** | Klaim salah (`wrong_iss`, `wrong_aud`, `nbf_future`) | Ditolak (401) | **Didukung** | `jwt.WithIssuer`, `jwt.WithAudience`, dan validasi `nbf` otomatis menolak token yang klaimnya tidak cocok -> 401 ([`internal/auth/jwt.go#L97-L107`](file:///home/mark/Development/go-api-gateway/internal/auth/jwt.go#L97-L107)). |
| **S10** | Tebakan API key berulang (brute-force rate limit) | Muncul 429 | **Didukung** | `rate-limiting` berjalan sebelum `auth`. 500 tebakan acak menaikkan counter hingga batas terlampaui -> `429 Rate limit exceeded` ([`internal/middleware/ratelimit.go#L48-L56`](file:///home/mark/Development/go-api-gateway/internal/middleware/ratelimit.go#L48-L56)). |
| **S11** | Banjir permintaan sah (rate limit threshold) | 200 lalu 429 | **Didukung** | Request sah dihitung oleh window rate limit; setelah kuota per menit habis, gateway mengembalikan status 429 ([`internal/middleware/ratelimit.go#L53-L57`](file:///home/mark/Development/go-api-gateway/internal/middleware/ratelimit.go#L53-L57)). |
| **S12** | Akses langsung ke upstream tanpa gateway | Gagal dari host | **Didukung** | `mock-server` hanya mem-bind port di Docker network internal, tidak membuka port ke host pada compose ([`docker-compose.test.yml`](file:///home/mark/Development/go-api-gateway/docker-compose.yml)). |
| **S13** *(Opsional)* | *Header spoofing* (`X-User-Id`, `X-Forwarded-For`) | Amati penerusan | **Didukung** | Gateway meneruskan header klien apa adanya ke upstream tanpa sanitasi otomatis (temuan celah keamanan) ([`internal/gateway/routing.go#L375-L400`](file:///home/mark/Development/go-api-gateway/internal/gateway/routing.go#L375-L400)). |
| **S14** *(Opsional)* | Token malformed & oversized | Gateway tidak panic | **Didukung** | Format token rusak di-handle aman oleh `bearerToken` & `golang-jwt`. Header raksasa (>64KB/1MB) ditolak aman oleh net/http tanpa panic ([`internal/auth/runtime.go#L173-L183`](file:///home/mark/Development/go-api-gateway/internal/auth/runtime.go#L173-L183)). |
| **S15** *(Opsional)* | Timing side-channel pada API key | Ukur latensi key | **Didukung** | Hashing SHA-256 + `subtle.ConstantTimeCompare` memastikan waktu perbandingan konstan ([`internal/secrets/hash.go#L43-L48`](file:///home/mark/Development/go-api-gateway/internal/secrets/hash.go#L43-L48)). |

---

## 9. Daftar Celah (*Gaps*) dan Keterbatasan Arsitektur yang Ditemukan

Berikut adalah temuan celah teknis dan batasan arsitektur pada kode gateway saat ini (dicatat apa adanya sesuai aturan Fase 0 tanpa mengubah kode inti gateway):

1. **Model API Key Tunggal (Single-Tenant Machine Credential):**
   * *Temuan:* Gateway hanya menyimpan satu `expected` API key hash per runtime ([`internal/auth/api_key.go#L15`](file:///home/mark/Development/go-api-gateway/internal/auth/api_key.go#L15)).
   * *Dampak:* Gateway belum memiliki database/tabel multi-client API keys (misal key khusus Client A, key khusus Client B). Pencabutan kredensial berarti merotasi satu-satunya key gateway yang berdampak ke seluruh klien mesin.

2. **Kekakuan Blok `auth:` Top-Level (Inability to Exclude Public Routes):**
   * *Temuan:* Jika konfigurasi menggunakan blok `auth:` di level teratas `config.yaml`, runtime gateway menyuntikkan plugin auth ke **seluruh** rute secara mutlak ([`internal/gateway/routing.go#L153-L158`](file:///home/mark/Development/go-api-gateway/internal/gateway/routing.go#L153-L158)).
   * *Dampak:* Tidak ada mekanisme *bypass* rute publik (misal `/health` atau `/public`) jika `auth:` diaktifkan di level teratas, kecuali rute tersebut dipisahkan menggunakan format plugin per-rute.

3. **Tidak Ada Sanitasi Header Identitas Masuk (*Identity Header Spoofing Risk*):**
   * *Temuan:* Gateway tidak membersihkan (*strip*) header identitas seperti `X-User-Id` atau klaim peran yang dikirim oleh klien sebelum mem-proxy ke upstream ([`internal/gateway/routing.go#L390-L400`](file:///home/mark/Development/go-api-gateway/internal/gateway/routing.go#L390-L400)).
   * *Dampak:* Jika layanan microservice di upstream mempercayai header `X-User-Id` tanpa validasi, penyerang dapat memanipulasi identitas pengguna lain meskipun token JWT yang dikirim valid.

4. **Ketiadaan Mekanisme Pencabutan JWT Seketika (*No Token Blacklist / Denylist*):**
   * *Temuan:* Verifikasi JWT murni bersifat *stateless* (memeriksa signature dan klaim waktu `exp`/`nbf`). Gateway tidak memiliki pengecekan terhadap daftar pencabutan token (misal Redis blacklist untuk `jti`).
   * *Dampak:* Token JWT yang telah dicuri tetap valid hingga masa berlakunya (`exp`) habis, kecuali jika signing secret dirotasi secara global.

5. **Rate Limiting Berbasis Memori Lokal & IP Tunggal (*In-Memory Fixed Window*):**
   * *Temuan:* Plugin `rate-limiting` menggunakan in-memory map Go (`map[string]int`) dengan penguncian mutex lokal ([`internal/middleware/ratelimit.go#L26-L39`](file:///home/mark/Development/go-api-gateway/internal/middleware/ratelimit.go#L26-L39)).
   * *Dampak:* Jika gateway di-deploy multi-instance, limit tidak terdistribusi (belum menggunakan Redis di binary utama). Selain itu, identifikasi hanya berdasarkan `r.RemoteAddr`, bukan identitas API key atau subjek JWT.

---
*Laporan Fase 0 selesai disusun.*

