# Go API Gateway

A lightweight, high-performance API Gateway written in Go. Features dynamic configuration, plugin-based middleware, circuit breaker pattern, and hot-reload support.

## Features

- **Dynamic Routing** - Route requests to backend services based on path patterns
- **Plugin System** - Extensible middleware (rate limiting, authentication, logging)
- **Circuit Breaker** - Automatic failure detection and recovery
- **Hot Reload** - Configuration changes without restart
- **Per-Service Timeouts** - Configurable connect/read timeouts per service
- **Management API** - REST API for CRUD operations on services, routes, and plugins

## Project Structure

```
├── cmd/api-gateway/     # Application entry point
├── configs/             # Configuration files (config.yaml)
├── internal/
│   ├── config/          # Configuration management
│   ├── gateway/         # Core proxy, routing, circuit breaker
│   ├── handler/         # Management API handlers
│   ├── middleware/      # Built-in plugins (auth, rate-limit, logger)
│   └── domain/          # DTOs and models
├── mock-server/         # Mock upstream for testing
└── performance/         # K6 load test scripts
```

## Prerequisites

- Go 1.21+
- Docker & Docker Compose (optional)

## Installation

```bash
# Clone repository
git clone https://github.com/haiser1/go-api-gateway.git
cd go-api-gateway

# Install dependencies
go mod tidy
```

## Local Development

### Option 1: Go Run (Development)

```bash
# Run in development mode
make run-dev

# Or directly
go run ./cmd/api-gateway/main.go
```

### Option 2: Build & Run (Production)

```bash
# Build binary
make build

# Run
make run
# or: ./bin/api-gateway
```

### Option 3: Docker Compose

```bash
# Start gateway + mock server
docker-compose up -d api-gateway mock-server

# View logs
docker-compose logs -f api-gateway

# Stop
docker-compose down
```

## Configuration

Edit `configs/config.yaml`:

```yaml
log_level: info

global_plugins: []

services:
  - id: my-service-id (auto generated uuidv4)
    name: my-backend
    host: localhost
    port: 5000
    protocol: http          # default: http
    timeout: 30             # total timeout (seconds)
    connect_timeout: 10     # connection timeout (seconds)
    read_timeout: 30        # read timeout (seconds)
    retries: 3              # retry count
    retry_backoff: 1.5      # exponential backoff multiplier

routes:
  - id: route-id (auto generated uuidv4)
    name: my-route
    methods: [GET, POST]
    paths: [/api/v1/users]
    serviceId: my-service-id
    plugins:
      - name: rate-limiting
        config:
          requests_per_minute: 100
```

## Management API

The gateway exposes a REST API on port `8080`:

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/management/services` | List all services |
| POST | `/management/services` | Add a service |
| GET | `/management/services/:id` | Get service details |
| PUT | `/management/services/:id` | Update a service |
| DELETE | `/management/services/:id` | Delete a service |
| GET | `/management/routes` | List all routes |
| POST | `/management/routes` | Add a route |
| PUT | `/management/routes/:id` | Update a route |
| DELETE | `/management/routes/:id` | Delete a route |

## Load Testing

```bash
# Run K6 load test via Docker
docker-compose run k6

# Or install K6 locally
k6 run performance/load-test.js
```



## Roadmap

This project is under active development. Upcoming features include:

- **CORS Support** - Configurable CORS headers for cross-origin requests.
- **Redis Rate Limiting** - Distributed rate limiting using Redis for cluster environments.
- **gRPC Support** - Enable gRPC proxying and protocol translation.
- **WebSockets** - Support for persistent WebSocket connections.
- **Prometheus Metrics** - Built-in metrics for monitoring gateway performance.
- **Service Discovery** - Integration with HashiCorp Consul/Etcd for dynamic service registration.
- **Advanced Load Balancing** - Support for Round Robin, Least Connections, and Weighted strategies.
- **Request/Response Transformation** - Plugins for modifying headers and body on the fly.

## License

MIT License - see [LICENSE](LICENSE)
