# Implementation Plan — Go API Gateway
## Runtime Architecture, Hot Reload, Transport & Reliability Improvements

**Project:** `go-api-gateway`  
**Objective:** Meningkatkan konsistensi runtime, keamanan concurrency, dan reliability API Gateway, khususnya pada mekanisme hot reload, routing, transport management, dan circuit breaker.

---

# 1. Objectives

Implementasi ini bertujuan untuk:

1. Memastikan seluruh komponen runtime menggunakan versi konfigurasi yang konsisten.
2. Mengimplementasikan immutable runtime snapshot dengan atomic pointer swap.
3. Memperbaiki transport cache agar perubahan konfigurasi benar-benar diterapkan.
4. Mengelola lifecycle transport ketika konfigurasi berubah atau service dihapus.
5. Memastikan request yang sedang berjalan tetap aman ketika terjadi hot reload.
6. Memperkuat circuit breaker untuk concurrent requests dan upstream failures.
7. Memperbaiki HTTP error handling dan routing behavior.
8. Menambahkan automated tests untuk concurrency, reload, dan resource lifecycle.

## Prinsip implementasi

- Jangan melakukan rewrite seluruh project.
- Pertahankan arsitektur dan fitur yang sudah berjalan.
- Lakukan perubahan secara incremental.
- Hindari breaking changes pada konfigurasi YAML kecuali memang diperlukan.
- Jangan mengorbankan request yang sedang berjalan ketika reload.
- Semua konfigurasi baru harus divalidasi sebelum dipublikasikan.
- Setiap perubahan harus disertai unit test atau integration test yang relevan.

---

# 2. Target Architecture

Arsitektur runtime yang ingin dicapai:

```text
                 CONFIGURATION
                       |
                       v
                 Config Manager
                       |
             Load / Validate / Parse
                       |
                       v
                  Runtime Builder
                       |
          +------------+-------------+
          |            |             |
          v            v             v
       Radix Tree   Middleware    Transports
          |            |             |
          +------------+-------------+
                       |
                       v
                Runtime Snapshot
                       |
                 Atomic Pointer
                       |
                       v
                 Proxy Handler
                       |
                 ServeHTTP()
                       |
                       v
                  Upstream
```

Runtime snapshot harus menjadi satu kesatuan yang immutable setelah dipublikasikan.

Contoh struktur konseptual:

```go
type RuntimeSnapshot struct {
    Version uint64

    Tree *RadixTree

    // Runtime dependencies:
    // - Compiled routes
    // - Middleware handlers
    // - Upstream selectors
    // - HTTP transports
    // - Other required runtime components
}

type Proxy struct {
    snapshot atomic.Pointer[RuntimeSnapshot]
}
```

Catatan:
- Struktur final harus disesuaikan dengan implementasi yang sudah ada.
- Jangan membuat dependency yang sudah tersedia menjadi duplikat.
- Circuit breaker boleh memiliki lifecycle terpisah jika memang perlu mempertahankan state antar-reload.
- Setelah snapshot dipublikasikan, jangan memodifikasi data yang menjadi bagian dari snapshot tersebut.

---

# 3. Implementation Phases

## Phase 1 — Audit Existing Implementation

**Goal:** Memahami struktur project sebelum melakukan perubahan.

Tasks:

1. Audit `Proxy`, `RuntimeSnapshot`, dan `buildRoutingTable()`.
2. Audit `Config Manager`, termasuk:
   - `loadConfigFromFile()`
   - `reloadFromFileIfChanged()`
   - `commitConfigLocked()`
   - `WriteConfigSafe()`
   - `watchConfig()`
3. Audit transport management:
   - `GetServiceTransport()`
   - `createTransport()`
   - `transportEntry`
4. Audit circuit breaker implementation.
5. Audit plugin lifecycle dan middleware compilation.
6. Identifikasi seluruh global mutable state.
7. Identifikasi seluruh dependency yang masih diakses melalui global registry ketika request berlangsung.

Deliverables:

