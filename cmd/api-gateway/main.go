package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/haiser1/go-api-gateway/internal/config"
	"github.com/haiser1/go-api-gateway/internal/gateway"

	// Import for plugin registration
	_ "github.com/haiser1/go-api-gateway/internal/middleware"
)

func main() {
	cfgManager, err := config.NewManager("configs")
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load config")
	}

	// --- Configure Logging ---
	cfg := cfgManager.GetConfig()
	logLevel, err := zerolog.ParseLevel(cfg.Server.LogLevel)
	if err != nil {
		logLevel = zerolog.InfoLevel // Default
	}
	zerolog.SetGlobalLevel(logLevel)

	log.Logger = log.Output(os.Stderr)

	log.Info().Str("level", logLevel.String()).Msg("Logger initialized")

	server := gateway.NewServer(cfgManager)

	go func() {
		if err := server.Start(); err != nil {
			log.Fatal().Err(err).Msg("Server Error")
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatal().Err(err).Msg("Failed to shutdown server")
	}

	log.Info().Msg("Server Stopped")
}
