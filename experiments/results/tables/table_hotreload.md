# Tabel Hot Reload dan Pencabutan

Sumber: `results/hotreload.csv` dan `results/raw/hotreload/`. Pencabutan 10 repetisi; reload admin 5 kali saat k6 50 VU berjalan 60 s.

## Pencabutan API key (config D)

| Rep | t_change | t_first_reject (ms) |
|---|---|---|
| 1 | 2026-11-09T13:42:01.486085+00:00 | 50.602 |
| 2 | 2026-11-09T13:42:02.450161+00:00 | 50.584 |
| 3 | 2026-11-09T13:42:03.423868+00:00 | 51.208 |
| 4 | 2026-11-09T13:42:04.396869+00:00 | 51.074 |
| 5 | 2026-11-09T13:42:05.369288+00:00 | 50.640 |
| 6 | 2026-11-09T13:42:06.341855+00:00 | 51.104 |
| 7 | 2026-11-09T13:42:07.314037+00:00 | 50.438 |
| 8 | 2026-11-09T13:42:08.282511+00:00 | 50.681 |
| 9 | 2026-11-09T13:42:09.252742+00:00 | 50.524 |
| 10 | 2026-11-09T13:42:10.219989+00:00 | 50.732 |

Rerata **50.759 ms**, minimum **50.438 ms**, maksimum **51.208 ms**; request in-flight yang masih 200: **10**. Probe revocation memakai interval 50 ms, sehingga nilai t_first_reject adalah batas atas dengan resolusi 50 ms (bukan latensi internal gateway).

## Ketersediaan saat reload (rute /health)

| Reload | t_change | Admin selesai (ms) | Request window | Non-200 window | p95 window (ms) | Max window (ms) |
|---|---|---|---|---|---|---|
| 1 | 2026-11-09T13:42:20.896423+00:00 | 14.8 | 49 | 0 | 7.6 | 8.9 |
| 2 | 2026-11-09T13:42:30.890107+00:00 | 14.6 | 50 | 0 | 8.4 | 10.8 |
| 3 | 2026-11-09T13:42:40.890760+00:00 | 15.0 | 49 | 0 | 10.5 | 12.8 |
| 4 | 2026-11-09T13:42:50.892170+00:00 | 17.3 | 49 | 0 | 8.8 | 11.0 |
| 5 | 2026-11-09T13:43:00.888103+00:00 | 16.0 | 50 | 0 | 8.1 | 13.5 |

k6: **50 VU**, **7582.7 req/s**, error rate **0.00%**, p95 **15.53 ms**. Probe: **0 non-200 dari 2956**, median **1.56 ms**, maksimum **26.54 ms**. Container gateway tetap sama: **True**.
