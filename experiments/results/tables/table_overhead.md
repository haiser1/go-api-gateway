# Tabel Overhead Latensi dan Throughput

Baseline utama adalah A (proxy tanpa auth); perbandingan A vs Z mengukur overhead proxy terhadap direct-to-mock. Rerata ± SD antar-3 pengulangan.

## Overhead terhadap baseline

| Perbandingan | Mode | VU | Avg overhead (%) | p95 overhead (%) | p99 overhead (%) | Δ RPS (%) |
|---|---|---|---|---|---|---|
| B vs A | closed | 50 | 7.3 ± 31.7 | 7.8 ± 27.8 | 12.8 ± 37.9 | -6.4 ± 27.3 |
| C vs A | closed | 50 | 13.1 ± 34.1 | 8.4 ± 29.2 | 8.0 ± 35.5 | -10.8 ± 27.2 |
| D vs A | closed | 50 | 16.6 ± 32.0 | 12.4 ± 27.3 | 11.3 ± 35.0 | -14.2 ± 24.9 |
| C2 vs A | closed | 50 | 14.3 ± 21.9 | 1.9 ± 18.8 | -1.1 ± 24.1 | -14.5 ± 19.4 |
| F vs A | closed | 50 | 0.1 ± 22.3 | -3.2 ± 20.0 | -4.1 ± 26.2 | -2.4 ± 20.1 |
| B vs A | closed | 100 | 5.2 ± 27.9 | 5.6 ± 28.2 | 7.1 ± 29.5 | -4.9 ± 25.4 |
| C vs A | closed | 100 | 7.6 ± 32.0 | 7.5 ± 32.9 | 7.1 ± 38.1 | -6.1 ± 26.8 |
| D vs A | closed | 100 | 16.1 ± 32.4 | 13.9 ± 32.5 | 12.3 ± 35.4 | -13.2 ± 25.4 |
| C2 vs A | closed | 100 | 19.1 ± 19.4 | 12.8 ± 19.3 | 7.5 ± 20.8 | -17.9 ± 18.3 |
| F vs A | closed | 100 | -1.7 ± 21.0 | -3.7 ± 21.0 | -7.1 ± 22.3 | -0.3 ± 19.8 |
| B vs A | closed | 200 | 2.2 ± 23.8 | 0.7 ± 18.3 | 2.3 ± 8.1 | -2.8 ± 21.8 |
| C vs A | closed | 200 | -1.9 ± 21.3 | -8.3 ± 18.8 | 0.8 ± 17.3 | 0.6 ± 20.3 |
| D vs A | closed | 200 | 10.3 ± 29.2 | 4.1 ± 24.1 | -5.6 ± 5.5 | -8.8 ± 24.0 |
| C2 vs A | closed | 200 | -10.9 ± 19.7 | -26.9 ± 18.0 | -68.2 ± 5.6 | 10.4 ± 19.2 |
| F vs A | closed | 200 | -2.7 ± 18.7 | -6.0 ± 16.5 | -1.8 ± 7.2 | 0.8 ± 17.6 |
| B vs A | open | 200 | -19.4 ± 99.7 | -21.1 ± 105.9 | -20.9 ± 95.2 | 0.3 ± 0.8 |
| C vs A | open | 200 | -8.8 ± 103.2 | -8.8 ± 115.3 | -1.2 ± 104.0 | 0.2 ± 0.8 |
| D vs A | open | 200 | 38.0 ± 112.6 | 49.7 ± 132.7 | 45.3 ± 116.8 | -0.1 ± 0.9 |
| A vs Z | closed | 50 | 56.6 ± 32.9 | 46.4 ± 26.2 | 35.2 ± 31.7 | -34.4 ± 12.4 |
| A vs Z | closed | 100 | 61.5 ± 31.5 | 49.3 ± 28.8 | 48.5 ± 30.5 | -36.6 ± 11.8 |
| A vs Z | closed | 200 | 133.1 ± 42.6 | 160.9 ± 43.0 | 511.4 ± 25.9 | -56.1 ± 8.3 |

Nilai positif berarti konfigurasi lebih lambat/lebih tinggi daripada baseline; Δ RPS negatif berarti throughput lebih rendah. Overhead dihitung dari rerata latensi; SD pada kolom adalah propagasi SD konfigurasi dan baseline dan bersifat aproksimasi (bukan distribusi rasio yang eksak).

## Uji aditivitas biaya auth (D vs A dibandingkan (B−A)+(C−A))

| Mode | VU | Metrik | B−A (ms) | C−A (ms) | (B−A)+(C−A) | D−A (ms) | Selisih (ms) | Rasio D−A / jumlah (%) |
|---|---|---|---|---|---|---|---|---|
| closed | 50 | avg | 0.51 | 0.92 | 1.44 | 1.17 | -0.27 | 81.2 |
| closed | 50 | p95 | 1.27 | 1.38 | 2.65 | 2.02 | -0.62 | 76.5 |
| closed | 100 | avg | 0.72 | 1.05 | 1.78 | 2.25 | 0.47 | 126.4 |
| closed | 100 | p95 | 1.66 | 2.20 | 3.86 | 4.10 | 0.23 | 106.1 |
| closed | 200 | avg | 0.86 | -0.74 | 0.12 | 4.09 | 3.97 | n/a |
| closed | 200 | p95 | 0.59 | -7.33 | -6.74 | 3.62 | 10.36 | n/a |
| open | 200 | avg | -0.61 | -0.28 | -0.89 | 1.20 | 2.09 | n/a |
| open | 200 | p95 | -3.17 | -1.33 | -4.50 | 7.48 | 11.98 | n/a |

B = API key, C = JWT, D = keduanya. Rasio mendekati 100% berarti biaya dua lapis mendekati jumlah biaya masing-masing. Rasio tidak dilaporkan (n/a) bila jumlah biaya individual ≤ 0,5 ms (terlalu dekat noise) atau negatif.
