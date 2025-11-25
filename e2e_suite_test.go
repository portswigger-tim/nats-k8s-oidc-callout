// +build e2e

package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	natsclient "github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/k3s"
	"github.com/testcontainers/testcontainers-go/wait"
	authv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// findAvailablePort finds an available ephemeral port
func findAvailablePort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to find available port: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().(*net.TCPAddr)
	return addr.Port
}

// buildAuthServiceBinary builds the auth-service binary for E2E tests
func buildAuthServiceBinary(t *testing.T) string {
	t.Helper()
	t.Log("Building auth-service binary...")

	binaryPath := filepath.Join(os.TempDir(), "nats-k8s-oidc-callout-e2e-test")

	cmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/server")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build binary: %v\nOutput: %s", err, string(output))
	}

	t.Logf("Binary built: %s", binaryPath)
	return binaryPath
}

// createNATSCredsFileWithKey creates a NATS credentials file with the auth service account key
// This key is used to sign authorization responses that are validated by the NATS server
func createNATSCredsFileWithKey(t *testing.T, publicKey string, seed []byte) string {
	t.Helper()

	credsFile, err := os.CreateTemp("", "nats-creds-*.creds")
	if err != nil {
		t.Fatalf("Failed to create credentials file: %v", err)
	}

	// Write credentials file with the actual auth service account key
	// The auth service will use this seed to sign authorization response JWTs
	// The NATS server validates these JWTs using the public key in auth_callout.issuer
	credsContent := fmt.Sprintf(`-----BEGIN NATS USER JWT-----
eyJ0eXAiOiJKV1QiLCJhbGciOiJlZDI1NTE5LW5rZXkifQ.eyJqdGkiOiJEVU1NWSIsImlhdCI6MTYwMDAwMDAwMCwiaXNzIjoiJUciLCJuYW1lIjoiYXV0aC1zZXJ2aWNlIiwic3ViIjoiJUciLCJuYXRzIjp7InB1YiI6e30sInN1YiI6e30sInN1YnMiOi0xLCJkYXRhIjotMSwicGF5bG9hZCI6LTEsInR5cGUiOiJ1c2VyIiwidmVyc2lvbiI6Mn19.ZHVtbXktc2lnbmF0dXJl
------END NATS USER JWT------

-----BEGIN USER NKEY SEED-----
%s
------END USER NKEY SEED------
`, seed)

	if _, err := credsFile.WriteString(credsContent); err != nil {
		credsFile.Close()
		os.Remove(credsFile.Name())
		t.Fatalf("Failed to write credentials: %v", err)
	}

	credsFile.Close()
	t.Logf("Created credentials file with auth service key (pub=%s): %s", publicKey, credsFile.Name())
	return credsFile.Name()
}

// waitForAuthService waits for the auth service to be ready by checking its health endpoint
func waitForAuthService(t *testing.T, port int, timeout time.Duration) error {
	t.Helper()

	healthURL := fmt.Sprintf("http://localhost:%d/health", port)
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		resp, err := http.Get(healthURL)
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			t.Log("Auth service is ready")
			return nil
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("auth service did not become ready within %v", timeout)
}

// E2ETestSuite holds shared test infrastructure
type E2ETestSuite struct {
	ctx               context.Context
	k3sContainer      *k3s.K3sContainer
	natsContainer     testcontainers.Container
	authServiceCmd    *exec.Cmd
	authServiceBinary string
	authServiceCreds  string
	jwksFile          string
	clientset         *kubernetes.Clientset
	kubeconfigFile    string
	natsURL           string
	authServiceKey    nkeys.KeyPair
	authServicePort   int
}

