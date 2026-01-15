// Package config provides configuration loading from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all application configuration loaded from environment variables.
type Config struct {
	// HTTP Server
	Port int

	// NATS Connection Authentication (pick one):
	// Option 1: URL with embedded credentials (nats://user:pass@host:port)
	// Option 2: Separate user credentials file (NATS_USER_CREDS_FILE)
	// Option 3: Token authentication (NATS_TOKEN)
	NatsURL           string
	NatsUserCredsFile string // Optional: User credentials file (user JWT + user key)
	NatsToken         string // Optional: Token for authentication
	NatsAccount       string

	// NATS Authorization Signing (required)
	// Account signing key used to sign authorization response JWTs
	// This must be an account private key (starts with SA...)
	NatsSigningKeyFile string

	// Kubernetes JWT Validation
	JWKSUrl     string // JWKS URL (mutually exclusive with JWKSPath)
	JWKSPath    string // JWKS file path (mutually exclusive with JWKSUrl)
	JWTIssuer   string
	JWTAudience string

	// ServiceAccount Annotation Settings
	SAAnnotationPrefix string

	// Cache & Cleanup
	CacheCleanupInterval time.Duration

	// Kubernetes Client
	K8sInCluster bool
	K8sNamespace string

	// Logging
	LogLevel string
}

// Load reads configuration from environment variables and returns a Config.
// Returns an error if required variables are missing or invalid.
func Load() (*Config, error) {
	cfg := &Config{
		// Defaults
		Port:                 getEnvInt("PORT", 8080),
		K8sInCluster:         getEnvBool("K8S_IN_CLUSTER", true),
		K8sNamespace:         getEnv("K8S_NAMESPACE", ""),
		LogLevel:             getEnv("LOG_LEVEL", "info"),
		SAAnnotationPrefix:   getEnv("SA_ANNOTATION_PREFIX", "nats.io/"),
		CacheCleanupInterval: getEnvDuration("CACHE_CLEANUP_INTERVAL", 15*time.Minute),
	}

	// NATS configuration with default URL
	cfg.NatsURL = getEnv("NATS_URL", "nats://nats:4222")

	// NATS authentication options (all optional - can use URL-embedded credentials)
	cfg.NatsUserCredsFile = os.Getenv("NATS_USER_CREDS_FILE")
	cfg.NatsToken = os.Getenv("NATS_TOKEN")

	// Kubernetes JWT validation with conditional defaults for in-cluster deployments
	cfg.JWKSPath = os.Getenv("JWKS_PATH")
	if cfg.K8sInCluster {
		cfg.JWKSUrl = getEnv("JWKS_URL", "https://kubernetes.default.svc/openid/v1/jwks")
		cfg.JWTIssuer = getEnv("JWT_ISSUER", "https://kubernetes.default.svc")
	} else {
		cfg.JWKSUrl = os.Getenv("JWKS_URL")
		cfg.JWTIssuer = os.Getenv("JWT_ISSUER")
	}
	cfg.JWTAudience = getEnv("JWT_AUDIENCE", "nats")

	// Required variables (no reasonable defaults)
	var missing []string

	// NATS_SIGNING_KEY_FILE is always required
	if cfg.NatsSigningKeyFile = os.Getenv("NATS_SIGNING_KEY_FILE"); cfg.NatsSigningKeyFile == "" {
		missing = append(missing, "NATS_SIGNING_KEY_FILE")
	}

	if cfg.NatsAccount = os.Getenv("NATS_ACCOUNT"); cfg.NatsAccount == "" {
		missing = append(missing, "NATS_ACCOUNT")
	}

	// Either JWKS_URL or JWKS_PATH is required (but not both)
	if cfg.JWKSUrl == "" && cfg.JWKSPath == "" {
		missing = append(missing, "JWKS_URL or JWKS_PATH")
	}
	if cfg.JWKSUrl != "" && cfg.JWKSPath != "" {
		return nil, fmt.Errorf("JWKS_URL and JWKS_PATH are mutually exclusive; provide only one")
	}
	if cfg.JWTIssuer == "" {
		missing = append(missing, "JWT_ISSUER")
	}

	// Validate mutually exclusive NATS auth options
	authMethods := 0
	if cfg.NatsUserCredsFile != "" {
		authMethods++
	}
	if cfg.NatsToken != "" {
		authMethods++
	}
	// Note: URL-embedded credentials are always allowed and don't count as a method
	// (they're the default/fallback)

	if authMethods > 1 {
		return nil, fmt.Errorf("NATS_USER_CREDS_FILE and NATS_TOKEN are mutually exclusive; provide at most one")
	}

	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required environment variables: %v", missing)
	}

	return cfg, nil
}

// getEnv returns the value of an environment variable or a default value.
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvInt returns the integer value of an environment variable or a default value.
func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// getEnvBool returns the boolean value of an environment variable or a default value.
func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}

// getEnvDuration returns the duration value of an environment variable or a default value.
func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
}
