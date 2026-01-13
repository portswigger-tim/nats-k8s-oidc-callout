package nats

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
	"go.uber.org/zap"

	internalAuth "github.com/portswigger-tim/nats-k8s-oidc-callout/internal/auth"
)

// Mock auth handler for testing
type mockAuthHandler struct {
	authorizeFunc func(req *internalAuth.AuthRequest) *internalAuth.AuthResponse
}

func (m *mockAuthHandler) Authorize(req *internalAuth.AuthRequest) *internalAuth.AuthResponse {
	return m.authorizeFunc(req)
}

// TestClient_Create tests client creation
func TestClient_Create(t *testing.T) {
	logger := zap.NewNop() // No-op logger for tests
	authHandler := &mockAuthHandler{
		authorizeFunc: func(req *internalAuth.AuthRequest) *internalAuth.AuthResponse {
			return &internalAuth.AuthResponse{Allowed: true}
		},
	}

	client, err := NewClient("nats://localhost:4222", "", authHandler, logger)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	if client == nil {
		t.Fatal("Expected non-nil client")
	}
}

// TestClient_BuildUserClaims tests building NATS user claims from auth response
func TestClient_BuildUserClaims(t *testing.T) {
	// Create user key
	userKey, err := nkeys.CreateUser()
	if err != nil {
		t.Fatalf("Failed to create user key: %v", err)
	}
	userPubKey, _ := userKey.PublicKey()

	// Auth response with permissions
	authResp := &internalAuth.AuthResponse{
		PublishPermissions:   []string{"hakawai.>", "platform.events.>"},
		SubscribePermissions: []string{"hakawai.>", "platform.commands.*"},
	}

	// Build user claims
	uc := jwt.NewUserClaims(userPubKey)
	uc.Pub.Allow.Add(authResp.PublishPermissions...)
	uc.Sub.Allow.Add(authResp.SubscribePermissions...)
	uc.Expires = time.Now().Add(5 * time.Minute).Unix()

	// Verify claims
	if len(uc.Pub.Allow) != 2 {
		t.Errorf("Expected 2 pub permissions, got %d", len(uc.Pub.Allow))
	}

	if len(uc.Sub.Allow) != 2 {
		t.Errorf("Expected 2 sub permissions, got %d", len(uc.Sub.Allow))
	}
}

// TestClient_AuthorizationFailure tests authorization rejection
func TestClient_AuthorizationFailure(t *testing.T) {
	// Mock auth handler that rejects requests
	authHandler := &mockAuthHandler{
		authorizeFunc: func(req *internalAuth.AuthRequest) *internalAuth.AuthResponse {
			return &internalAuth.AuthResponse{
				Allowed: false,
				Error:   "authorization failed",
			}
		},
	}

	authReq := &internalAuth.AuthRequest{
		Token: "invalid.jwt.token",
	}

	resp := authHandler.Authorize(authReq)

	if resp.Allowed {
		t.Error("Expected authorization to be denied")
	}

	if resp.Error != "authorization failed" {
		t.Errorf("Expected error message, got: %s", resp.Error)
	}
}

// TestClient_Shutdown tests graceful shutdown
func TestClient_Shutdown(t *testing.T) {
	authHandler := &mockAuthHandler{
		authorizeFunc: func(req *internalAuth.AuthRequest) *internalAuth.AuthResponse {
			return &internalAuth.AuthResponse{Allowed: true}
		},
	}

	// Create a minimal config for testing shutdown
	// This test validates we can create and shutdown cleanly
	client := &Client{
		authHandler: authHandler,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := client.Shutdown(ctx)
	if err != nil {
		t.Errorf("Shutdown should not error: %v", err)
	}
}

// TestExtractToken tests JWT token extraction from authorization requests
func TestExtractToken(t *testing.T) {
	tests := []struct {
		name    string
		request *jwt.AuthorizationRequest
		wantJWT string
	}{
		{
			name: "Token in JWT field",
			request: &jwt.AuthorizationRequest{
				ConnectOptions: jwt.ConnectOptions{
					JWT: "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.test.jwt",
				},
			},
			wantJWT: "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.test.jwt",
		},
		{
			name: "Token in Token field",
			request: &jwt.AuthorizationRequest{
				ConnectOptions: jwt.ConnectOptions{
					Token: "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.test.token",
				},
			},
			wantJWT: "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.test.token",
		},
		{
			name: "JWT field takes precedence over Token",
			request: &jwt.AuthorizationRequest{
				ConnectOptions: jwt.ConnectOptions{
					JWT:   "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.jwt.field",
					Token: "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.token.field",
				},
			},
			wantJWT: "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.jwt.field",
		},
		{
			name: "Empty when no token provided",
			request: &jwt.AuthorizationRequest{
				ConnectOptions: jwt.ConnectOptions{},
			},
			wantJWT: "",
		},
	}

	// Create a minimal client for testing with a no-op logger
	logger := zap.NewNop()
	client := &Client{logger: logger}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := client.extractToken(tt.request)
			if got != tt.wantJWT {
				t.Errorf("extractToken() = %q, want %q", got, tt.wantJWT)
			}
		})
	}
}