// SetupSuite initializes shared infrastructure once for all tests
func setupE2ESuite(t *testing.T) *E2ETestSuite {
	t.Helper()
	ctx := context.Background()
	suite := &E2ETestSuite{
		ctx: ctx,
	}

	// Step 0: Find available port for HTTP server
	suite.authServicePort = findAvailablePort(t)
	t.Logf("Using ephemeral port for auth service: %d", suite.authServicePort)

	// Step 1: Build auth-service binary (fast: ~1-2s)
	suite.authServiceBinary = buildAuthServiceBinary(t)

	// Step 2: Start k3s cluster (expensive: ~8-10s)
	t.Log("Starting shared k3s cluster...")
	k3sContainer, err := k3s.Run(ctx, "rancher/k3s:v1.31.3-k3s1")
	if err != nil {
		t.Fatalf("Failed to start k3s: %v", err)
	}
	suite.k3sContainer = k3sContainer

	// Get kubeconfig
	kubeConfigYAML, err := k3sContainer.GetKubeConfig(ctx)
	if err != nil {
		k3sContainer.Terminate(ctx)
		t.Fatalf("Failed to get kubeconfig: %v", err)
	}

	// Write kubeconfig to temp file
	kubeconfigFile, err := os.CreateTemp("", "kubeconfig-*.yaml")
	if err != nil {
		k3sContainer.Terminate(ctx)
		t.Fatalf("Failed to create kubeconfig file: %v", err)
	}
	suite.kubeconfigFile = kubeconfigFile.Name()

	if _, err := kubeconfigFile.Write(kubeConfigYAML); err != nil {
		k3sContainer.Terminate(ctx)
		os.Remove(suite.kubeconfigFile)
		t.Fatalf("Failed to write kubeconfig: %v", err)
	}
	kubeconfigFile.Close()

	t.Logf("k3s cluster started, kubeconfig: %s", suite.kubeconfigFile)

	// Create Kubernetes clientset
	config, err := clientcmd.BuildConfigFromFlags("", suite.kubeconfigFile)
	if err != nil {
		suite.Cleanup(t)
		t.Fatalf("Failed to build config: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		suite.Cleanup(t)
		t.Fatalf("Failed to create clientset: %v", err)
	}
	suite.clientset = clientset

	// Step 3: Start NATS server (quick: ~1s)
	t.Log("Starting shared NATS server...")
	authServiceKey, err := nkeys.CreateAccount()
	if err != nil {
		suite.Cleanup(t)
		t.Fatalf("Failed to create auth service account key: %v", err)
	}
	suite.authServiceKey = authServiceKey

	authServicePub, err := authServiceKey.PublicKey()
	if err != nil {
		suite.Cleanup(t)
		t.Fatalf("Failed to get auth service public key: %v", err)
	}

	natsConfig := fmt.Sprintf(`
port: 4222
debug: true
trace: true

authorization {
	users: [
		{ user: "auth-service", password: "auth-service-pass" }
	]

	auth_callout {
		issuer: %s
		auth_users: [ "auth-service" ]
	}
}
`, authServicePub)

	natsContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "nats:latest",
			ExposedPorts: []string{"4222/tcp"},
			Cmd:          []string{"-c", "/etc/nats/nats-server.conf"},
			Files: []testcontainers.ContainerFile{
				{
					HostFilePath:      "",
					ContainerFilePath: "/etc/nats/nats-server.conf",
					FileMode:          0644,
					Reader:            strings.NewReader(natsConfig),
				},
			},
			WaitingFor: wait.ForLog("Server is ready"),
		},
		Started: true,
	})
	if err != nil {
		suite.Cleanup(t)
		t.Fatalf("Failed to start NATS container: %v", err)
	}
	suite.natsContainer = natsContainer

	natsHost, err := natsContainer.Host(ctx)
	if err != nil {
		suite.Cleanup(t)
		t.Fatalf("Failed to get NATS host: %v", err)
	}

	natsPort, err := natsContainer.MappedPort(ctx, "4222")
	if err != nil {
		suite.Cleanup(t)
		t.Fatalf("Failed to get NATS port: %v", err)
	}

	suite.natsURL = fmt.Sprintf("nats://%s:%s", natsHost, natsPort.Port())
	t.Logf("NATS server started at: %s", suite.natsURL)

	// Step 4: Create NATS credentials file with the auth service key
	t.Log("Creating NATS credentials file with auth service key...")
	authServiceSeed, err := authServiceKey.Seed()
	if err != nil {
		suite.Cleanup(t)
		t.Fatalf("Failed to get auth service seed: %v", err)
	}
	suite.authServiceCreds = createNATSCredsFileWithKey(t, authServicePub, authServiceSeed)

	// Step 5: Fetch JWKS from k3s and save to file (avoids TLS cert verification issues)
	t.Log("Fetching JWKS from k3s...")
	// K3s uses the cluster-internal service name as the issuer
	jwtIssuer := "https://kubernetes.default.svc.cluster.local"
	t.Logf("Using JWT issuer: %s", jwtIssuer)

	// Use the Kubernetes client to fetch JWKS (it already trusts the k3s CA from kubeconfig)
	jwksData, err := clientset.RESTClient().Get().AbsPath("/openid/v1/jwks").DoRaw(ctx)
	if err != nil {
		suite.Cleanup(t)
		t.Fatalf("Failed to fetch JWKS: %v", err)
	}

	// Write JWKS to temp file
	jwksFile, err := os.CreateTemp("", "jwks-*.json")
	if err != nil {
		suite.Cleanup(t)
		t.Fatalf("Failed to create JWKS file: %v", err)
	}
	if _, err := jwksFile.Write(jwksData); err != nil {
		jwksFile.Close()
		os.Remove(jwksFile.Name())
		suite.Cleanup(t)
		t.Fatalf("Failed to write JWKS: %v", err)
	}
	jwksFile.Close()
	suite.jwksFile = jwksFile.Name()
	t.Logf("JWKS saved to: %s", suite.jwksFile)

	// Step 6: Start auth service as separate process
	t.Log("Starting auth service binary...")

	// Configure NATS URL with username/password authentication (actual auth mechanism)
	natsURLWithAuth := fmt.Sprintf("nats://auth-service:auth-service-pass@%s", strings.TrimPrefix(suite.natsURL, "nats://"))

	// Configure environment variables for auth service
	cmd := exec.Command(suite.authServiceBinary)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("NATS_URL=%s", natsURLWithAuth),
		fmt.Sprintf("NATS_CREDS_FILE=%s", suite.authServiceCreds),
		fmt.Sprintf("NATS_ACCOUNT=%s", authServicePub),
		fmt.Sprintf("JWKS_PATH=%s", suite.jwksFile), // Use file-based JWKS (no TLS issues)
		fmt.Sprintf("JWT_ISSUER=%s", jwtIssuer),
		fmt.Sprintf("JWT_AUDIENCE=nats"),
		fmt.Sprintf("PORT=%d", suite.authServicePort),
		fmt.Sprintf("LOG_LEVEL=debug"),
		fmt.Sprintf("KUBECONFIG=%s", suite.kubeconfigFile),
		"K8S_IN_CLUSTER=false",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		suite.Cleanup(t)
		t.Fatalf("Failed to start auth service: %v", err)
	}
	suite.authServiceCmd = cmd

	// Wait for auth service to be ready
	if err := waitForAuthService(t, suite.authServicePort, 10*time.Second); err != nil {
		suite.Cleanup(t)
		t.Fatalf("Auth service did not start: %v", err)
	}

	t.Log("Auth service started successfully")

	return suite
}

