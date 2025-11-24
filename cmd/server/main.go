package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/portswigger-tim/nats-k8s-oidc-callout/internal/config"
	httpserver "github.com/portswigger-tim/nats-k8s-oidc-callout/internal/http"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Initialize logger
	logger, err := initLogger(cfg.LogLevel)
	if err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}
	defer logger.Sync()

	logger.Info("starting nats-k8s-oidc-callout",
		zap.String("port", fmt.Sprintf("%d", cfg.Port)),
		zap.String("log_level", cfg.LogLevel),
	)

	// Initialize HTTP server with health checks
	httpSrv := httpserver.New(cfg.Port, logger, httpserver.HealthChecks{
		// Health check functions will be set when we implement NATS and K8s clients
		NatsConnected:    nil,
		K8sConnected:     nil,
		CacheInitialized: nil,
	})

	// Start HTTP server in a goroutine
	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- httpSrv.Start()
	}()

	// Wait for interrupt signal or server error
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErrors:
		return fmt.Errorf("server error: %w", err)
	case sig := <-shutdown:
		logger.Info("shutdown signal received", zap.String("signal", sig.String()))

		// Graceful shutdown with timeout
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		if err := httpSrv.Shutdown(ctx); err != nil {
			logger.Error("failed to shutdown HTTP server gracefully", zap.Error(err))
			return err
		}

		logger.Info("shutdown complete")
	}

	return nil
}

// initLogger creates a zap logger based on the specified log level.
func initLogger(level string) (*zap.Logger, error) {
	// Parse log level
	var zapLevel zapcore.Level
	if err := zapLevel.UnmarshalText([]byte(level)); err != nil {
		return nil, fmt.Errorf("invalid log level %q: %w", level, err)
	}

	// Create logger config
	config := zap.NewProductionConfig()
	config.Level = zap.NewAtomicLevelAt(zapLevel)
	config.EncoderConfig.TimeKey = "timestamp"
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	return config.Build()
}