// TestClient_AuthorizerFunction tests the authorizer function integration
func TestClient_AuthorizerFunction(t *testing.T) {
	tests := []struct {
		name        string
		token       string
		authHandler func(req *internalAuth.AuthRequest) *internalAuth.AuthResponse
		wantError   bool
		wantPubLen  int
		wantSubLen  int
	}{
		{
			name:  "Successful authorization with permissions",
			token: "valid.jwt.token",
			authHandler: func(req *internalAuth.AuthRequest) *internalAuth.AuthResponse {
				return &internalAuth.AuthResponse{
					Allowed:              true,
					PublishPermissions:   []string{"test.>", "events.>"},
					SubscribePermissions: []string{"test.>", "commands.*"},
				}
			},
			wantError:  false,
			wantPubLen: 2,
			wantSubLen: 2,
		},
		{
			name:  "Authorization denied",
			token: "invalid.jwt.token",
			authHandler: func(req *internalAuth.AuthRequest) *internalAuth.AuthResponse {
				return &internalAuth.AuthResponse{
					Allowed: false,
					Error:   "authorization failed",
				}
			},
			wantError:  true,
			wantPubLen: 0,
			wantSubLen: 0,
		},
		{
			name:  "Empty token rejected",
			token: "",
			authHandler: func(req *internalAuth.AuthRequest) *internalAuth.AuthResponse {
				t.Error("Auth handler should not be called with empty token")
				return &internalAuth.AuthResponse{Allowed: false}
			},
			wantError:  true,
			wantPubLen: 0,
			wantSubLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create client
			logger := zap.NewNop()
			authHandler := &mockAuthHandler{
				authorizeFunc: tt.authHandler,
			}

			client, err := NewClient("nats://localhost:4222", "", authHandler, logger)
			if err != nil {
				t.Fatalf("Failed to create client: %v", err)
			}

			// Set up signing key for JWT encoding
			signingKey, err := nkeys.CreateAccount()
			if err != nil {
				t.Fatalf("Failed to create signing key: %v", err)
			}
			client.SetSigningKey(signingKey)

			// Create authorization request
			userKey, _ := nkeys.CreateUser()
			userPubKey, _ := userKey.PublicKey()

			req := &jwt.AuthorizationRequest{
				UserNkey: userPubKey,
				ConnectOptions: jwt.ConnectOptions{
					JWT: tt.token,
				},
			}

			// Call the internal authorizer logic (simulate)
			token := client.extractToken(req)

			if token == "" {
				// Should be rejected
				if !tt.wantError {
					t.Error("Expected success but got empty token")
				}
				return
			}

			authReq := &internalAuth.AuthRequest{Token: token}
			authResp := authHandler.Authorize(authReq)

			if authResp.Allowed && tt.wantError {
				t.Error("Expected authorization to fail but it succeeded")
			}

			if !authResp.Allowed && !tt.wantError {
				t.Error("Expected authorization to succeed but it failed")
			}

			if authResp.Allowed {
				// Build user claims
				uc := jwt.NewUserClaims(req.UserNkey)
				uc.Pub.Allow.Add(authResp.PublishPermissions...)
				uc.Sub.Allow.Add(authResp.SubscribePermissions...)
				uc.Expires = time.Now().Add(5 * time.Minute).Unix()

				// Encode to verify it works
				encoded, err := uc.Encode(client.signingKey)
				if err != nil {
					t.Errorf("Failed to encode user claims: %v", err)
				}

				if encoded == "" {
					t.Error("Expected non-empty encoded JWT")
				}

				if len(uc.Pub.Allow) != tt.wantPubLen {
					t.Errorf("Got %d pub permissions, want %d", len(uc.Pub.Allow), tt.wantPubLen)
				}

				if len(uc.Sub.Allow) != tt.wantSubLen {
					t.Errorf("Got %d sub permissions, want %d", len(uc.Sub.Allow), tt.wantSubLen)
				}
			}
		})
	}
}

