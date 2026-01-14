package config

import (
	"os"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		envVars map[string]string
		want    *Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "in-cluster with all defaults",
			envVars: map[string]string{
				"NATS_SIGNING_KEY_FILE": "/etc/nats/auth.creds",
				"NATS_ACCOUNT":          "TestAccount",
				// K8S_IN_CLUSTER defaults to true
				// NATS_URL, JWKS_URL, JWT_ISSUER should use defaults
			},
			want: &Config{
				Port:                 8080,
				NatsURL:              "nats://nats:4222",
				NatsSigningKeyFile:   "/etc/nats/auth.creds",
				NatsAccount:          "TestAccount",
				JWKSUrl:              "https://kubernetes.default.svc/openid/v1/jwks",
				JWTIssuer:            "https://kubernetes.default.svc",
				JWTAudience:          "nats",
				SAAnnotationPrefix:   "nats.io/",
				CacheCleanupInterval: 15 * time.Minute,
				K8sInCluster:         true,
				K8sNamespace:         "",
				LogLevel:             "info",
			},
			wantErr: false,
		},
		{
			name: "in-cluster with explicit overrides",
			envVars: map[string]string{
				"NATS_URL":               "nats://custom:4222",
				"NATS_SIGNING_KEY_FILE":  "/custom/creds",
				"NATS_ACCOUNT":           "CustomAccount",
				"JWKS_URL":               "https://custom.example.com/jwks",
				"JWT_ISSUER":             "https://custom.example.com",
				"JWT_AUDIENCE":           "custom-aud",
				"PORT":                   "9090",
				"K8S_IN_CLUSTER":         "true",
				"K8S_NAMESPACE":          "test-ns",
				"LOG_LEVEL":              "debug",
				"SA_ANNOTATION_PREFIX":   "custom.io/",
				"CACHE_CLEANUP_INTERVAL": "30m",
			},
			want: &Config{
				Port:                 9090,
				NatsURL:              "nats://custom:4222",
				NatsSigningKeyFile:   "/custom/creds",
				NatsAccount:          "CustomAccount",
				JWKSUrl:              "https://custom.example.com/jwks",
				JWTIssuer:            "https://custom.example.com",
				JWTAudience:          "custom-aud",
				SAAnnotationPrefix:   "custom.io/",
				CacheCleanupInterval: 30 * time.Minute,
				K8sInCluster:         true,
				K8sNamespace:         "test-ns",
				LogLevel:             "debug",
			},
			wantErr: false,
		},
		{
			name: "out-of-cluster requires explicit JWKS_URL and JWT_ISSUER",
			envVars: map[string]string{
				"NATS_SIGNING_KEY_FILE": "/etc/nats/auth.creds",
				"NATS_ACCOUNT":          "TestAccount",
				"K8S_IN_CLUSTER":        "false",
				"JWKS_URL":              "https://external.example.com/jwks",
				"JWT_ISSUER":            "https://external.example.com",
			},
			want: &Config{
				Port:                 8080,
				NatsURL:              "nats://nats:4222",
				NatsSigningKeyFile:   "/etc/nats/auth.creds",
				NatsAccount:          "TestAccount",
				JWKSUrl:              "https://external.example.com/jwks",
				JWTIssuer:            "https://external.example.com",
				JWTAudience:          "nats",
				SAAnnotationPrefix:   "nats.io/",
				CacheCleanupInterval: 15 * time.Minute,
				K8sInCluster:         false,
				K8sNamespace:         "",
				LogLevel:             "info",
			},
			wantErr: false,
		},
		{
			name: "out-of-cluster missing JWKS_URL",
			envVars: map[string]string{
				"NATS_SIGNING_KEY_FILE": "/etc/nats/auth.creds",
				"NATS_ACCOUNT":          "TestAccount",
				"K8S_IN_CLUSTER":        "false",
				// Missing JWKS_URL
				"JWT_ISSUER": "https://external.example.com",
			},
			wantErr: true,
			errMsg:  "JWKS_URL",
		},
		{
			name: "out-of-cluster missing JWT_ISSUER",
			envVars: map[string]string{
				"NATS_SIGNING_KEY_FILE": "/etc/nats/auth.creds",
				"NATS_ACCOUNT":          "TestAccount",
				"K8S_IN_CLUSTER":        "false",
				"JWKS_URL":              "https://external.example.com/jwks",
				// Missing JWT_ISSUER
			},
			wantErr: true,
			errMsg:  "JWT_ISSUER",
		},
		{
			name: "missing NATS_CREDS_FILE",
			envVars: map[string]string{
				"NATS_ACCOUNT": "TestAccount",
				// Missing NATS_CREDS_FILE
			},
			wantErr: true,
			errMsg:  "NATS_SIGNING_KEY_FILE",
		},
		{
			name: "missing NATS_ACCOUNT",
			envVars: map[string]string{
				"NATS_SIGNING_KEY_FILE": "/etc/nats/auth.creds",
				// Missing NATS_ACCOUNT
			},
			wantErr: true,
			errMsg:  "NATS_ACCOUNT",
		},
		{
			name:    "missing multiple required variables",
			envVars: map[string]string{
				// Missing both NATS_CREDS_FILE and NATS_ACCOUNT
			},
			wantErr: true,
			errMsg:  "NATS_SIGNING_KEY_FILE",
		},
		{
			name: "invalid PORT value falls back to default",
			envVars: map[string]string{
				"NATS_SIGNING_KEY_FILE": "/etc/nats/auth.creds",
				"NATS_ACCOUNT":          "TestAccount",
				"PORT":                  "invalid",
			},
			want: &Config{
				Port:                 8080, // Falls back to default
				NatsURL:              "nats://nats:4222",
				NatsSigningKeyFile:   "/etc/nats/auth.creds",
				NatsAccount:          "TestAccount",
				JWKSUrl:              "https://kubernetes.default.svc/openid/v1/jwks",
				JWTIssuer:            "https://kubernetes.default.svc",
				JWTAudience:          "nats",
				SAAnnotationPrefix:   "nats.io/",
				CacheCleanupInterval: 15 * time.Minute,
				K8sInCluster:         true,
				K8sNamespace:         "",
				LogLevel:             "info",
			},
			wantErr: false,
		},
		{
			name: "invalid K8S_IN_CLUSTER falls back to default true",
			envVars: map[string]string{
				"NATS_SIGNING_KEY_FILE": "/etc/nats/auth.creds",
				"NATS_ACCOUNT":          "TestAccount",
				"K8S_IN_CLUSTER":        "invalid",
			},
			want: &Config{
				Port:                 8080,
				NatsURL:              "nats://nats:4222",
				NatsSigningKeyFile:   "/etc/nats/auth.creds",
				NatsAccount:          "TestAccount",
				JWKSUrl:              "https://kubernetes.default.svc/openid/v1/jwks",
				JWTIssuer:            "https://kubernetes.default.svc",
				JWTAudience:          "nats",
				SAAnnotationPrefix:   "nats.io/",
				CacheCleanupInterval: 15 * time.Minute,
				K8sInCluster:         true, // Falls back to default
				K8sNamespace:         "",
				LogLevel:             "info",
			},
			wantErr: false,
		},
		{
			name: "invalid CACHE_CLEANUP_INTERVAL falls back to default",
			envVars: map[string]string{
				"NATS_SIGNING_KEY_FILE":  "/etc/nats/auth.creds",
				"NATS_ACCOUNT":           "TestAccount",
				"CACHE_CLEANUP_INTERVAL": "invalid",
			},
			want: &Config{
				Port:                 8080,
				NatsURL:              "nats://nats:4222",
				NatsSigningKeyFile:   "/etc/nats/auth.creds",
				NatsAccount:          "TestAccount",
				JWKSUrl:              "https://kubernetes.default.svc/openid/v1/jwks",
				JWTIssuer:            "https://kubernetes.default.svc",
				JWTAudience:          "nats",
				SAAnnotationPrefix:   "nats.io/",
				CacheCleanupInterval: 15 * time.Minute, // Falls back to default
				K8sInCluster:         true,
				K8sNamespace:         "",
				LogLevel:             "info",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear all environment variables before each test
			clearEnv()

			// Set environment variables for this test
			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}

			// Ensure cleanup after test
			defer clearEnv()

			// Load configuration
			got, err := Load()

			// Check error expectation
			if (err != nil) != tt.wantErr {
				t.Errorf("Load() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// If we expected an error, verify it contains the expected message
			if tt.wantErr {
				if err == nil {
					t.Errorf("Load() expected error containing %q, got nil", tt.errMsg)
					return
				}
				if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("Load() error = %q, want error containing %q", err.Error(), tt.errMsg)
				}
				return
			}

			// Compare configuration
			if got == nil {
				t.Fatal("Load() returned nil config without error")
			}

			compareConfig(t, got, tt.want)
		})
	}
}