- Existing architecture summary.
- Dependency and lifecycle map.
- List of concurrency risks.
- Proposed minimal refactoring plan.

Acceptance criteria:

- Tidak ada perubahan behavior pada fase audit.
- Seluruh dependency runtime berhasil diidentifikasi.
- Agent memahami jalur config sampai request diteruskan ke upstream.

---

## Phase 2 — Fix Transport Cache Concurrency

**Priority: P0**

**Goal:** Memperbaiki bug pada `GetServiceTransport()` ketika konfigurasi timeout berubah.

Current issue:

Penggunaan `sync.Map.LoadOrStore()` tidak mengganti entry lama. Akibatnya, transport dengan timeout lama dapat terus digunakan walaupun konfigurasi sudah berubah.

Tasks:

1. Ganti mekanisme cache menjadi thread-safe replacement.
2. Gunakan mutex dengan double-check locking atau implementasi atomic replacement yang benar.
3. Pastikan transport baru menggantikan transport lama ketika konfigurasi berubah.
4. Jangan menutup transport baru yang berhasil dibuat.
5. Tutup idle connections dari transport lama setelah replacement.
6. Pertimbangkan seluruh konfigurasi yang memengaruhi transport, bukan hanya `ConnectTimeout` dan `ReadTimeout`.
7. Tambahkan logging untuk transport creation dan replacement.

Suggested implementation:

```go
type transportEntry struct {
    transport *http.Transport

    // Fingerprint of transport-related configuration.
    configHash string
}

var (
    serviceTransports = make(map[string]*transportEntry)
    transportMu       sync.RWMutex
)
```

Buat fungsi untuk menghasilkan fingerprint dari konfigurasi transport.

Fingerprint minimal mencakup seluruh field yang memengaruhi perilaku transport, seperti:

- Connect timeout.
- TLS configuration.
- Connection pool limits.
- Keep-alive configuration.
- Proxy configuration.
- HTTP protocol configuration.

Jangan memasukkan timeout yang tidak relevan terhadap transport ke fingerprint.

Tests:

1. Transport dibuat dengan timeout 5 detik.
2. Config berubah menjadi 10 detik.
3. Panggil `GetServiceTransport()` kembali.
4. Pastikan transport yang dikembalikan berbeda dari transport sebelumnya.
5. Pastikan transport baru menggunakan konfigurasi terbaru.
6. Jalankan concurrent calls untuk service yang sama.
7. Jalankan `go test -race`.

Acceptance criteria:

- Transport benar-benar terganti ketika konfigurasi berubah.
- Tidak terjadi duplicate replacement yang tidak diperlukan.
- Tidak ada data race pada cache.
- Request yang masih menggunakan transport lama tidak mengalami panic atau forced interruption.

---

## Phase 3 — Introduce Unified Runtime Snapshot

**Priority: P0**

**Goal:** Menyatukan seluruh dependency runtime dalam satu immutable snapshot.

Tasks:

1. Buat struktur runtime snapshot.
2. Pindahkan dependency runtime yang diperlukan dari global state ke snapshot.
3. Pastikan snapshot mencakup:
   - Radix Tree.
   - Compiled routes.
   - Middleware handlers.
   - Upstream runtime/selectors.
   - HTTP transports.
   - Runtime metadata/version.
4. Tentukan lifecycle circuit breaker:
   - Per runtime, atau
   - Shared registry dengan identitas service/upstream yang stabil.
5. Pastikan setiap `CompiledHandler` menggunakan dependency dari runtime yang sama.
6. Pastikan request tidak melakukan lookup terhadap konfigurasi global yang dapat berubah.
7. Pertahankan atomic pointer swap.

Expected flow:

```go
func (p *Proxy) UpdateRoutes(cfg *config.Config) error {
    runtime, err := BuildRuntime(cfg)
    if err != nil {
        return err
    }

    p.snapshot.Store(runtime)

    return nil
}
```

Catatan:
- Signature di atas merupakan contoh target. Sesuaikan dengan API yang ada.
- Jangan publish snapshot jika proses build atau validasi gagal.
- Snapshot lama harus tetap aktif ketika build snapshot baru gagal.

