.PHONY: run run-dev build test docker-up docker-down docker-build docker-logs docker-stats docker-ps docker-clean

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

# ===================================================================
# DOCKER COMPOSE COMMANDS
# ===================================================================

# Menjalankan container di background
docker-up:
	@echo "Starting containers in background..."
	@docker compose up -d

# Mematikan dan menghapus container
docker-down:
	@echo "Stopping and removing containers..."
	@docker compose down

# Membangun ulang image
docker-build:
	@echo "Building docker images..."
	@docker compose build

# Melihat log container secara real-time
docker-logs:
	@docker compose logs -f

# Melihat status container
docker-ps:
	@docker compose ps

# Menghapus resource docker yang tidak terpakai (Cleanup)
docker-clean:
	@echo "Cleaning up unused docker resources..."
	@docker system prune -f

# jalankan test k6
k6-test:
	@echo "Running k6 test..."
	@docker-compose run k6

# ===================================================================
# ANALYTICS & MONITORING
# ===================================================================

# Melihat statistik resource container (CPU, RAM, Network)
docker-stats:
	@docker compose stats --no-stream --format "table {{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}\t{{.NetIO}}"

# Analitik Log sederhana: Menghitung jumlah log per level (INFO/ERROR/WARN)
log-analytics:
	@echo "Log Level Analytics (Last 1000 lines):"
	@docker compose logs --tail=1000 | grep -oE '"level":"[^"]+"' | sort | uniq -c
