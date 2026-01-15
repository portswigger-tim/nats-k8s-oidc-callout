// Package main provides the entry point for the NATS Kubernetes OIDC auth callout service.
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
	"k8s.io/client-go/tools/clientcmd"

	"github.com/portswigger-tim/nats-k8s-oidc-callout/internal/auth"
	"github.com/portswigger-tim/nats-k8s-oidc-callout/internal/config"
	"github.com/portswigger-tim/nats-k8s-oidc-callout/internal/httpserver"
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

// initJWTValidator initializes the JWT validator from either file or URL.
func initJWTValidator(cfg *config.Config, logger *zap.Logger) (*jwt.Validator, error) {
	if cfg.JWKSPath != "" {
		logger.Info("initializing JWT validator from file", zap.String("jwks_path", cfg.JWKSPath))
		validator, err := jwt.NewValidatorFromFile(cfg.JWKSPath, cfg.JWTIssuer, cfg.JWTAudience)
		if err != nil {
			return nil, fmt.Errorf("failed to create JWT validator from file: %w", err)
		}
		return validator, nil
	}

	logger.Info("initializing JWT validator from URL", zap.String("jwks_url", cfg.JWKSUrl))
	validator, err := jwt.NewValidatorFromURL(cfg.JWKSUrl, cfg.JWTIssuer, cfg.JWTAudience)
	if err != nil {
		return nil, fmt.Errorf("failed to create JWT validator from URL: %w", err)
	}
	return validator, nil
}

// initK8sClient initializes the Kubernetes client with config, clientset, and informer factory.
func initK8sClient(cfg *config.Config, logger *zap.Logger) (*k8s.Client, informers.SharedInformerFactory, chan struct{}, error) {
	logger.Info("initializing Kubernetes client")

	// Get Kubernetes config
	var k8sConfig *rest.Config
	var err error
	if cfg.K8sInCluster {
		logger.Info("using in-cluster Kubernetes config")
		k8sConfig, err = rest.InClusterConfig()
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to get in-cluster config: %w", err)
		}
	} else {
		logger.Info("using out-of-cluster Kubernetes config from KUBECONFIG")
		loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
		configOverrides := &clientcmd.ConfigOverrides{}
		kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
		k8sConfig, err = kubeConfig.ClientConfig()
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to load kubeconfig: %w", err)
		}
	}

	// Create clientset
	clientset, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create Kubernetes clientset: %w", err)
	}

	// Create informer factory
	informerFactory := informers.NewSharedInformerFactory(clientset, 0)

	// Create K8s client with ServiceAccount cache
	k8sClient := k8s.NewClient(informerFactory, logger)

	// Create stop channel for lifecycle management
	stopCh := make(chan struct{})

	return k8sClient, informerFactory, stopCh, nil
}

// startK8sInformers starts the informer factory and waits for caches to sync.
func startK8sInformers(factory informers.SharedInformerFactory, stopCh chan struct{}, logger *zap.Logger) {
	factory.Start(stopCh)
	logger.Info("waiting for Kubernetes caches to sync")
	factory.WaitForCacheSync(stopCh)
	logger.Info("Kubernetes caches synced")
}

// initNATSClient initializes the NATS client with signing key configuration.
func initNATSClient(cfg *config.Config, authHandler *auth.Handler, logger *zap.Logger) (*nats.Client, error) {
	// Determine auth mode for logging
	authMode := "URL-embedded"
	if cfg.NatsUserCredsFile != "" {
		authMode = "user-credentials"
	} else if cfg.NatsToken != "" {
		authMode = "token"
	}

	logger.Info("initializing NATS client",
		zap.String("url", cfg.NatsURL),
		zap.String("auth_mode", authMode),
		zap.String("user_creds_file", cfg.NatsUserCredsFile),
		zap.String("signing_key_file", cfg.NatsSigningKeyFile))

	// Create NATS client with authentication configuration
	natsClient, err := nats.NewClient(cfg.NatsURL, cfg.NatsUserCredsFile, cfg.NatsToken, authHandler, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create NATS client: %w", err)
	}

	// Load signing key from separate file
	logger.Info("loading account signing key", zap.String("signing_key_file", cfg.NatsSigningKeyFile))
	signingKey, err := nats.LoadSigningKeyFromFile(cfg.NatsSigningKeyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load signing key from file %s: %w",
			cfg.NatsSigningKeyFile, err)
	}
	natsClient.SetSigningKey(signingKey)

	return natsClient, nil
}

// waitForShutdown starts the HTTP server and waits for shutdown signal or server error.
// Coordinates graceful shutdown of all services with timeout.
func waitForShutdown(httpSrv *httpserver.Server, natsClient *nats.Client, logger *zap.Logger) error {
	// Start HTTP server in a goroutine
	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- httpSrv.Start()
	}()

	logger.Info("all services started successfully")

	// Wait for interrupt signal or server error
	shutdownCh := make(chan os.Signal, 1)
	signal.Notify(shutdownCh, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErrors:
		return fmt.Errorf("server error: %w", err)
	case sig := <-shutdownCh:
		logger.Info("shutdown signal received", zap.String("signal", sig.String()))

		// Graceful shutdown with timeout
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		// Shutdown in reverse order (NATS first, then HTTP)
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
	defer func() {
		if err := logger.Sync(); err != nil {
			// Sync may fail on stdout/stderr, which is expected behavior
			_ = err
		}
	}()

	logger.Info("starting nats-k8s-oidc-callout",
		zap.String("port", fmt.Sprintf("%d", cfg.Port)),
		zap.String("log_level", cfg.LogLevel),
		zap.String("nats_url", cfg.NatsURL),
		zap.String("jwks_url", cfg.JWKSUrl),
	)

	// Initialize JWT validator
	jwtValidator, err := initJWTValidator(cfg, logger)
	if err != nil {
		return err
	}

	// Initialize Kubernetes client
	k8sClient, informerFactory, stopCh, err := initK8sClient(cfg, logger)
	if err != nil {
		return err
	}
	defer close(stopCh)

	// Start informers and wait for cache sync
	startK8sInformers(informerFactory, stopCh, logger)

	// Initialize authorization handler
	authHandler := auth.NewHandler(jwtValidator, k8sClient)

	// Initialize NATS client with signing key
	natsClient, err := initNATSClient(cfg, authHandler, logger)
	if err != nil {
		return err
	}

	// Start NATS auth callout service
	ctx := context.Background()
	if err := natsClient.Start(ctx); err != nil {
		return fmt.Errorf("failed to start NATS client: %w", err)
	}

	logger.Info("NATS auth callout service started successfully")

	// Initialize HTTP server
	httpSrv := httpserver.New(cfg.Port, logger)

	// Wait for shutdown signal and coordinate graceful shutdown
	return waitForShutdown(httpSrv, natsClient, logger)
}

// initLogger creates a zap logger based on the specified log level.
func initLogger(level string) (*zap.Logger, error) {
	// Parse log level
	var zapLevel zapcore.Level
	if err := zapLevel.UnmarshalText([]byte(level)); err != nil {
		return nil, fmt.Errorf("invalid log level %q: %w", level, err)
	}

	// Create logger config
	loggerConfig := zap.NewProductionConfig()
	loggerConfig.Level = zap.NewAtomicLevelAt(zapLevel)
	loggerConfig.EncoderConfig.TimeKey = "timestamp"
	loggerConfig.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	return loggerConfig.Build()
}