Tests:

1. Build runtime versi 1.
2. Publish runtime versi 1.
3. Kirim request menggunakan runtime versi 1.
4. Build dan publish runtime versi 2.
5. Pastikan request baru menggunakan runtime versi 2.
6. Pastikan request lama dapat menyelesaikan proses menggunakan runtime versi 1.
7. Simulasikan build failure dan pastikan runtime versi 1 tetap aktif.

Acceptance criteria:

- Snapshot hanya dipublikasikan setelah seluruh dependency berhasil dibuat.
- Tidak ada partial runtime publication.
- Request membaca snapshot sekali pada awal `ServeHTTP()`.
- Failed reload tidak merusak runtime aktif.

---

## Phase 4 — Implement Runtime Resource Lifecycle

**Priority: P1**

**Goal:** Mengelola transport dan resource runtime lama secara aman.

Current issue:

Transport disimpan pada global registry sehingga service yang sudah dihapus dari konfigurasi berpotensi meninggalkan resource yang tidak digunakan.

Tasks:

1. Tentukan ownership transport.
2. Jika transport menjadi bagian runtime snapshot, pastikan runtime lama dapat dipensiunkan.
3. Jangan menutup transport lama ketika masih digunakan request yang sedang berjalan.
4. Buat mekanisme retirement atau cleanup runtime.
5. Bersihkan idle connections dari transport yang tidak lagi digunakan.
6. Pastikan transport yang masih digunakan oleh runtime aktif tidak ikut dibersihkan.
7. Tangani service yang dihapus dari konfigurasi.
8. Pastikan cleanup dapat dijalankan berulang tanpa efek samping.

Important considerations:

- Atomic pointer swap tidak otomatis mengelola lifecycle resource.
- Request lama harus tetap dapat menyelesaikan pekerjaannya.
- `CloseIdleConnections()` hanya menutup koneksi yang sedang idle.
- Jangan mengandalkan atomic pointer saja untuk mengetahui bahwa semua request lama sudah selesai.

Pertimbangkan mekanisme reference counting atau runtime lease jika cleanup perlu mengetahui kapan runtime lama sudah tidak digunakan.

Tests:

- Reload saat request upstream masih berlangsung.
- Hapus service dari konfigurasi.
- Pastikan request lama tetap selesai.
- Pastikan resource lama akhirnya dibersihkan.
- Pastikan cleanup tidak menutup transport runtime aktif.

Acceptance criteria:

- Tidak ada premature cleanup.
- Tidak ada transport yang tertinggal tanpa lifecycle yang jelas.
- Request in-flight tetap aman selama reload.

---

## Phase 5 — Improve Config Manager and Reload Consistency

**Priority: P0**

**Goal:** Memastikan file config, config memory, dan runtime snapshot tidak mengalami ketidakkonsistenan.

Tasks:

1. Perbaiki race condition pada `reloadFromFileIfChanged()`.
2. Koordinasikan file read dan internal config commit agar watcher tidak memublikasikan data stale.
3. Implementasikan atomic file write:
   - Write temporary file pada direktori yang sama.
   - Set file permission.
   - Write data.
   - Sync file.
   - Close file.
   - Rename temporary file ke file config.
   - Sync parent directory jika diperlukan.
4. Pastikan backup failure tidak diabaikan jika backup diwajibkan.
5. Daftarkan watcher secara synchronous ketika inisialisasi manager.
6. Jika watcher gagal diinisialisasi, kembalikan error dari `NewManager()`.
7. Tangani file removal dan rename.
8. Tambahkan lifecycle shutdown untuk watcher.
9. Pastikan `WriteConfigSafe()` dan `commitConfigLocked()` memiliki semantik yang jelas.
10. Jangan expose mutable config pointer kepada consumer.
11. Tambahkan strict YAML decoding dan semantic validation.

Validation minimal:

