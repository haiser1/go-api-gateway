package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/haiser1/go-api-gateway/internal/config"
	"github.com/haiser1/go-api-gateway/internal/gateway"

	// Import for plugin registration
	_ "github.com/haiser1/go-api-gateway/internal/middleware"
)

func main() {
	cfgManager, err := config.NewManager("configs")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	server := gateway.NewServer(cfgManager)

	go func() {
		if err := server.Start(); err != nil {
			log.Fatalf("Server Error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Failed to shutdown server: %v", err)
	}

	log.Println("Server Stopped")
}
