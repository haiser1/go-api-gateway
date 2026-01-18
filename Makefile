.PHONY: run run-dev build test

# ===================================================================
# RUN COMMANDS
# ===================================================================

# Perintah 'run' (Production Mode)
run: build
	@echo "Starting server (production mode)..."
	./bin/api-gateway

# Perintah 'run-dev' (Development Mode)
# Menggunakan 'go run' untuk iterasi cepat tanpa build artifact permanen
run-dev:
	@echo "Starting server (development mode)..."
	@go run ./cmd/api-gateway/main.go


# Perintah 'build' untuk membuat binary produksi
build:
	@echo "Building binary..."
	@go build -o bin/api-gateway ./cmd/api-gateway/main.go

# Perintah 'test'
test:
	@go test -v ./...