- Duplicate route.
- Invalid path pattern.
- Unknown service reference.
- Unknown upstream reference.
- Invalid target.
- Invalid timeout.
- Invalid plugin.
- Invalid plugin configuration.
- Invalid load balancing configuration.

Important:

Build runtime terlebih dahulu sebelum memublikasikan config baru.

Expected flow:

```text
Read Config
    |
    v
Parse YAML
    |
    v
Validate Config
    |
    v
Build Runtime
    |
    v
Persist Config (for admin updates)
    |
    v
Publish Config + Runtime
    |
    v
Notify Reload Consumers
```

Urutan persistence dan publication perlu disesuaikan dengan sumber perubahan. Untuk admin update, jangan publish konfigurasi yang gagal dipersist jika kebijakan sistem mengharuskan disk dan memory selalu konsisten.

Acceptance criteria:

- Invalid config tidak menggantikan config aktif.
- Failed runtime build tidak menggantikan runtime aktif.
- Concurrent internal update dan file watcher tidak menghasilkan stale runtime.
- File config tidak terbaca dalam kondisi partial write.
- Watcher dapat dihentikan dengan bersih.

---

## Phase 6 — Circuit Breaker Reliability

**Priority: P1**

**Goal:** Memastikan circuit breaker aman untuk concurrent requests dan memiliki failure classification yang tepat.

Tasks:

1. Audit thread safety:
   - `AllowRequest()`
   - `RecordFailure()`
   - `RecordSuccess()`
2. Pastikan state transition konsisten:
   - CLOSED
   - OPEN
   - HALF-OPEN
3. Batasi jumlah concurrent probe pada HALF-OPEN.
4. Definisikan failure classification:
   - Network error: failure.
   - Upstream 5xx: failure.
   - Upstream 4xx: default success.
   - 429: configurable atau kebijakan eksplisit.
   - Client cancellation: jangan otomatis dianggap upstream failure.
5. Pastikan timeout upstream dapat dihitung sebagai failure.
6. Pastikan circuit breaker state tidak ter-reset tanpa alasan ketika konfigurasi reload.
7. Tambahkan observability untuk state transition.

Retry consideration:

Tentukan apakah circuit breaker menghitung setiap upstream attempt atau setiap request dari client.

Rekomendasi awal: hitung setiap upstream attempt karena breaker melindungi upstream dari percobaan yang terus gagal.

Tests:

- Failure threshold.
- Open state.
- Rejected request ketika OPEN.
- HALF-OPEN probe.
- Recovery setelah success.
- Concurrent requests.
- Context cancellation.
- Reload saat breaker berada pada OPEN.

Acceptance criteria:

- Tidak ada data race.
- Tidak ada probe berlebihan pada HALF-OPEN.
- State transition deterministik.
- Error circuit breaker dapat dikenali oleh proxy.

---

## Phase 7 — Reverse Proxy and HTTP Behavior

**Priority: P2**

**Goal:** Memperbaiki HTTP behavior dan konsistensi error response.

Tasks:

1. Buat JSON error response helper.
2. Ganti penggunaan `http.Error()` untuk response yang seharusnya JSON.
3. Implementasikan HTTP 405 untuk path yang tersedia tetapi method tidak sesuai.
4. Tambahkan `Allow` header untuk HTTP 405.
5. Pastikan `X-Forwarded-Host` menggunakan `r.Host`, bukan `r.Header.Get("Host")`.
6. Audit sanitasi dan trust policy untuk forwarding headers.
7. Pastikan circuit breaker error diterjemahkan menjadi HTTP 503.
8. Pastikan upstream/network error diterjemahkan menjadi HTTP 502.
9. Pastikan tidak ada response writer yang ditulis dua kali.

Suggested error mapping:

| Condition | HTTP Status |
|---|---:|
| Service not ready | 503 |
| Route not found | 404 |
| Method not allowed | 405 |
| Circuit breaker open | 503 |
| Upstream connection failure | 502 |
| Upstream timeout | 504 |

Catatan: mapping ini harus konsisten dengan kebijakan gateway dan tidak mengubah status response upstream yang memang berhasil diteruskan.