// Cleanup tears down shared infrastructure
func (s *E2ETestSuite) Cleanup(t *testing.T) {
	t.Helper()

	// Stop auth service process
	if s.authServiceCmd != nil && s.authServiceCmd.Process != nil {
		t.Log("Stopping auth service...")
		s.authServiceCmd.Process.Kill()
		s.authServiceCmd.Wait()
	}

	// Clean up temporary files
	if s.authServiceBinary != "" {
		os.Remove(s.authServiceBinary)
	}
	if s.authServiceCreds != "" {
		os.Remove(s.authServiceCreds)
	}
	if s.jwksFile != "" {
		os.Remove(s.jwksFile)
	}
	if s.kubeconfigFile != "" {
		os.Remove(s.kubeconfigFile)
	}

	// Terminate containers
	if s.natsContainer != nil {
		s.natsContainer.Terminate(s.ctx)
	}
	if s.k3sContainer != nil {
		s.k3sContainer.Terminate(s.ctx)
	}
}

// CreateServiceAccount creates a ServiceAccount for a test
func (s *E2ETestSuite) CreateServiceAccount(t *testing.T, name string, annotations map[string]string) {
	t.Helper()
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   "default",
			Annotations: annotations,
		},
	}

	_, err := s.clientset.CoreV1().ServiceAccounts("default").Create(s.ctx, sa, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create ServiceAccount %s: %v", name, err)
	}
	t.Logf("Created ServiceAccount: %s", name)
}

