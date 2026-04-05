// mock-server/main.go
package main

import (
	"log"
	"net"
	"net/http"
	"os"
	"runtime"
	"time"
)

// Pre-allocate response bytes (zero allocation per-request)
var mockResponse = []byte("{\"status\":\"mock_ok\"}\n")
var healthResponse = []byte("{\"status\":\"healthy\"}\n")

func main() {
	// Use all available CPUs
	runtime.GOMAXPROCS(runtime.NumCPU())

	mux := http.NewServeMux()

	mux.HandleFunc("/mock", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(mockResponse)
	})

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(healthResponse)
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "9090"
	}

	server := &http.Server{
		Handler: mux,

		// Timeouts tuned for extreme stress test
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       120 * time.Second,
		ReadHeaderTimeout: 2 * time.Second,
		MaxHeaderBytes:    1 << 16, // 64KB
	}

	// Custom listener with high TCP backlog
	// Default backlog is 128, kita naikkan agar tidak reject connection saat spike
	ln, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("Failed to listen: %s\n", err)
	}

	log.Printf("Starting optimized mock server on :%s (GOMAXPROCS=%d)", port, runtime.GOMAXPROCS(0))
	if err := server.Serve(ln); err != nil {
		log.Fatalf("Could not start mock server: %s\n", err)
	}
}
