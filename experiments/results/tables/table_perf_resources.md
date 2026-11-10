# Tabel Sumber Daya Gateway

CPU dalam persen dari satu core; cpuset gateway 2 core sehingga 200% = seluruh cpuset. Memori dalam MiB. Rerata ± SD antar-3 pengulangan. Z tidak memakai gateway (direct-to-mock) sehingga CPU/memori gateway mendekati idle.

| Config | Mode | VU | CPU gateway (%) | Memori gateway (MiB) |
|---|---|---|---|---|
| Z | closed | 50 | 0.0 ± 0.0 | 43.7 ± 10.8 |
| Z | closed | 100 | 0.0 ± 0.0 | 43.0 ± 10.8 |
| Z | closed | 200 | 0.0 ± 0.0 | 23.7 ± 21.9 |
| A | closed | 50 | 159.4 ± 5.4 | 16.1 ± 3.5 |
| A | closed | 100 | 169.6 ± 6.4 | 25.9 ± 4.1 |
| A | closed | 200 | 182.8 ± 0.0 | 47.3 ± 11.4 |
| B | closed | 50 | 156.1 ± 4.3 | 14.9 ± 2.5 |
| B | closed | 100 | 168.5 ± 6.3 | 23.9 ± 3.8 |
| B | closed | 200 | 184.4 ± 0.9 | 46.4 ± 8.6 |
| C | closed | 50 | 168.0 ± 6.4 | 17.8 ± 4.6 |
| C | closed | 100 | 173.1 ± 3.1 | 28.1 ± 10.7 |
| C | closed | 200 | 186.5 ± 2.1 | 48.3 ± 8.2 |
| D | closed | 50 | 168.1 ± 3.9 | 16.4 ± 5.1 |
| D | closed | 100 | 176.0 ± 1.8 | 26.2 ± 5.5 |
| D | closed | 200 | 182.6 ± 5.1 | 49.5 ± 11.0 |
| C2 | closed | 50 | 181.6 ± 1.3 | 22.7 ± 2.1 |
| C2 | closed | 100 | 187.0 ± 1.1 | 37.0 ± 1.0 |
| C2 | closed | 200 | 189.3 ± 2.2 | 60.8 ± 8.9 |
| F | closed | 50 | 166.3 ± 1.1 | 21.8 ± 0.8 |
| F | closed | 100 | 174.4 ± 2.9 | 33.5 ± 0.9 |
| F | closed | 200 | 186.4 ± 0.8 | 55.5 ± 3.1 |
| A | open | 200 | 110.3 ± 3.7 | 51.4 ± 7.1 |
| B | open | 200 | 114.2 ± 3.1 | 47.4 ± 6.7 |
| C | open | 200 | 122.8 ± 3.9 | 49.8 ± 3.3 |
| D | open | 200 | 125.4 ± 2.8 | 50.5 ± 14.5 |
