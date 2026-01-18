.PHONY: run build test watch install-air

# Perintah 'run' ini sekarang untuk eksekusi normal (seperti produksi)
run: build
	@echo "Starting server (production mode)..."
	./bin/api-gateway

# Perintah 'build' untuk membuat binary produksi
build:
	@echo "Building binary..."
	@go build -o bin/api-gateway ./cmd/api-gateway/main.go

# Perintah 'test'
test:
	@go test -v ./...

# ===================================================================
# TARGET BARU UNTUK PENGEMBANGAN (DEVELOPMENT)
# ===================================================================

# (Tidak ada target khusus pengembangan saat ini karena hot-reload sudah built-in)