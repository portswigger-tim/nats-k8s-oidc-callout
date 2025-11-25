package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/portswigger-tim/nats-k8s-oidc-callout/internal/auth"
	"github.com/portswigger-tim/nats-k8s-oidc-callout/internal/config"
	httpserver "github.com/portswigger-tim/nats-k8s-oidc-callout/internal/http"
	"github.com/portswigger-tim/nats-k8s-oidc-callout/internal/jwt"
	"github.com/portswigger-tim/nats-k8s-oidc-callout/internal/k8s"
	"github.com/portswigger-tim/nats-k8s-oidc-callout/internal/nats"
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
		zap.String("nats_url", cfg.NatsURL),
		zap.String("jwks_url", cfg.JWKSUrl),
	)

	// Initialize JWT validator
	logger.Info("initializing JWT validator", zap.String("jwks_url", cfg.JWKSUrl))
	jwtValidator, err := jwt.NewValidatorFromURL(cfg.JWKSUrl, cfg.JWTIssuer, cfg.JWTAudience)
	if err != nil {
		return fmt.Errorf("failed to create JWT validator: %w", err)
	}

	// Initialize Kubernetes client
	logger.Info("initializing Kubernetes client")
	k8sConfig, err := rest.InClusterConfig()
	if err != nil {
		return fmt.Errorf("failed to get in-cluster config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes clientset: %w", err)
	}

	// Create informer factory
	informerFactory := informers.NewSharedInformerFactory(clientset, 0)

	// Create K8s client with ServiceAccount cache
	k8sClient := k8s.NewClient(informerFactory, logger)

	// Start informers
	stopCh := make(chan struct{})
	defer close(stopCh)

	informerFactory.Start(stopCh)

	// Wait for caches to sync
	logger.Info("waiting for Kubernetes caches to sync")
	informerFactory.WaitForCacheSync(stopCh)
	logger.Info("Kubernetes caches synced")

	// Initialize authorization handler
	authHandler := auth.NewHandler(jwtValidator, k8sClient)

	// Initialize NATS client
	logger.Info("initializing NATS client", zap.String("url", cfg.NatsURL))
	natsClient, err := nats.NewClient(cfg.NatsURL, authHandler, logger)
	if err != nil {
		return fmt.Errorf("failed to create NATS client: %w", err)
	}

	// Start NATS auth callout service
	ctx := context.Background()
	if err := natsClient.Start(ctx); err != nil {
		return fmt.Errorf("failed to start NATS client: %w", err)
	}
	defer natsClient.Shutdown(ctx)

	logger.Info("NATS auth callout service started successfully")

	// Initialize HTTP server with health checks
	httpSrv := httpserver.New(cfg.Port, logger, httpserver.HealthChecks{
		NatsConnected: func() bool {
			// TODO: Add proper health check
			return true
		},
		K8sConnected: func() bool {
			// TODO: Add proper health check
			return true
		},
		CacheInitialized: func() bool {
			// TODO: Add proper health check
			return true
		},
	})

	// Start HTTP server in a goroutine
	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- httpSrv.Start()
	}()

	logger.Info("all services started successfully")

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

		// Shutdown in reverse order
		logger.Info("shutting down NATS client")
		if err := natsClient.Shutdown(ctx); err != nil {
			logger.Error("failed to shutdown NATS client", zap.Error(err))
		}

		logger.Info("shutting down HTTP server")
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