Tests:

- 404.
- 405 + Allow header.
- 502.
- 503.
- 504.
- JSON Content-Type.
- Forwarded headers.

---

## Phase 8 — Load Balancer and Upstream Runtime

**Priority: P1**

**Goal:** Memastikan konfigurasi load balancing benar-benar digunakan oleh request.

Current concern:

Pada implementasi sebelumnya, routing mengambil:

```go
upstream.Targets[0]
```

Hal ini menyebabkan target pertama selalu digunakan dan konfigurasi load balancing belum benar-benar berjalan.

Tasks:

1. Buat upstream runtime/selector.
2. Jangan memilih target hanya ketika routing table dibangun.
3. Pilih target pada saat request berlangsung.
4. Implementasikan health-aware target selection.
5. Implementasikan algoritma awal:
   - Round Robin.
6. Persiapkan extensibility untuk:
   - Weighted Round Robin.
   - Least Connections.
   - Consistent Hashing.
7. Pastikan target unhealthy tidak dipilih.
8. Definisikan behavior ketika semua target unhealthy.
9. Pastikan target selection thread-safe.
10. Integrasikan retry dengan target selection.

Acceptance criteria:

- Request terdistribusi ke target sesuai algoritma.
- Target unhealthy tidak menerima request baru.
- Retry dapat memilih target lain jika kebijakan mengizinkan.
- Tidak ada data race pada selector.

---

# 4. Testing Strategy

Semua fase harus memiliki pengujian yang sesuai.

## Unit Tests

Fokus pada:

- Transport cache replacement.
- Runtime builder.
- Config validation.
- Circuit breaker.
- Route lookup.
- Error mapping.
- Load balancer.

## Integration Tests

Fokus pada:

- Config reload.
- Concurrent requests during reload.
- Upstream timeout.
- Upstream failure.
- Retry behavior.
- Circuit breaker behavior.
- Transport replacement.
- Service removal.

## Concurrency Tests

Wajib menjalankan:

```bash
go test -race ./...
```

Tambahkan stress test untuk:

- Concurrent `ServeHTTP()`.
- Concurrent runtime reload.
- Concurrent transport retrieval.
- Concurrent circuit breaker access.
- Config watcher dan admin config update secara bersamaan.

## Benchmark

Bandingkan performa sebelum dan sesudah refactoring.

Benchmark minimal:

- 100 routes.
- 1,000 routes.
- 10,000 routes.
- Static path.
- Parameter path.
- Wildcard path.
- Middleware execution.
- Runtime snapshot lookup.

Gunakan:

```bash
go test -bench=. -benchmem ./...
```

Jangan mengasumsikan optimasi berhasil tanpa hasil benchmark.

---

# 5. Suggested Implementation Order

| Order | Phase | Priority |
|---:|---|---|
| 1 | Audit Existing Implementation | P0 |
| 2 | Fix Transport Cache | P0 |
| 3 | Unified Runtime Snapshot | P0 |
| 4 | Config Manager Consistency | P0 |
| 5 | Runtime Resource Lifecycle | P1 |
| 6 | Circuit Breaker Reliability | P1 |
| 7 | Load Balancer and Upstream Runtime | P1 |
| 8 | Reverse Proxy HTTP Behavior | P2 |
| 9 | Integration, Race Tests & Benchmarks | P0 |

Catatan: testing harus berjalan pada setiap fase, bukan hanya di akhir.

---

# 6. Definition of Done

Implementasi dianggap selesai apabila:

- [ ] Runtime snapshot mencakup seluruh dependency yang diperlukan request.
- [ ] Runtime hanya dipublikasikan setelah proses build berhasil.
- [ ] Request membaca snapshot satu kali pada awal `ServeHTTP()`.
- [ ] Request lama tetap berjalan setelah hot reload.
- [ ] Transport cache dapat mengganti konfigurasi lama.
- [ ] Transport lifecycle memiliki mekanisme cleanup yang jelas.
- [ ] Config reload tidak menghasilkan stale runtime.
- [ ] Config invalid tidak menggantikan runtime aktif.
- [ ] Circuit breaker aman untuk concurrent requests.
- [ ] HALF-OPEN memiliki batas probe.
- [ ] Load balancing benar-benar memilih target.
- [ ] HTTP error response konsisten.
- [ ] Unit dan integration tests berhasil.
- [ ] `go test -race ./...` berhasil.
- [ ] Benchmark tersedia sebagai baseline.

