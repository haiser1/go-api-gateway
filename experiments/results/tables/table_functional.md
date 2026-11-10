# Tabel Uji Fungsional (F01–F09)

Dihasilkan ulang oleh `scripts/analyze.py` dari `results/functional.csv`. Catatan per kasus ada di kolom `notes` CSV dan data mentah di `results/raw/functional/`.

| ID | Skenario | Harapan | Hasil | Status |
|---|---|---|---|---|
| F01 | Rute valid + kredensial valid (API key + JWT) | 200; body == upstream | 200 | PASS |
| F02 | Rute tidak terdaftar | 404 | 404 | PASS |
| F03 | Rute publik (auth override enabled=false) tanpa kredensial | 200; body == upstream | 200 | PASS |
| F04 | API key valid saja pada rute yang butuh JWT | 401 | 401 | PASS |
| F05 | JWT valid saja pada rute yang butuh API key | 401/403 | 401 | PASS |
| F06 | Prioritas konfigurasi route > service > global (request-transform) | 200; X-Scope=route | 200 | PASS |
| F07 | Perubahan konfigurasi (hot reload) diterapkan tanpa restart | 401 -> 200 -> 401 | initial=401, open@0.106s, restore@0.21s | PASS |
| F08 | Upstream mati -> circuit breaker terbuka; pulih setelah hidup | muncul 503 lalu 200 | [502, 502, 502, 502, 502, 503, 503, 503] | PASS |
| F09 | Header/body diteruskan ke upstream; X-User-Id disanitasi | body/header sama; X-User-Id=user-123 | 200 | PASS |