// DeleteServiceAccount deletes a ServiceAccount after a test
func (s *E2ETestSuite) DeleteServiceAccount(t *testing.T, name string) {
	t.Helper()
	err := s.clientset.CoreV1().ServiceAccounts("default").Delete(s.ctx, name, metav1.DeleteOptions{})
	if err != nil {
		t.Logf("Warning: Failed to delete ServiceAccount %s: %v", name, err)
	}
}

// CreateToken creates a Kubernetes ServiceAccount token
func (s *E2ETestSuite) CreateToken(t *testing.T, serviceAccountName, audience string) string {
	t.Helper()
	treq := &authv1.TokenRequest{
		Spec: authv1.TokenRequestSpec{
			Audiences:         []string{audience},
			ExpirationSeconds: func() *int64 { i := int64(3600); return &i }(),
		},
	}

	tokenRequest, err := s.clientset.CoreV1().ServiceAccounts("default").CreateToken(
		s.ctx, serviceAccountName, treq, metav1.CreateOptions{},
	)
	if err != nil {
		t.Fatalf("Failed to create token for %s: %v", serviceAccountName, err)
	}

	return tokenRequest.Status.Token
}

// TestE2ESuite runs all E2E tests with shared infrastructure
func TestE2ESuite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E tests in short mode")
	}

	// Set up shared infrastructure once
	suite := setupE2ESuite(t)
	defer suite.Cleanup(t)

	// Run subtests
	t.Run("BasicAuthFlow", func(t *testing.T) {
		testBasicAuthFlow(t, suite)
	})

	t.Run("WrongAudience", func(t *testing.T) {
		testWrongAudience(t, suite)
	})

	t.Run("MaxMsgsOneResponseLimit", func(t *testing.T) {
		testMaxMsgsOneResponseLimit(t, suite)
	})

	t.Run("PrivateInboxPattern", func(t *testing.T) {
		testPrivateInboxPattern(t, suite)
	})
}