// TestClient_NewClient tests client creation edge cases
func TestClient_NewClient(t *testing.T) {
	authHandler := &mockAuthHandler{
		authorizeFunc: func(req *internalAuth.AuthRequest) *internalAuth.AuthResponse {
			return &internalAuth.AuthResponse{Allowed: true}
		},
	}

	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{
			name:    "Valid NATS URL",
			url:     "nats://localhost:4222",
			wantErr: false,
		},
		{
			name:    "Valid NATS URL with TLS",
			url:     "tls://localhost:4222",
			wantErr: false,
		},
		{
			name:    "Empty URL",
			url:     "",
			wantErr: false, // NewClient doesn't validate URL
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zap.NewNop()
			client, err := NewClient(tt.url, "", authHandler, logger)

			if tt.wantErr && err == nil {
				t.Error("Expected error but got none")
			}

			if !tt.wantErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if !tt.wantErr && client == nil {
				t.Error("Expected non-nil client")
			}

			if client != nil {
				if client.url != tt.url {
					t.Errorf("Client URL = %q, want %q", client.url, tt.url)
				}
			}
		})
	}
}

// TestClient_UserClaimsExpiration tests that user claims have proper expiration
func TestClient_UserClaimsExpiration(t *testing.T) {
	userKey, _ := nkeys.CreateUser()
	userPubKey, _ := userKey.PublicKey()

	before := time.Now()

	uc := jwt.NewUserClaims(userPubKey)
	uc.Expires = time.Now().Add(DefaultTokenExpiry).Unix()

	after := time.Now()

	// Check that expiration is within expected range
	expectedMin := before.Add(DefaultTokenExpiry).Unix()
	expectedMax := after.Add(DefaultTokenExpiry).Unix()

	if uc.Expires < expectedMin || uc.Expires > expectedMax {
		t.Errorf("Expiration %d not in expected range [%d, %d]", uc.Expires, expectedMin, expectedMax)
	}

	// Verify it's in the future
	if uc.Expires <= time.Now().Unix() {
		t.Error("Token expiration should be in the future")
	}
}

// TestClient_PermissionsMapping tests mapping auth response to NATS claims
func TestClient_PermissionsMapping(t *testing.T) {
	userKey, _ := nkeys.CreateUser()
	userPubKey, _ := userKey.PublicKey()

	tests := []struct {
		name     string
		pubPerms []string
		subPerms []string
	}{
		{
			name:     "Simple permissions",
			pubPerms: []string{"test.>"},
			subPerms: []string{"test.>"},
		},
		{
			name:     "Multiple permissions",
			pubPerms: []string{"prod.>", "staging.>", "events.*"},
			subPerms: []string{"prod.>", "staging.>", "commands.*", "_INBOX.>"},
		},
		{
			name:     "Wildcard permissions",
			pubPerms: []string{">"},
			subPerms: []string{">"},
		},
		{
			name:     "Empty permissions",
			pubPerms: []string{},
			subPerms: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uc := jwt.NewUserClaims(userPubKey)
			uc.Pub.Allow.Add(tt.pubPerms...)
			uc.Sub.Allow.Add(tt.subPerms...)

			if len(uc.Pub.Allow) != len(tt.pubPerms) {
				t.Errorf("Pub permissions count = %d, want %d", len(uc.Pub.Allow), len(tt.pubPerms))
			}

			if len(uc.Sub.Allow) != len(tt.subPerms) {
				t.Errorf("Sub permissions count = %d, want %d", len(uc.Sub.Allow), len(tt.subPerms))
			}

			// Verify each permission was added
			for _, perm := range tt.pubPerms {
				if !contains(uc.Pub.Allow, perm) {
					t.Errorf("Pub permission %q not found in claims", perm)
				}
			}

			for _, perm := range tt.subPerms {
				if !contains(uc.Sub.Allow, perm) {
					t.Errorf("Sub permission %q not found in claims", perm)
				}
			}
		})
	}
}

