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

# Perintah untuk menjalankan server dalam mode live-reload
# Ini akan menggunakan file .air.toml
watch:
	@echo "Starting server with live-reload (using air)..."
	@air

# Perintah satu kali untuk menginstal 'air'
install-air:
	@echo "Installing air live-reload tool..."
	@go install github.com/cosmtrek/air@latest
	@echo "Installing air live-reload tool done"