# Tabel Throughput Kinerja

Δ RPS vs A bernilai positif bila lebih tinggi dari A dan negatif bila lebih rendah. Baseline A adalah proxy tanpa auth; Z adalah direct-to-mock tanpa gateway. Data: `results/raw/perf/manifest.csv`.

| Config | Mode | VU | RPS (rerata ± SD) | Δ RPS vs A (%) | Error rate (%) |
|---|---|---|---|---|---|
| Z | closed | 50 | 10903.6 ± 136.3 | 52.3 | 0.00 |
| Z | closed | 100 | 11447.1 ± 283.5 | 57.8 | 0.00 |
| Z | closed | 200 | 11594.6 ± 415.3 | 127.8 | 0.00 |
| A | closed | 50 | 7157.7 ± 1346.3 | 0.0 | 0.00 |
| A | closed | 100 | 7255.4 ± 1325.4 | 0.0 | 0.00 |
| A | closed | 200 | 5090.3 ± 868.0 | 0.0 | 0.00 |
| B | closed | 50 | 6698.9 ± 1417.8 | -6.4 | 0.00 |
| B | closed | 100 | 6897.8 ± 1278.2 | -4.9 | 0.00 |
| B | closed | 200 | 4949.1 ± 691.1 | -2.8 | 0.00 |
| C | closed | 50 | 6382.4 ± 1405.7 | -10.8 | 0.00 |
| C | closed | 100 | 6812.3 ± 1424.4 | -6.1 | 0.00 |
| C | closed | 200 | 5120.5 ± 557.7 | 0.6 | 0.00 |
| D | closed | 50 | 6139.5 ± 1164.1 | -14.2 | 0.00 |
| D | closed | 100 | 6295.2 ± 1276.7 | -13.2 | 0.00 |
| D | closed | 200 | 4641.6 ± 862.1 | -8.8 | 0.00 |
| C2 | closed | 50 | 6122.3 ± 348.5 | -14.5 | 0.00 |
| C2 | closed | 100 | 5954.1 ± 29.9 | -17.9 | 0.00 |
| C2 | closed | 200 | 5621.0 ± 448.0 | 10.4 | 0.00 |
| F | closed | 50 | 6986.5 ± 509.5 | -2.4 | 0.00 |
| F | closed | 100 | 7235.9 ± 558.1 | -0.3 | 0.00 |
| F | closed | 200 | 5130.0 ± 226.1 | 0.8 | 0.00 |
| A | open | 200 | 4200.8 ± 32.8 | 0.0 | 0.00 |
| B | open | 200 | 4214.4 ± 7.6 | 0.3 | 0.00 |
| C | open | 200 | 4208.9 ± 11.5 | 0.2 | 0.00 |
| D | open | 200 | 4195.4 ± 21.6 | -0.1 | 0.00 |