// clearEnv clears all environment variables used by the config package
func clearEnv() {
	envVars := []string{
		"PORT",
		"NATS_URL",
		"NATS_SIGNING_KEY_FILE",
		"NATS_ACCOUNT",
		"JWKS_URL",
		"JWT_ISSUER",
		"JWT_AUDIENCE",
		"SA_ANNOTATION_PREFIX",
		"CACHE_CLEANUP_INTERVAL",
		"K8S_IN_CLUSTER",
		"K8S_NAMESPACE",
		"LOG_LEVEL",
	}
	for _, v := range envVars {
		os.Unsetenv(v)
	}
}

// compareConfig compares two Config structs field by field
func compareConfig(t *testing.T, got, want *Config) {
	t.Helper()

	if got.Port != want.Port {
		t.Errorf("Port = %v, want %v", got.Port, want.Port)
	}
	if got.NatsURL != want.NatsURL {
		t.Errorf("NatsURL = %v, want %v", got.NatsURL, want.NatsURL)
	}
	if got.NatsSigningKeyFile != want.NatsSigningKeyFile {
		t.Errorf("NatsSigningKeyFile = %v, want %v", got.NatsSigningKeyFile, want.NatsSigningKeyFile)
	}
	if got.NatsAccount != want.NatsAccount {
		t.Errorf("NatsAccount = %v, want %v", got.NatsAccount, want.NatsAccount)
	}
	if got.JWKSUrl != want.JWKSUrl {
		t.Errorf("JWKSUrl = %v, want %v", got.JWKSUrl, want.JWKSUrl)
	}
	if got.JWTIssuer != want.JWTIssuer {
		t.Errorf("JWTIssuer = %v, want %v", got.JWTIssuer, want.JWTIssuer)
	}
	if got.JWTAudience != want.JWTAudience {
		t.Errorf("JWTAudience = %v, want %v", got.JWTAudience, want.JWTAudience)
	}
	if got.SAAnnotationPrefix != want.SAAnnotationPrefix {
		t.Errorf("SAAnnotationPrefix = %v, want %v", got.SAAnnotationPrefix, want.SAAnnotationPrefix)
	}
	if got.CacheCleanupInterval != want.CacheCleanupInterval {
		t.Errorf("CacheCleanupInterval = %v, want %v", got.CacheCleanupInterval, want.CacheCleanupInterval)
	}
	if got.K8sInCluster != want.K8sInCluster {
		t.Errorf("K8sInCluster = %v, want %v", got.K8sInCluster, want.K8sInCluster)
	}
	if got.K8sNamespace != want.K8sNamespace {
		t.Errorf("K8sNamespace = %v, want %v", got.K8sNamespace, want.K8sNamespace)
	}
	if got.LogLevel != want.LogLevel {
		t.Errorf("LogLevel = %v, want %v", got.LogLevel, want.LogLevel)
	}
}

// contains checks if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && stringContains(s, substr)))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