// Helper function to check if StringList contains a string
func contains(list jwt.StringList, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

// TestClient_WithValidCredentialsFile tests creating a client with a valid credentials file
func TestClient_WithValidCredentialsFile(t *testing.T) {
	// Create a temporary credentials file
	credsFile, err := os.CreateTemp("", "test-creds-*.creds")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(credsFile.Name())

	// Write valid credentials content
	credsContent := `-----BEGIN NATS USER JWT-----
eyJ0eXAiOiJKV1QiLCJhbGciOiJlZDI1NTE5LW5rZXkifQ.test
------END NATS USER JWT------

-----BEGIN USER NKEY SEED-----
SUAAVVV6MJIGCPXSBFF7P5IPJYLNE3IYINMPIZTQZZ6M4G6HBIVZM
------END USER NKEY SEED------
`
	if _, err := credsFile.WriteString(credsContent); err != nil {
		t.Fatalf("Failed to write credentials: %v", err)
	}
	credsFile.Close()

	// Set proper permissions
	if err := os.Chmod(credsFile.Name(), 0600); err != nil {
		t.Fatalf("Failed to set permissions: %v", err)
	}

	logger := zap.NewNop()
	authHandler := &mockAuthHandler{}

	// Should succeed with valid credentials file
	client, err := NewClient("nats://localhost:4222", credsFile.Name(), authHandler, logger)
	if err != nil {
		t.Fatalf("Failed to create client with valid credentials: %v", err)
	}

	if client == nil {
		t.Fatal("Client should not be nil")
	}

	if client.credsFile != credsFile.Name() {
		t.Errorf("Client credsFile = %q, want %q", client.credsFile, credsFile.Name())
	}
}

// TestClient_WithInvalidCredentialsFile tests validation of invalid credentials files
func TestClient_WithInvalidCredentialsFile(t *testing.T) {
	logger := zap.NewNop()
	authHandler := &mockAuthHandler{}

	tests := []struct {
		name      string
		credsFile string
		wantErr   string
	}{
		{
			name:      "Non-existent file",
			credsFile: "/tmp/nonexistent-file-12345.creds",
			wantErr:   "credentials file validation failed",
		},
		{
			name:      "Directory instead of file",
			credsFile: os.TempDir(),
			wantErr:   "not a regular file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient("nats://localhost:4222", tt.credsFile, authHandler, logger)

			if err == nil {
				t.Errorf("Expected error containing %q, got nil", tt.wantErr)
			} else if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Expected error containing %q, got %q", tt.wantErr, err.Error())
			}

			if client != nil {
				t.Error("Client should be nil on error")
			}
		})
	}
}

// TestClient_PathTraversalProtection tests that path traversal attempts are detected
func TestClient_PathTraversalProtection(t *testing.T) {
	logger := zap.NewNop()
	authHandler := &mockAuthHandler{}

	// Paths that contain .. and would be cleaned differently
	suspiciousPaths := []string{
		"/tmp/../etc/passwd",
		"./config/../../../etc/hosts",
		"creds/../../sensitive.creds",
	}

	for _, path := range suspiciousPaths {
		t.Run(path, func(t *testing.T) {
			client, err := NewClient("nats://localhost:4222", path, authHandler, logger)

			// These paths will fail validation either due to:
			// 1. Path traversal detection (if cleaned path != original)
			// 2. File not found (if they happen to be equivalent)
			if err == nil {
				t.Errorf("Expected error for suspicious path %q, got nil", path)
			}

			if client != nil {
				t.Error("Client should be nil on error")
			}
		})
	}
}

// TestClient_WithEmptyCredentialsFile tests that empty credentials file is valid (URL-based auth)
func TestClient_WithEmptyCredentialsFile(t *testing.T) {
	logger := zap.NewNop()
	authHandler := &mockAuthHandler{}

	// Should succeed with empty credentials file (URL-based auth)
	client, err := NewClient("nats://user:pass@localhost:4222", "", authHandler, logger)
	if err != nil {
		t.Fatalf("Failed to create client with empty credentials: %v", err)
	}

	if client == nil {
		t.Fatal("Client should not be nil")
	}

	if client.credsFile != "" {
		t.Errorf("Client credsFile should be empty, got %q", client.credsFile)
	}
}
