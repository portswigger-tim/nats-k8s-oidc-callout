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

	// NATS Connection
	NatsURL       string
	NatsCredsFile string
	NatsAccount   string

	// Kubernetes JWT Validation
	JWKSUrl      string // JWKS URL (mutually exclusive with JWKSPath)
	JWKSPath     string // JWKS file path (mutually exclusive with JWKSUrl)
	JWTIssuer    string
	JWTAudience  string

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

	if cfg.NatsCredsFile = os.Getenv("NATS_CREDS_FILE"); cfg.NatsCredsFile == "" {
		missing = append(missing, "NATS_CREDS_FILE")
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
