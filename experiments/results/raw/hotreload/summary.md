# Ringkasan Hot Reload dan Pencabutan

## Pencabutan API key (config D)

- Repetisi: 10
- Rerata t_first_reject: 50.759 ms
- Minimum: 50.438 ms
- Maksimum: 51.208 ms
- Request in-flight yang masih 200: 10

## Ketersediaan saat reload (50 VU, 60 s, rute /health)

- k6 RPS: 7582.739443711802
- k6 error rate: 0
- k6 p95: 15.529988 ms
- Probe non-200: 0 dari 2956
- Probe median: 1.557 ms
- Probe maksimum: 26.544 ms

| Reload | t_change | admin selesai (ms) | non-200 di window | max latency window (ms) |
|---|---|---|---|---|
| 1 | 2026-11-09T13:42:20.896423+00:00 | 14.804 | 0 | 8.884 |
| 2 | 2026-11-09T13:42:30.890107+00:00 | 14.647 | 0 | 10.829 |
| 3 | 2026-11-09T13:42:40.890760+00:00 | 14.969 | 0 | 12.799 |
| 4 | 2026-11-09T13:42:50.892170+00:00 | 17.273 | 0 | 10.994 |
| 5 | 2026-11-09T13:43:00.888103+00:00 | 16.046 | 0 | 13.504 |
