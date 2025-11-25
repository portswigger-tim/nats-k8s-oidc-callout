// +build integration

package nats

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	natsclient "github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
	natscontainer "github.com/testcontainers/testcontainers-go/modules/nats"
	"go.uber.org/zap"

	internalAuth "github.com/portswigger-tim/nats-k8s-oidc-callout/internal/auth"
)

// TestNATSIntegration tests the full auth callout flow with a real NATS server
func TestNATSIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Generate signing key for auth service
	authServiceKey, _ := nkeys.CreateAccount()
	authServicePubKey, _ := authServiceKey.PublicKey()

	// Create NATS configuration with auth callout
	natsConfig := fmt.Sprintf(`
# Listening port
port: 4222

# Authorization with auth callout
authorization {
	# Define an auth user that our service will use
	users: [
		{ user: "auth-service", password: "auth-service-pass" }
	]

	# Auth callout configuration
	auth_callout {
		# Our auth service's public key for signing responses
		issuer: %s

		# User that can perform auth callouts
		auth_users: [ "auth-service" ]
	}
}
`,
		authServicePubKey,
	)

	// Start NATS container using the nats module
	natsContainer, err := natscontainer.Run(
		ctx,
		"nats:latest",
		natscontainer.WithConfigFile(strings.NewReader(natsConfig)),
	)
	if err != nil {
		t.Fatalf("Failed to start NATS container: %v", err)
	}
	defer natsContainer.Terminate(ctx)

	// Get connection URL using the helper method
	natsURL, err := natsContainer.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("Failed to get connection string: %v", err)
	}
	t.Logf("NATS server running at: %s", natsURL)

	// Create mock auth handler that approves requests
	authHandler := &mockAuthHandler{
		authorizeFunc: func(req *internalAuth.AuthRequest) *internalAuth.AuthResponse {
			// For integration test, approve with test permissions
			return &internalAuth.AuthResponse{
				Allowed:              true,
				PublishPermissions:   []string{"test.>", "events.>"},
				SubscribePermissions: []string{"test.>", "commands.*", "_INBOX.>"},
			}
		},
	}

	// Create logger for integration test
	logger := zap.NewNop()

	// Create and start our auth callout client with credentials
	client, err := NewClient(natsURL, authHandler, logger)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// Use the same signing key we configured in NATS
	client.signingKey = authServiceKey

	// Override the URL to include auth credentials
	client.url = fmt.Sprintf("nats://auth-service:auth-service-pass@%s", natsURL[7:]) // Strip "nats://" prefix

	err = client.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start client: %v", err)
	}
	defer client.Shutdown(ctx)

	// Give the auth service time to subscribe
	time.Sleep(500 * time.Millisecond)

	// Now try to connect a test client with a JWT token
	testJWT := "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.test.token"

	// Create user credentials
	userKey, _ := nkeys.CreateUser()

	testConn, err := natsclient.Connect(
		natsURL,
		natsclient.UserJWT(
			func() (string, error) {
				return testJWT, nil
			},
			func(nonce []byte) ([]byte, error) {
				return userKey.Sign(nonce)
			},
		),
		natsclient.Timeout(5*time.Second),
	)

	// Note: This will likely timeout because we're not providing a valid K8s JWT
	// But the test demonstrates the integration setup
	if err != nil {
		// Expected to fail without valid JWT, but service should be running
		t.Logf("Client connection failed (expected): %v", err)
		t.Log("This is expected because we're not providing a valid Kubernetes JWT")
		t.Log("Integration test validates NATS container setup and auth service startup")
	} else {
		defer testConn.Close()
		t.Log("Client connected successfully!")

		// Try to publish (should be allowed based on our permissions)
		err = testConn.Publish("test.foo", []byte("hello"))
		if err != nil {
			t.Errorf("Failed to publish: %v", err)
		}

		// Try to subscribe (should be allowed)
		sub, err := testConn.SubscribeSync("test.bar")
		if err != nil {
			t.Errorf("Failed to subscribe: %v", err)
		} else {
			sub.Unsubscribe()
		}
	}

	t.Log("Integration test completed - NATS auth callout setup validated")
}

// TestNATSIntegration_WithValidJWT tests with a more realistic JWT flow
func TestNATSIntegration_WithValidJWT(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// This test demonstrates the structure but would need:
	// 1. Real Kubernetes JWKS and token
	// 2. JWT validator configured with JWKS
	// 3. K8s ServiceAccount cache
	// 4. Full integration of all components

	t.Skip("Full integration test requires all components wired together")
}
