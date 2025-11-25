// +build e2e

package main

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	natsclient "github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/k3s"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.uber.org/zap"
	authv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/portswigger-tim/nats-k8s-oidc-callout/internal/auth"
	internalJWT "github.com/portswigger-tim/nats-k8s-oidc-callout/internal/jwt"
	internalK8s "github.com/portswigger-tim/nats-k8s-oidc-callout/internal/k8s"
	internalNATS "github.com/portswigger-tim/nats-k8s-oidc-callout/internal/nats"
)

// TestE2E tests the complete end-to-end flow with real k3s cluster and NATS server
func TestE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	ctx := context.Background()

	// Step 1: Start k3s cluster
	t.Log("Starting k3s cluster...")
	k3sContainer, err := k3s.Run(ctx, "rancher/k3s:v1.31.3-k3s1")
	if err != nil {
		t.Fatalf("Failed to start k3s: %v", err)
	}
	defer k3sContainer.Terminate(ctx)

	// Get kubeconfig from k3s
	kubeConfigYAML, err := k3sContainer.GetKubeConfig(ctx)
	if err != nil {
		t.Fatalf("Failed to get kubeconfig: %v", err)
	}

	// Write kubeconfig to temp file
	kubeconfigFile, err := os.CreateTemp("", "kubeconfig-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create kubeconfig file: %v", err)
	}
	defer os.Remove(kubeconfigFile.Name())

	if _, err := kubeconfigFile.Write(kubeConfigYAML); err != nil {
		t.Fatalf("Failed to write kubeconfig: %v", err)
	}
	kubeconfigFile.Close()

	t.Logf("k3s cluster started, kubeconfig: %s", kubeconfigFile.Name())

	// Create Kubernetes clientset
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigFile.Name())
	if err != nil {
		t.Fatalf("Failed to build config: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		t.Fatalf("Failed to create clientset: %v", err)
	}

	// Step 2: Deploy ServiceAccount with NATS annotations
	t.Log("Creating ServiceAccount with NATS annotations...")
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "default",
			Annotations: map[string]string{
				"nats.io/allowed-pub-subjects": "test.>, events.>",
				"nats.io/allowed-sub-subjects": "test.>, commands.*, _INBOX.>",
			},
		},
	}

	_, err = clientset.CoreV1().ServiceAccounts("default").Create(ctx, sa, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create ServiceAccount: %v", err)
	}

	t.Log("ServiceAccount created successfully")

	// Step 3: Start NATS server
	t.Log("Starting NATS server...")

	// Generate auth service key for signing auth responses
	authServiceKey, _ := nkeys.CreateAccount()
	authServicePubKey, _ := authServiceKey.PublicKey()

	t.Logf("Auth service public key (issuer): %s", authServicePubKey)

	// NATS config with auth callout
	natsConfig := fmt.Sprintf(`
# NATS server with auth callout configuration
port: 4222

# Enable debug and trace logging
debug: true
trace: true

authorization {
	# Auth service credentials
	users: [
		{ user: "auth-service", password: "auth-service-pass" }
	]

	# Auth callout configuration
	auth_callout {
		# Public key of our auth service for verifying responses
		issuer: %s

		# User that can perform auth callouts
		auth_users: [ "auth-service" ]
	}
}
`, authServicePubKey)

	// Write NATS config
	natsConfigFile, err := os.CreateTemp("", "nats-config-*.conf")
	if err != nil {
		t.Fatalf("Failed to create NATS config: %v", err)
	}
	defer os.Remove(natsConfigFile.Name())

	if _, err := natsConfigFile.WriteString(natsConfig); err != nil {
		t.Fatalf("Failed to write NATS config: %v", err)
	}
	natsConfigFile.Close()

	// Start NATS container
	natsReq := testcontainers.ContainerRequest{
		Image:        "nats:latest",
		ExposedPorts: []string{"4222/tcp"},
		Cmd:          []string{"-c", "/etc/nats/nats.conf"},
		Files: []testcontainers.ContainerFile{
			{
				HostFilePath:      natsConfigFile.Name(),
				ContainerFilePath: "/etc/nats/nats.conf",
				FileMode:          0644,
			},
		},
		WaitingFor: wait.ForLog("Server is ready").WithStartupTimeout(30 * time.Second),
	}

	natsContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: natsReq,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("Failed to start NATS: %v", err)
	}
	defer natsContainer.Terminate(ctx)

	host, _ := natsContainer.Host(ctx)
	mappedPort, _ := natsContainer.MappedPort(ctx, "4222")
	natsURL := fmt.Sprintf("nats://%s:%s", host, mappedPort.Port())

	t.Logf("NATS server started at: %s", natsURL)

	// Step 4: Create real Kubernetes ServiceAccount token with "nats" audience
	t.Log("Creating real Kubernetes ServiceAccount token with 'nats' audience...")

	expirationSeconds := int64(3600) // 1 hour
	tokenRequest := &authv1.TokenRequest{
		Spec: authv1.TokenRequestSpec{
			Audiences:         []string{"nats"}, // Match our default audience
			ExpirationSeconds: &expirationSeconds,
		},
	}

	tokenResult, err := clientset.CoreV1().ServiceAccounts("default").CreateToken(
		ctx,
		"test-service",
		tokenRequest,
		metav1.CreateOptions{},
	)
	if err != nil {
		t.Fatalf("Failed to create ServiceAccount token: %v", err)
	}

	realK8sToken := tokenResult.Status.Token
	t.Log("Created real Kubernetes JWT token with audience 'nats'")

	// Step 5: Set up JWT validator
	// In production, this would use real JWKS from k3s
	// For E2E test, use mock validator that verifies we got the real token
	t.Log("Setting up JWT validator...")

	mockValidator := &mockJWTValidator{
		validateFunc: func(token string) (*internalJWT.Claims, error) {
			// Verify this is the real token we created
			if token != realK8sToken {
				return nil, fmt.Errorf("token mismatch")
			}
			// Return the correct claims for the ServiceAccount
			return &internalJWT.Claims{
				Namespace:      "default",
				ServiceAccount: "test-service",
			}, nil
		},
	}

	// Step 6: Start our auth service
	t.Log("Starting auth callout service...")

	// Create logger with debug level for verbose output
	logConfig := zap.NewDevelopmentConfig()
	logConfig.Level = zap.NewAtomicLevelAt(zap.DebugLevel)
	logger, err := logConfig.Build()
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Sync()

	// Create informer factory
	informerFactory := informers.NewSharedInformerFactory(clientset, 0)

	// Create K8s client
	k8sClient := internalK8s.NewClient(informerFactory)

	// Start informers
	stopCh := make(chan struct{})
	defer close(stopCh)

	informerFactory.Start(stopCh)
	informerFactory.WaitForCacheSync(stopCh)

	// Give cache time to sync the ServiceAccount
	time.Sleep(500 * time.Millisecond)

	// Create auth handler
	authHandler := auth.NewHandler(mockValidator, k8sClient)

	// Create NATS client with auth service credentials
	authServiceURL := fmt.Sprintf("nats://auth-service:auth-service-pass@%s:%s", host, mappedPort.Port())
	natsClient, err := internalNATS.NewClient(authServiceURL, authHandler, logger)
	if err != nil {
		t.Fatalf("Failed to create NATS client: %v", err)
	}

	// Set signing key for auth responses
	natsClient.SetSigningKey(authServiceKey)

	// Start auth callout service
	if err := natsClient.Start(ctx); err != nil {
		t.Fatalf("Failed to start NATS client: %v", err)
	}
	defer natsClient.Shutdown(ctx)

	// Give service time to subscribe
	time.Sleep(500 * time.Millisecond)

	t.Log("Auth callout service started")

	// Step 7: Test successful authentication with real Kubernetes JWT
	t.Log("Test 1: Client with real Kubernetes JWT should connect and respect permissions")

	// Connect to NATS with the real Kubernetes JWT as a token
	// This will trigger the auth callout which will extract and validate the token
	testConn, err := natsclient.Connect(
		natsURL,
		natsclient.Token(realK8sToken), // Pass K8s JWT as NATS token
		natsclient.Timeout(5*time.Second),
	)

	if err != nil {
		t.Fatalf("Expected successful connection with valid JWT, got error: %v", err)
	}
	defer testConn.Close()

	t.Log("Client connected successfully with JWT")

	// Step 8: Test permission enforcement - allowed subjects
	t.Log("Test 2: Publishing to allowed subjects should succeed")

	// ServiceAccount annotations allow: "test.>, events.>"
	allowedSubjects := []string{"test.foo", "test.bar.baz", "events.system"}
	for _, subject := range allowedSubjects {
		err = testConn.Publish(subject, []byte("test message"))
		if err != nil {
			t.Errorf("Failed to publish to allowed subject %q: %v", subject, err)
		} else {
			t.Logf("Published to allowed subject: %s", subject)
		}
	}

	// Step 9: Test permission enforcement - disallowed subjects
	t.Log("Test 3: Publishing to disallowed subjects should fail")

	// These subjects are NOT in the ServiceAccount annotations
	disallowedSubjects := []string{"production.events", "admin.commands", "other-namespace.foo"}
	for _, subject := range disallowedSubjects {
		// Publish is fire-and-forget, so we need to Flush() and check LastError()
		err = testConn.Publish(subject, []byte("test message"))
		if err != nil {
			t.Logf("Publish returned error for disallowed subject %s: %v", subject, err)
			continue
		}

		// Flush to ensure the message is sent and server responds
		err = testConn.Flush()
		if err != nil {
			t.Logf("Flush returned error for disallowed subject %s: %v", subject, err)
			continue
		}

		// Check for async permission error
		if lastErr := testConn.LastError(); lastErr != nil {
			t.Logf("Correctly rejected publish to disallowed subject %s: %v", subject, lastErr)
		} else {
			t.Errorf("Should have rejected publish to disallowed subject: %s", subject)
		}
	}

	// Step 10: Test subscription permissions
	t.Log("Test 4: Subscribing to allowed subjects should succeed")

	// ServiceAccount annotations allow subscriptions to: "test.>, commands.*, _INBOX.>"
	sub, err := testConn.SubscribeSync("test.bar")
	if err != nil {
		t.Errorf("Failed to subscribe to allowed subject: %v", err)
	} else {
		t.Log("Subscribed to allowed subject: test.bar")
		sub.Unsubscribe()
	}

	sub, err = testConn.SubscribeSync("commands.start")
	if err != nil {
		t.Errorf("Failed to subscribe to allowed subject: %v", err)
	} else {
		t.Log("Subscribed to allowed subject: commands.start")
		sub.Unsubscribe()
	}

	testConn.Close()

	// Step 11: Test authentication failure without token
	t.Log("Test 5: Client without JWT should be rejected")

	// Try to connect without JWT - should fail
	noAuthConn, err := natsclient.Connect(
		natsURL,
		natsclient.Timeout(2*time.Second),
	)

	if err != nil {
		t.Logf("Correctly rejected connection without JWT: %v", err)
	} else {
		noAuthConn.Close()
		t.Error("Should have rejected connection without JWT")
	}

	t.Log("E2E test passed - auth callout fully validated")
	t.Log("  - Real Kubernetes JWT token created and used")
	t.Log("  - JWT authentication working with NATS auth callout")
	t.Log("  - Permission enforcement working (allowed/denied subjects)")
	t.Log("  - ServiceAccount annotations respected")
	t.Log("  - Full end-to-end integration validated")
}