// testBasicAuthFlow tests the complete auth callout flow
func testBasicAuthFlow(t *testing.T, suite *E2ETestSuite) {
	// Create ServiceAccount
	suite.CreateServiceAccount(t, "test-service", map[string]string{
		"nats.io/allowed-pub-subjects": "test.>, events.>",
		"nats.io/allowed-sub-subjects": "test.>, commands.*, _INBOX.>",
	})
	defer suite.DeleteServiceAccount(t, "test-service")

	// Wait for informer to sync the new ServiceAccount
	time.Sleep(200 * time.Millisecond)

	// Create JWT token
	token := suite.CreateToken(t, "test-service", "nats")
	t.Log("Created real Kubernetes JWT token with audience 'nats'")

	// Connect client with JWT
	nc, err := natsclient.Connect(suite.natsURL, natsclient.Token(token))
	if err != nil {
		t.Fatalf("Failed to connect to NATS: %v", err)
	}
	defer nc.Close()
	t.Log("Client connected successfully with JWT")

	// Test 1: Publishing to allowed subjects should succeed
	t.Log("Test 1: Publishing to allowed subjects should succeed")
	allowedSubjects := []string{"test.foo", "test.bar.baz", "events.system"}
	for _, subj := range allowedSubjects {
		if err := nc.Publish(subj, []byte("test message")); err != nil {
			t.Errorf("Failed to publish to allowed subject %s: %v", subj, err)
		} else {
			t.Logf("Published to allowed subject: %s", subj)
		}
	}

	// Test 2: Publishing to disallowed subjects should fail
	t.Log("Test 2: Publishing to disallowed subjects should fail")
	disallowedSubjects := []string{"production.events", "admin.commands", "other-namespace.foo"}
	for _, subj := range disallowedSubjects {
		if err := nc.Publish(subj, []byte("test message")); err != nil {
			t.Logf("Correctly rejected publish to disallowed subject %s: %v", subj, err)
		} else {
			nc.Flush()
			if lastErr := nc.LastError(); lastErr != nil {
				t.Logf("Correctly rejected publish to disallowed subject %s: %v", subj, lastErr)
			} else {
				t.Errorf("Should not be able to publish to disallowed subject: %s", subj)
			}
		}
	}

	// Test 3: Subscribing to allowed subjects should succeed
	t.Log("Test 3: Subscribing to allowed subjects should succeed")
	sub1, err := nc.SubscribeSync("test.bar")
	if err != nil {
		t.Errorf("Failed to subscribe to allowed subject: %v", err)
	} else {
		t.Log("Subscribed to allowed subject: test.bar")
		sub1.Unsubscribe()
	}

	sub2, err := nc.SubscribeSync("commands.start")
	if err != nil {
		t.Errorf("Failed to subscribe to allowed subject: %v", err)
	} else {
		t.Log("Subscribed to allowed subject: commands.start")
		sub2.Unsubscribe()
	}

	// Test 4: Subscribing to disallowed subjects should fail
	t.Log("Test 4: Subscribing to disallowed subjects should fail")
	disallowedSubs := []string{"production.events", "admin.commands"}
	for _, subj := range disallowedSubs {
		sub, err := nc.SubscribeSync(subj)
		if err != nil {
			t.Logf("Correctly rejected subscription to disallowed subject %s: %v", subj, err)
		} else {
			nc.Flush()
			if lastErr := nc.LastError(); lastErr != nil {
				t.Logf("Correctly rejected subscription to disallowed subject %s: %v", subj, lastErr)
			} else {
				t.Errorf("Should not be able to subscribe to disallowed subject: %s", subj)
			}
			sub.Unsubscribe()
		}
	}

	// Test 5: Full pub/sub message flow
	t.Log("Test 5: Full pub/sub message flow")
	msgReceived := make(chan string, 1)
	sub, err := nc.Subscribe("test.messages", func(msg *natsclient.Msg) {
		msgReceived <- string(msg.Data)
	})
	if err != nil {
		t.Fatalf("Failed to create subscription: %v", err)
	}
	defer sub.Unsubscribe()

	testMsg := "Hello from E2E test"
	if err := nc.Publish("test.messages", []byte(testMsg)); err != nil {
		t.Fatalf("Failed to publish message: %v", err)
	}

	select {
	case received := <-msgReceived:
		if received != testMsg {
			t.Errorf("Wrong message: got %q, want %q", received, testMsg)
		} else {
			t.Logf("Successfully published and received message: %s", received)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for message")
	}

	// Test 6: Request-reply pattern
	t.Log("Test 6: Request-reply pattern (validates _INBOX.> permissions)")
	replySub, err := nc.Subscribe("test.request", func(msg *natsclient.Msg) {
		msg.Respond([]byte("pong"))
	})
	if err != nil {
		t.Fatalf("Failed to create reply subscription: %v", err)
	}
	defer replySub.Unsubscribe()

	resp, err := nc.Request("test.request", []byte("ping"), 2*time.Second)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if string(resp.Data) != "pong" {
		t.Errorf("Wrong response: got %q, want %q", string(resp.Data), "pong")
	} else {
		t.Log("Request-reply pattern successful - _INBOX.> permissions working")
	}

	// Test 7: Client without JWT should be rejected
	t.Log("Test 7: Client without JWT should be rejected")
	_, err = natsclient.Connect(suite.natsURL)
	if err == nil {
		t.Error("Client without JWT should be rejected")
	} else {
		t.Logf("Correctly rejected connection without JWT: %v", err)
	}

	t.Log("✅ BasicAuthFlow test passed")
}

// testWrongAudience tests that JWT with wrong audience is rejected
func testWrongAudience(t *testing.T, suite *E2ETestSuite) {
	// Create ServiceAccount
	suite.CreateServiceAccount(t, "test-service-wrong-aud", map[string]string{
		"nats.io/allowed-pub-subjects": "test.>",
		"nats.io/allowed-sub-subjects": "test.>",
	})
	defer suite.DeleteServiceAccount(t, "test-service-wrong-aud")

	// Wait for informer to sync the new ServiceAccount
	time.Sleep(200 * time.Millisecond)

	// Create JWT token with WRONG audience
	token := suite.CreateToken(t, "test-service-wrong-aud", "wrong-audience")
	t.Log("Created Kubernetes JWT token with audience 'wrong-audience'")

	// Try to connect - should be rejected
	_, err := natsclient.Connect(suite.natsURL, natsclient.Token(token))
	if err == nil {
		t.Fatal("Client with wrong audience should be rejected")
	}
	t.Logf("Correctly rejected connection with wrong audience: %v", err)

	t.Log("✅ WrongAudience test passed")
}

// testMaxMsgsOneResponseLimit tests MaxMsgs: 1 response limitation
func testMaxMsgsOneResponseLimit(t *testing.T, suite *E2ETestSuite) {
	// Create ServiceAccount
	suite.CreateServiceAccount(t, "test-maxmsgs", map[string]string{
		"nats.io/allowed-pub-subjects": "test.>",
		"nats.io/allowed-sub-subjects": "test.>, _INBOX.>",
	})
	defer suite.DeleteServiceAccount(t, "test-maxmsgs")

	// Wait for informer to sync the new ServiceAccount
	time.Sleep(200 * time.Millisecond)

	// Create JWT token and connect
	token := suite.CreateToken(t, "test-maxmsgs", "nats")
	nc, err := natsclient.Connect(suite.natsURL, natsclient.Token(token))
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer nc.Close()

	// Test: Responder should only be able to send ONE reply
	t.Log("Test: Responder should only be able to send ONE reply (MaxMsgs: 1)")

	var replyInbox string
	firstReplyErr := make(chan error, 1)
	secondReplyErr := make(chan error, 1)

	// Set up responder
	sub, err := nc.Subscribe("test.maxmsgs-request", func(msg *natsclient.Msg) {
		replyInbox = msg.Reply
		t.Logf("Responder received request, reply inbox: %s", replyInbox)

		// First reply - should succeed
		if err := msg.Respond([]byte("first reply")); err != nil {
			firstReplyErr <- err
		} else {
			firstReplyErr <- nil
			t.Log("First reply sent successfully")
		}

		// Second reply - should fail (MaxMsgs: 1)
		time.Sleep(100 * time.Millisecond)
		if err := nc.Publish(replyInbox, []byte("second reply")); err != nil {
			secondReplyErr <- err
		} else {
			nc.Flush()
			if lastErr := nc.LastError(); lastErr != nil {
				secondReplyErr <- lastErr
			} else {
				secondReplyErr <- nil
			}
		}
	})
	if err != nil {
		t.Fatalf("Failed to create responder: %v", err)
	}
	defer sub.Unsubscribe()

	// Send request
	t.Log("Sending request...")
	resp, err := nc.Request("test.maxmsgs-request", []byte("test request"), 2*time.Second)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if string(resp.Data) != "first reply" {
		t.Errorf("Wrong response: got %q, want %q", string(resp.Data), "first reply")
	}
	t.Log("Received first reply successfully")

	// Check first reply error (should be nil)
	if err := <-firstReplyErr; err != nil {
		t.Errorf("First reply should succeed but got error: %v", err)
	} else {
		t.Log("✅ First reply succeeded (expected)")
	}

	// Check second reply error (should fail)
	if err := <-secondReplyErr; err != nil {
		t.Logf("✅ Second reply correctly rejected: %v", err)
	} else {
		t.Error("Second reply should fail due to MaxMsgs: 1 limitation")
	}

	t.Log("✅ MaxMsgsOneResponseLimit test passed")
}

// testPrivateInboxPattern tests private inbox isolation between ServiceAccounts
func testPrivateInboxPattern(t *testing.T, suite *E2ETestSuite) {
	// Create two ServiceAccounts with private inbox permissions
	suite.CreateServiceAccount(t, "service-a", map[string]string{
		"nats.io/allowed-pub-subjects": "test.>",
		"nats.io/allowed-sub-subjects": "_INBOX_default_service-a.>, test.>, _INBOX.>",
	})
	defer suite.DeleteServiceAccount(t, "service-a")

	suite.CreateServiceAccount(t, "service-b", map[string]string{
		"nats.io/allowed-pub-subjects": "test.>",
		"nats.io/allowed-sub-subjects": "_INBOX_default_service-b.>, test.>, _INBOX.>",
	})
	defer suite.DeleteServiceAccount(t, "service-b")

	// Wait for informer to sync the new ServiceAccounts
	time.Sleep(200 * time.Millisecond)

	// Create tokens and connect both services
	tokenA := suite.CreateToken(t, "service-a", "nats")
	tokenB := suite.CreateToken(t, "service-b", "nats")

	connA, err := natsclient.Connect(suite.natsURL,
		natsclient.Token(tokenA),
		natsclient.CustomInboxPrefix("_INBOX_default_service-a"),
	)
	if err != nil {
		t.Fatalf("Failed to connect service-a: %v", err)
	}
	defer connA.Close()

	connB, err := natsclient.Connect(suite.natsURL,
		natsclient.Token(tokenB),
	)
	if err != nil {
		t.Fatalf("Failed to connect service-b: %v", err)
	}
	defer connB.Close()

	// Test 1: Service-a using private inbox pattern
	t.Log("Test 1: Service-a using private inbox pattern")
	responderA, err := connA.Subscribe("test.private-inbox-request", func(msg *natsclient.Msg) {
		t.Logf("Service-a responder: received request, reply inbox: %s", msg.Reply)
		msg.Respond([]byte("response from service-a"))
	})
	if err != nil {
		t.Fatalf("Failed to create responder on service-a: %v", err)
	}
	defer responderA.Unsubscribe()

	respA, err := connA.Request("test.private-inbox-request", []byte("request from service-a"), 2*time.Second)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if string(respA.Data) != "response from service-a" {
		t.Errorf("Wrong response: got %q, want %q", string(respA.Data), "response from service-a")
	}
	t.Log("✅ Private inbox request-reply successful")

	// Test 2: Service-b trying to eavesdrop on service-a's private inbox
	t.Log("Test 2: Service-b trying to eavesdrop on service-a's private inbox")
	privateInboxA := "_INBOX_default_service-a.test123"
	subB, err := connB.SubscribeSync(privateInboxA)
	if err != nil {
		t.Logf("✅ Eavesdrop correctly rejected (immediate error): %v", err)
	} else {
		connB.Flush()
		if lastErr := connB.LastError(); lastErr != nil {
			t.Logf("✅ Eavesdrop correctly rejected (permission denied): %v", lastErr)
		} else {
			t.Error("Service-b should NOT be able to subscribe to service-a's private inbox")
		}
		subB.Unsubscribe()
	}

	// Test 3: Service-b using standard inbox (works)
	t.Log("Test 3: Service-b using standard inbox pattern (_INBOX.>)")
	responderB, err := connB.Subscribe("test.standard-inbox-request", func(msg *natsclient.Msg) {
		t.Logf("Service-b responder: received request, reply inbox: %s", msg.Reply)
		msg.Respond([]byte("response from service-b"))
	})
	if err != nil {
		t.Fatalf("Failed to create responder on service-b: %v", err)
	}
	defer responderB.Unsubscribe()

	respB, err := connB.Request("test.standard-inbox-request", []byte("request from service-b"), 2*time.Second)
	if err != nil {
		t.Fatalf("Request failed with standard inbox: %v", err)
	}
	if string(respB.Data) != "response from service-b" {
		t.Errorf("Wrong response: got %q, want %q", string(respB.Data), "response from service-b")
	}
	t.Log("✅ Standard inbox request-reply successful")

	// Test 4: Cross-service request-reply (service-a private inbox → service-b responds)
	t.Log("Test 4: Cross-service request-reply with private inbox")
	responderCrossService, err := connB.Subscribe("test.cross-service-request", func(msg *natsclient.Msg) {
		t.Logf("Service-b responder: received request from service-a, reply inbox: %s", msg.Reply)
		// Service-b responds to service-a's private inbox via allow_responses (MaxMsgs: 1)
		if err := msg.Respond([]byte("response from service-b to service-a")); err != nil {
			t.Errorf("Service-b failed to respond: %v", err)
		}
	})
	if err != nil {
		t.Fatalf("Failed to create cross-service responder on service-b: %v", err)
	}
	defer responderCrossService.Unsubscribe()

	// Service-a makes request using private inbox, service-b responds
	respCrossService, err := connA.Request("test.cross-service-request", []byte("request from service-a to service-b"), 2*time.Second)
	if err != nil {
		t.Fatalf("Cross-service request failed: %v", err)
	}
	if string(respCrossService.Data) != "response from service-b to service-a" {
		t.Errorf("Wrong cross-service response: got %q, want %q", string(respCrossService.Data), "response from service-b to service-a")
	}
	t.Log("✅ Cross-service request-reply with private inbox successful")

	t.Log("✅ PrivateInboxPattern test passed")
}
