# Tabel Uji Keamanan (S01–S14)

Dihasilkan ulang oleh `scripts/analyze.py` dari ringkasan JSON terbaru per skenario/config di `results/raw/security/`. N = 30 serangan + 30 kontrol per skenario, kecuali S10 = 500 tebakan, S11 = banjir sampai 429, S12 = 5 host direct. S13/S14 opsional; S13 status 200 memang diharapkan dan dinilai dari header identitas di upstream.

| ID | Config | Dikirim | Ditolak | Block rate | Kode dominan | Kontrol (FRR) | Catatan |
|---|---|---|---|---|---|---|---|
| S01 | D | 30 | 30 | 100.0% | 401 | 0/30 (0.0%) | Tanpa API key dan tanpa Authorization |
| S02 | D | 30 | 30 | 100.0% | 401 | 0/30 (0.0%) | API key acak + JWT valid |
| S03 | D | 30 | 30 | 100.0% | 401 | 0/30 (0.0%) | Cabut key via Admin API; t_first_reject=1.4 ms; admin_http=200 |
| S04 | D | 30 | 30 | 100.0% | 401 | 0/30 (0.0%) | API key valid tanpa JWT |
| S05 | D | 30 | 30 | 100.0% | 401 | 0/30 (0.0%) | JWT exp = now-60s |
| S06 | D | 30 | 30 | 100.0% | 401 | 0/30 (0.0%) | Payload diubah, signature asli dipertahankan |
| S07 | D | 30 | 30 | 100.0% | 401 | 0/30 (0.0%) | alg=none dengan/tanpa titik signature |
| S08 | C2 | 30 | 30 | 100.0% | 401 | 0/30 (0.0%) | RS256 aktif; token HS256 ditandatangani public key RSA |
| S09 | D | 30 | 30 | 100.0% | 401 | 0/30 (0.0%) | wrong_iss / wrong_aud / nbf_future |
| S10 | D | 500 | 500 | 100.0% | 401 | 0/30 (0.0%) | no_rate_limit: 500 tebakan key acak berurutan pada satu koneksi keep-alive; tidak ada 429 |
| S10 | RL | 500 | 500 | 100.0% | 429 | 0/30 (0.0%) | rate_limited: 500 tebakan key acak berurutan pada satu koneksi keep-alive; 429 pertama pada iterasi 61; kontrol setelah reset window 28.04 s |
| S11 | RL | 64 | 4 | 6.2% | 200 | 0/30 (0.0%) | 200 sebelum 429 = 60; 429 pertama pada iterasi 61; 200 lagi setelah 34.03 s |
| S12 | A | 5 | 5 | 100.0% | 0 (connection refused) | 0/1 (0.0%) | host direct failed 5/5; docker-network direct status=200 (membuktikan kontrol jaringan) |
| S13 | D | 30 | 30 | 100.0% | 200 | 0/30 (0.0%) | Opsional: X-User-Id spoof dihormati 0/30; status 200 memang diharapkan (dinilai dari header di upstream) |
| S14 | D | 30 | 25 | 83.3% | 401 | 0/30 (0.0%) | Opsional: malformed 20/20 ditolak 401; oversized_garbage 5/5 ditolak 401; oversized_valid 5/5 diterima 200 (signature valid); gateway /health=200 |

**FRR agregat:** 0/421 = **0.00%**.
