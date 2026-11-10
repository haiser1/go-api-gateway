# Ringkasan Hasil Eksperimen

Dihasilkan oleh `experiments/scripts/analyze.py` dari data mentah di `experiments/results/raw/`.
Rerata ± simpangan baku dihitung antar-3 pengulangan; tabel lengkap ada di
`results/tables/` dan grafik di `results/figures/`.

## Angka kunci

- Fungsional: **9/9 PASS** (F01–F09).
- Keamanan: **0/421 = 0.00% FRR**
  (kontrol sah yang ditolak); S01–S10 dan S12 diblokir 100%, S11 menguji ambang rate
  limit (4 dari 64 request pertama kena 429, 200 lagi setelah 34,03 s), S14 opsional
  25/30 karena 5 token besar dengan signature valid memang diterima.
- Hot reload/pencabutan: 10 repetisi, t_first_reject **50.76 ms**
  (min 50.44, maks 51.21; resolusi probe 50 ms); k6 50 VU/60 s pada
  `/health`: **0 error**, p95 **15.53 ms**, probe **0 non-200
  dari 2956**, maksimum **26.54 ms**.
- Throughput Z (direct-to-mock) pada VU 200: **11595 RPS**; A **5090 RPS**;
  D **4642 RPS**. CPU gateway pada VU 200: **182.6–186.5%**
  dari satu core (cpuset 2 core = 200%).
- Open model 4.226 req/s: D p95 **22.53 ms** versus A **15.05 ms**
  (overhead **+49.7%**), error rate 0.
- Overhead p95 D vs A (closed): VU 50 +12.4%, VU 100 +13.9%, VU 200 +4.1%.
- Uji aditivitas auth (p95): closed VU 50: jumlah 2.65 ms vs D−A 2.02 ms (rasio 76%); closed VU 100: jumlah 3.86 ms vs D−A 4.10 ms (rasio 106%); closed VU 200: jumlah -6.74 ms vs D−A 3.62 ms (rasio n/a); open VU 200: jumlah -4.50 ms vs D−A 7.48 ms (rasio n/a).

## Temuan utama

1. Semua kasus fungsional F01–F09 lulus; validasi API key + JWT, prioritas plugin,
   hot reload, circuit breaker, dan sanitasi header berjalan sesuai harapan.
2. Seluruh serangan wajib S01–S10 dan S12 diblokir 100%; tidak ada kontrol sah yang
   ditolak (FRR agregat 0%). S11 menunjukkan rate limiter bekerja pada ambang yang
   diuji, bukan kegagalan blokir. S14 (opsional) menerima 5/5 token besar yang masih
   memiliki signature valid; ini perilaku yang diharapkan, bukan bypass autentikasi.
3. Pencabutan API key melalui Admin API efektif pada pengukuran berikutnya; nilai
   50,4–51,2 ms adalah batas atas karena probe berinterval 50 ms. Refresh konfigurasi
   tidak menimbulkan error pada 2956 probe dan 50 VU k6; container gateway
   tidak restart.
4. Rerata p95 D (API key + JWT) lebih tinggi daripada A pada semua titik yang diuji:
   open model 4.226 req/s 22.53 vs 15.05 ms (+49.7%); closed
   model VU 50 +12.4%, VU 100 +13.9%, VU 200 +4.1%.
   Namun SD antar-run juga besar (27–133%), sehingga perbedaan ini **belum dapat
   disebut signifikan** dari 3 repetisi; arah trennya konsisten, besarnya tidak.
5. Throughput menurun tajam pada VU 200 untuk semua konfigurasi gateway; CPU gateway
   mencapai 182.6–186.5% dari satu core (cpuset 2 core), sehingga
   VU 500 tidak dijalankan. Bottleneck utama adalah CPU gateway, bukan k6.
6. Z jauh lebih cepat daripada A (11595 vs 5090 RPS pada VU 200), jadi biaya
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
