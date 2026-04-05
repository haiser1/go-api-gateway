# Stage 1: Build the Go binary
FROM golang:1.24-alpine AS builder

# Set the working directory inside the container
WORKDIR /app

# Copy go.mod and go.sum to download dependencies first
COPY go.mod go.sum ./
RUN go mod download

# Copy the entire source code into the container
COPY . .

# Build the Go application with optimized flags
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -trimpath -o /api-gateway ./cmd/api-gateway

# Stage 2: Create the final, minimal production image
FROM scratch

# Copy the compiled binary from the builder stage
COPY --from=builder /api-gateway /api-gateway

# Copy the configuration directory needed by the application at runtime
COPY --from=builder /app/configs /configs

# Expose the ports that the API Gateway listens on
EXPOSE 8080 8081

# Set the command to run when the container starts
ENTRYPOINT ["/api-gateway"]