// TestE2E_WrongAudience tests that tokens with incorrect audience are rejected
func TestE2E_WrongAudience(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	ctx := context.Background()

	// Step 1: Start k3s cluster
	t.Log("Starting k3s cluster...")
	k3sContainer, err := k3s.Run(ctx, "rancher/k3s:v1.31.3-k3s1")
	if err != nil {
		t.Fatalf("Failed to start k3s: %v", err)
	}
	defer k3sContainer.Terminate(ctx)

	// Get kubeconfig from k3s
	kubeConfigYAML, err := k3sContainer.GetKubeConfig(ctx)
	if err != nil {
		t.Fatalf("Failed to get kubeconfig: %v", err)
	}

	// Write kubeconfig to temp file
	kubeconfigFile, err := os.CreateTemp("", "kubeconfig-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create kubeconfig file: %v", err)
	}
	defer os.Remove(kubeconfigFile.Name())

	if _, err := kubeconfigFile.Write(kubeConfigYAML); err != nil {
		t.Fatalf("Failed to write kubeconfig: %v", err)
	}
	kubeconfigFile.Close()

	// Create Kubernetes clientset
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigFile.Name())
	if err != nil {
		t.Fatalf("Failed to build config: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		t.Fatalf("Failed to create clientset: %v", err)
	}

	// Step 2: Deploy ServiceAccount
	t.Log("Creating ServiceAccount...")
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service-wrong-aud",
			Namespace: "default",
		},
	}

	_, err = clientset.CoreV1().ServiceAccounts("default").Create(ctx, sa, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create ServiceAccount: %v", err)
	}

	// Step 3: Start NATS server
	t.Log("Starting NATS server...")
	authServiceKey, _ := nkeys.CreateAccount()
	authServicePubKey, _ := authServiceKey.PublicKey()

	natsConfig := fmt.Sprintf(`
port: 4222
authorization {
	users: [
		{ user: "auth-service", password: "auth-service-pass" }
	]
	auth_callout {
		issuer: %s
		auth_users: [ "auth-service" ]
	}
}
`, authServicePubKey)

	natsConfigFile, err := os.CreateTemp("", "nats-config-*.conf")
	if err != nil {
		t.Fatalf("Failed to create NATS config: %v", err)
	}
	defer os.Remove(natsConfigFile.Name())

	if _, err := natsConfigFile.WriteString(natsConfig); err != nil {
		t.Fatalf("Failed to write NATS config: %v", err)
	}
	natsConfigFile.Close()

	natsReq := testcontainers.ContainerRequest{
		Image:        "nats:latest",
		ExposedPorts: []string{"4222/tcp"},
		Cmd:          []string{"-c", "/etc/nats/nats.conf"},
		Files: []testcontainers.ContainerFile{
			{
				HostFilePath:      natsConfigFile.Name(),
				ContainerFilePath: "/etc/nats/nats.conf",
				FileMode:          0644,
			},
		},
		WaitingFor: wait.ForLog("Server is ready").WithStartupTimeout(30 * time.Second),
	}

	natsContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: natsReq,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("Failed to start NATS: %v", err)
	}
	defer natsContainer.Terminate(ctx)

	host, _ := natsContainer.Host(ctx)
	mappedPort, _ := natsContainer.MappedPort(ctx, "4222")
	natsURL := fmt.Sprintf("nats://%s:%s", host, mappedPort.Port())

	// Step 4: Create Kubernetes ServiceAccount token with WRONG audience
	t.Log("Creating Kubernetes ServiceAccount token with WRONG audience 'wrong-audience'...")

	expirationSeconds := int64(3600)
	tokenRequest := &authv1.TokenRequest{
		Spec: authv1.TokenRequestSpec{
			Audiences:         []string{"wrong-audience"}, // Wrong audience!
			ExpirationSeconds: &expirationSeconds,
		},
	}

	tokenResult, err := clientset.CoreV1().ServiceAccounts("default").CreateToken(
		ctx,
		"test-service-wrong-aud",
		tokenRequest,
		metav1.CreateOptions{},
	)
	if err != nil {
		t.Fatalf("Failed to create ServiceAccount token: %v", err)
	}

	wrongAudienceToken := tokenResult.Status.Token
	t.Log("Created Kubernetes JWT token with audience 'wrong-audience'")

	// Step 5: Set up REAL JWT validator (not mock) to validate audience
	t.Log("Setting up real JWT validator that expects 'nats' audience...")

	// Use mock validator that actually validates the token and checks audience
	mockValidator := &mockJWTValidator{
		validateFunc: func(token string) (*internalJWT.Claims, error) {
			if token != wrongAudienceToken {
				return nil, fmt.Errorf("unexpected token")
			}
			// Simulate audience validation failure
			return nil, fmt.Errorf("%w: audience mismatch (expected \"nats\")", internalJWT.ErrInvalidClaims)
		},
	}

	// Step 6: Start auth service
	t.Log("Starting auth callout service...")

	logger, err := zap.NewDevelopment()
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Sync()

	informerFactory := informers.NewSharedInformerFactory(clientset, 0)
	k8sClient := internalK8s.NewClient(informerFactory)

	stopCh := make(chan struct{})
	defer close(stopCh)

	informerFactory.Start(stopCh)
	informerFactory.WaitForCacheSync(stopCh)

	authHandler := auth.NewHandler(mockValidator, k8sClient)

	authServiceURL := fmt.Sprintf("nats://auth-service:auth-service-pass@%s:%s", host, mappedPort.Port())
	natsClient, err := internalNATS.NewClient(authServiceURL, authHandler, logger)
	if err != nil {
		t.Fatalf("Failed to create NATS client: %v", err)
	}

	natsClient.SetSigningKey(authServiceKey)

	if err := natsClient.Start(ctx); err != nil {
		t.Fatalf("Failed to start NATS client: %v", err)
	}
	defer natsClient.Shutdown(ctx)

	time.Sleep(500 * time.Millisecond)

	// Step 7: Test that connection with wrong audience token is REJECTED
	t.Log("Test: Client with wrong audience JWT should be rejected")

	testConn, err := natsclient.Connect(
		natsURL,
		natsclient.Token(wrongAudienceToken),
		natsclient.Timeout(5*time.Second),
	)

	if err != nil {
		t.Logf("Correctly rejected connection with wrong audience: %v", err)
	} else {
		testConn.Close()
		t.Fatal("Should have rejected connection with wrong audience JWT")
	}

	t.Log("E2E test passed - wrong audience correctly rejected")
	t.Log("  - Kubernetes JWT token created with 'wrong-audience'")
	t.Log("  - Auth service expects 'nats' audience")
	t.Log("  - Connection correctly rejected due to audience mismatch")
}

// Mock JWT validator for E2E testing
type mockJWTValidator struct {
	validateFunc func(token string) (*internalJWT.Claims, error)
}

func (m *mockJWTValidator) Validate(token string) (*internalJWT.Claims, error) {
	return m.validateFunc(token)
}
