# Tabel Uji Keamanan (S01–S12 + opsional S13/S14)

Dihasilkan otomatis oleh `experiments/scripts/run_security.py` dari `results/security_raw.csv`. N = 30 serangan + 30 kontrol per skenario (kecuali S10 = 500 tebakan dan S11 = banjir sampai 429).

| ID | Config | Dikirim | Ditolak | Block rate | Kode dominan | Kontrol (FRR) | Catatan |
|---|---|---|---|---|---|---|---|
| S01 | D | 30 | 30 | 100.0% | 401 | 0/30 (0.0%) | Tanpa API key dan tanpa Authorization |
| S02 | D | 30 | 30 | 100.0% | 401 | 0/30 (0.0%) | API key acak + JWT valid |
| S03 | D | 30 | 30 | 100.0% | 401 | 0/30 (0.0%) | Cabut key via Admin API; t_first_reject=1.3 ms; admin_http=200 |
| S04 | D | 30 | 30 | 100.0% | 401 | 0/30 (0.0%) | API key valid tanpa JWT |
| S05 | D | 30 | 30 | 100.0% | 401 | 0/30 (0.0%) | JWT exp = now-60s |
| S06 | D | 30 | 30 | 100.0% | 401 | 0/30 (0.0%) | Payload diubah, signature asli dipertahankan |
| S07 | D | 30 | 30 | 100.0% | 401 | 0/30 (0.0%) | alg=none dengan/tanpa titik signature |
| S08 | C2 | 30 | 30 | 100.0% | 401 | 0/30 (0.0%) | RS256 aktif; token HS256 ditandatangani public key RSA |
| S09 | D | 30 | 30 | 100.0% | 401 | 0/30 (0.0%) | wrong_iss / wrong_aud / nbf_future |
| S10 | D | 500 | 500 | 100.0% | 401 | 0/30 (0.0%) | no_rate_limit: 500 tebakan key acak berurutan pada satu koneksi keep-alive; tidak ada 429 |
| S10 | RL | 500 | 500 | 100.0% | 429 | 0/30 (0.0%) | rate_limited: 500 tebakan key acak berurutan pada satu koneksi keep-alive; 429 pertama pada iterasi 61 |
| S11 | RL | 64 | 4 | 6.25% | 200 | 0/30 (0.0%) | 200 sebelum 429 = 60; 429 pertama pada iterasi 61; 200 lagi setelah 60.04 s |
| S12 | A | 5 | 5 | 100.0% | 0 (connection refused) | 0/1 (0.0%) | host direct failed 5/5; docker-network direct status=200 (membuktikan kontrol jaringan) |
| S13 | D | 30 | 30 | 100.0% | 200 | 0/30 (0.0%) | Opsional: X-User-Id spoof dihormati 0/30; status 200 memang diharapkan (dinilai dari header di upstream) |
| S14 | D | 30 | 25 | 83.33% | 401 | 0/30 (0.0%) | Opsional: malformed 20/20 ditolak 401; oversized_garbage 5/5 ditolak 401; oversized_valid 5/5 diterima 200 (signature valid); gateway /health=200 |

**FRR agregat:** 0/421 = **0.0%** (kontrol sah yang ditolak).

Catatan: status 429 pada S10/S11 berasal dari konfigurasi `RL` (rate limit 60/menit), bukan dari konfigurasi D tanpa rate limit. Semua data mentah per request tersimpan di `results/security_raw.csv` dan `results/raw/security/`.