---

# 7. Instructions for AI Agent

Gunakan instruksi berikut ketika mengerjakan repository.

## Working Rules

1. Baca struktur project dan implementasi yang ada terlebih dahulu.
2. Jangan langsung mengubah banyak file sebelum memahami dependency.
3. Jangan melakukan full rewrite.
4. Pertahankan public API dan format konfigurasi yang sudah digunakan kecuali ada alasan teknis yang kuat.
5. Implementasikan satu phase dalam satu waktu.
6. Sebelum mengubah code, jelaskan:
   - Masalah yang ditemukan.
   - Penyebabnya.
   - File yang akan diubah.
   - Dampak perubahan.
7. Setelah implementasi, jelaskan:
   - Perubahan yang dilakukan.
   - Alasan desain.
   - Test yang ditambahkan.
   - Hasil test.
   - Risiko atau pekerjaan yang belum selesai.
8. Jangan menghapus fitur yang sudah berjalan hanya untuk menyederhanakan implementasi.
9. Jangan menambahkan dependency eksternal tanpa alasan kuat.
10. Jangan mengklaim test berhasil jika belum benar-benar dijalankan.

## Implementation Workflow

Untuk setiap phase:

**Step 1 — Inspect**

Pelajari file terkait dan cari seluruh pemanggilan fungsi yang akan diubah.

**Step 2 — Plan**

Buat daftar perubahan yang paling minimal dan identifikasi kemungkinan breaking changes.

**Step 3 — Implement**

Implementasikan perubahan hanya pada scope phase saat ini.

**Step 4 — Test**

Jalankan unit tests, integration tests yang relevan, dan race detection jika memungkinkan.

**Step 5 — Review**

Periksa:
- Data race.
- Resource leaks.
- Backward compatibility.
- Error handling.
- Concurrent request behavior.
- Unintended changes.

**Step 6 — Report**

Berikan laporan singkat sebelum melanjutkan phase berikutnya.

## Important Constraints

- Jangan mengimplementasikan semua phase dalam satu kali perubahan.
- Jangan melakukan optimasi premature.
- Jangan mengubah arsitektur yang sudah benar tanpa justifikasi.
- Jika menemukan masalah baru di luar scope, laporkan terlebih dahulu.
- Jika terdapat beberapa alternatif implementasi, jelaskan trade-off sebelum memilih.
- Jika terdapat asumsi yang belum dapat diverifikasi dari source code, jangan menganggap asumsi tersebut benar.

---

# 8. Final Expected Result

Setelah seluruh phase selesai, API Gateway diharapkan memiliki karakteristik berikut:

1. **Consistent Runtime:** Seluruh request menggunakan satu versi runtime yang konsisten.
2. **Safe Hot Reload:** Konfigurasi dapat diperbarui tanpa mengganggu request yang sedang berjalan.
3. **Reliable Transport:** Transport mengikuti konfigurasi terbaru dan memiliki lifecycle yang jelas.
4. **Resilient Upstream:** Circuit breaker, retry, dan load balancing bekerja secara konsisten.
5. **Concurrency Safety:** Tidak ada data race pada runtime publication, transport management, dan circuit breaker.
6. **Predictable HTTP Behavior:** Error response dan routing behavior mengikuti semantik HTTP yang tepat.
7. **Testable Architecture:** Perubahan runtime dan konfigurasi dapat diuji secara otomatis.

**Primary engineering objective:** Membangun runtime architecture yang aman untuk concurrent traffic dan configuration reload, tanpa mengorbankan kestabilan request yang sedang berlangsung.
