// Package nats provides NATS authentication callout service integration.
package nats

import (
	"bufio"
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nats-io/jwt/v2"
	natsclient "github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
	"github.com/synadia-io/callout.go"
	"go.uber.org/zap"

	"github.com/portswigger-tim/nats-k8s-oidc-callout/internal/auth"
	"github.com/portswigger-tim/nats-k8s-oidc-callout/internal/logging"
)

const (
	// DefaultTokenExpiry is the default expiry time for generated NATS user tokens
	DefaultTokenExpiry = 5 * time.Minute
)

// AuthHandler defines the interface for authorization
type AuthHandler interface {
	Authorize(req *auth.AuthRequest) *auth.AuthResponse
}

// Client manages NATS connection and auth callout subscription
type Client struct {
	url         string
	credsFile   string // User credentials file (optional)
	token       string // Token for authentication (optional)
	authHandler AuthHandler
	conn        *natsclient.Conn
	service     *callout.AuthorizationService
	signingKey  nkeys.KeyPair
	logger      *zap.Logger
}

// NewClient creates a new NATS auth callout client.
//
// Authentication Strategy:
// The client supports three NATS connection authentication methods:
//  1. URL-embedded credentials (simplest): nats://user:pass@host:port
//     Pass empty userCredsFile and empty token.
//  2. User credentials file (production): Separate .creds file with user JWT + user key
//     Pass non-empty userCredsFile path, empty token.
//  3. Token authentication: Static token for connection
//     Pass empty userCredsFile, non-empty token.
//
// The signing key must be loaded separately using SetSigningKey() before calling Start().
// The signing key is used to sign authorization response JWTs and must be an account private key.
func NewClient(natsURL, userCredsFile, token string, authHandler AuthHandler, logger *zap.Logger) (*Client, error) {
	// Validate user credentials file if provided
	if userCredsFile != "" {
		// Clean and validate the path to prevent path traversal attacks
		cleanPath := filepath.Clean(userCredsFile)
		if cleanPath != userCredsFile {
			return nil, fmt.Errorf("invalid user credentials file path: potential path traversal attempt")
		}

		// Check if file exists and is accessible
		fileInfo, err := os.Stat(userCredsFile)
		if err != nil {
			return nil, fmt.Errorf("user credentials file validation failed: %w", err)
		}

		// Verify it's a regular file (not a directory or special file)
		if !fileInfo.Mode().IsRegular() {
			return nil, fmt.Errorf("user credentials file is not a regular file: %s", userCredsFile)
		}
	}

	// Validate mutually exclusive auth methods
	if userCredsFile != "" && token != "" {
		return nil, fmt.Errorf("userCredsFile and token are mutually exclusive; provide at most one")
	}

	return &Client{
		url:         natsURL,
		credsFile:   userCredsFile, // User credentials file (optional)
		token:       token,
		authHandler: authHandler,
		logger:      logger,
	}, nil
}

// SetSigningKey sets the signing key for the client (useful for testing)
func (c *Client) SetSigningKey(key nkeys.KeyPair) {
	c.signingKey = key
}

// Start connects to NATS and starts the auth callout service
func (c *Client) Start(ctx context.Context) error {
	// Verify signing key is set
	if c.signingKey == nil {
		return fmt.Errorf("signing key not set; call SetSigningKey() before Start()")
	}

	// Build connection options with preallocated capacity
	opts := make([]natsclient.Option, 0, 4)
	opts = append(opts,
		natsclient.Timeout(5*time.Second),
		natsclient.Name("nats-k8s-oidc-callout"),
	)

	// Add authentication based on configured method
	// Priority: User credentials > Token > URL-embedded credentials
	authOpts, err := c.configureAuthentication()
	if err != nil {
		return fmt.Errorf("failed to configure authentication: %w", err)
	}
	opts = append(opts, authOpts...)

	// Connect to NATS
	conn, err := natsclient.Connect(c.url, opts...)
	if err != nil {
		return fmt.Errorf("failed to connect to NATS (url=%s, user_creds_file=%s): %w", c.url, c.credsFile, err)
	}
	c.conn = conn

	// Create authorizer function that bridges NATS and our auth handler
	authorizer := func(req *jwt.AuthorizationRequest) (string, error) {
		// Extract JWT token from request
		// The token is provided by the client in the connection options
		// For now, we'll extract it from the ConnectOptions if available
		token := c.extractToken(req)

		if token == "" {
			// Reject requests without a token by not returning a JWT
			// This causes the connection to timeout
			c.logger.Debug("auth request rejected: no token provided",
				zap.String("user_nkey", req.UserNkey))
			return "", fmt.Errorf("no token provided")
		}

		// Call our auth handler
		authReq := &auth.AuthRequest{
			Token: token,
		}

		c.logger.Debug("calling auth handler with token")
		authResp := c.authHandler.Authorize(authReq)

		c.logger.Debug("auth handler response",
			zap.Bool("allowed", authResp.Allowed),
			zap.Strings("publish_permissions", authResp.PublishPermissions),
			zap.Strings("subscribe_permissions", authResp.SubscribePermissions))

		// If denied, reject by not returning a JWT
		if !authResp.Allowed {
			c.logger.Debug("auth request denied",
				zap.String("user_nkey", req.UserNkey))
			return "", fmt.Errorf("authorization failed")
		}

		// Build NATS user claims
		uc := jwt.NewUserClaims(req.UserNkey)

		// Set the audience to the global account
		// $G is the NATS global account - simplest approach for single-tenant setups
		uc.Audience = "$G"

		uc.Pub.Allow.Add(authResp.PublishPermissions...)
		uc.Sub.Allow.Add(authResp.SubscribePermissions...)

		// Enable response permissions (equivalent to allow_responses: true)
		// This allows responders to publish to reply subjects during request handling
		// MaxMsgs: 1 = allow one response per request (NATS default)
		// Expires: 0 = no time limit
		uc.Resp = &jwt.ResponsePermission{
			MaxMsgs: 1,
			Expires: 0,
		}

		uc.Expires = time.Now().Add(DefaultTokenExpiry).Unix()

		c.logger.Debug("built user claims",
			zap.String("subject", uc.Subject),
			zap.String("audience", uc.Audience),
			zap.Any("pub_allow", uc.Pub.Allow),
			zap.Any("sub_allow", uc.Sub.Allow),
			zap.Int64("expires", uc.Expires))

		// Encode and return JWT
		encodedJWT, err := uc.Encode(c.signingKey)
		if err != nil {
			c.logger.Error("failed to encode auth response JWT",
				zap.Error(err),
				zap.String("user_nkey", req.UserNkey))
			return "", err
		}

		c.logger.Debug("encoded auth response JWT",
			zap.Int("jwt_length", len(encodedJWT)))

		return encodedJWT, nil
	}

	// Create auth callout service
	service, err := callout.NewAuthorizationService(
		conn,
		callout.Authorizer(authorizer),
		callout.ResponseSignerKey(c.signingKey),
	)
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to create authorization service: %w", err)
	}

	c.service = service
	return nil
}

// configureAuthentication configures NATS connection authentication options based on the configured method.
// Priority: User credentials > Token > URL-embedded credentials
func (c *Client) configureAuthentication() ([]natsclient.Option, error) {
	var opts []natsclient.Option

	if c.credsFile != "" {
		c.logger.Info("using user credentials file for NATS authentication",
			zap.String("user_creds_file", c.credsFile))
		opts = append(opts, natsclient.UserCredentials(c.credsFile))
		return opts, nil
	}

	if c.token != "" {
		c.logger.Info("using token for NATS authentication")
		opts = append(opts, natsclient.Token(c.token))
		return opts, nil
	}

	// URL-embedded credentials
	c.logger.Info("using URL-embedded credentials for NATS authentication (username/password in URL)")

	// Parse URL to extract credentials and create clean URL
	parsedURL, err := url.Parse(c.url)
	if err != nil {
		return nil, fmt.Errorf("failed to parse NATS URL: %w", err)
	}

	// Extract username and password from URL
	if parsedURL.User != nil {
		username := parsedURL.User.Username()
		password, hasPassword := parsedURL.User.Password()

		if hasPassword {
			// Use UserInfo option with extracted credentials
			opts = append(opts, natsclient.UserInfo(username, password))

			// Create clean URL without credentials for connection
			parsedURL.User = nil
			c.url = parsedURL.String()

			c.logger.Info("extracted credentials from URL, connecting with clean URL")
		}
	}

	return opts, nil
}

// Shutdown gracefully shuts down the client
func (c *Client) Shutdown(ctx context.Context) error {
	if c.service != nil {
		if err := c.service.Stop(); err != nil {
			c.logger.Error("failed to stop NATS service", zap.Error(err))
		}
	}

	if c.conn != nil {
		c.conn.Close()
	}

	return nil
}

// LoadSigningKeyFromFile loads an account signing key from a file.
//
// The file can be in one of two formats:
//  1. Plain seed format: Just the account seed (SA...) on a single line
//  2. NATS credentials format: Standard credentials file with seed section
//
// This function is designed to load ACCOUNT signing keys (starting with SA...),
// not user keys (SU...). Account keys are used to sign authorization response JWTs.
//
// Example plain seed file:
//
//	SAADGYQZI2OIVEXAMPLE...
//
// Example credentials format:
//
//	-----BEGIN NKEY SEED-----
//	SAADGYQZI2OIVEXAMPLE...
//	------END NKEY SEED------
func LoadSigningKeyFromFile(path string) (nkeys.KeyPair, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path comes from configuration
	if err != nil {
		return nil, fmt.Errorf("failed to read signing key file: %w", err)
	}

	// Try to extract seed from data
	seed := strings.TrimSpace(string(data))

	// If it looks like a credentials file format, extract the seed section
	if strings.Contains(seed, "BEGIN") {
		file, err := os.Open(path) //nolint:gosec // path comes from configuration
		if err != nil {
			return nil, fmt.Errorf("failed to open signing key file: %w", err)
		}
		defer func() {
			//nolint:errcheck,gosec // Error ignored - file opened read-only
			file.Close()
		}()

		seed, err = extractSeedFromFile(file)
		if err != nil {
			return nil, err
		}
	}

	// Parse the seed into a KeyPair
	kp, err := nkeys.FromSeed([]byte(seed))
	if err != nil {
		return nil, fmt.Errorf("failed to parse signing key: %w", err)
	}

	// Verify it's an account key (SA...)
	keyType, _, err := nkeys.DecodeSeed([]byte(seed))
	if err != nil {
		return nil, fmt.Errorf("failed to decode seed type: %w", err)
	}

	// Account keys start with prefix 'A' in the public key (e.g., "AA...")
	// and 'SA' in the seed (e.g., "SAADGYQZI...")
	if keyType != nkeys.PrefixByteAccount {
		return nil, fmt.Errorf("signing key must be an account private key (starts with SA...), got: %s", keyType.String())
	}

	return kp, nil
}

// LoadSigningKeyFromCredsFile parses a NATS credentials file and extracts the account seed
// Credentials file format:
//
//	-----BEGIN NATS USER JWT-----
//	<jwt>
//	------END NATS USER JWT------
//
//	-----BEGIN USER NKEY SEED-----
//	<seed>
//	------END USER NKEY SEED------
//
// Deprecated: Use LoadSigningKeyFromFile instead, which supports both formats
// and validates that the key is an account key.
func LoadSigningKeyFromCredsFile(path string) (nkeys.KeyPair, error) {
	return LoadSigningKeyFromFile(path)
}

// extractSeedFromFile scans a credentials file and extracts the seed value.
func extractSeedFromFile(file *os.File) (string, error) {
	var seed string
	inSeedSection := false

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if isSeedSectionBegin(line) {
			inSeedSection = true
			continue
		}

		if isSeedSectionEnd(line) {
			break
		}

		if inSeedSection && line != "" {
			seed = line
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("failed to read credentials file: %w", err)
	}

	if seed == "" {
		return "", fmt.Errorf("no seed found in credentials file")
	}

	return seed, nil
}

// isSeedSectionBegin checks if a line marks the beginning of the seed section.
func isSeedSectionBegin(line string) bool {
	return strings.Contains(line, "BEGIN USER NKEY SEED") || strings.Contains(line, "BEGIN NKEY SEED")
}

// isSeedSectionEnd checks if a line marks the end of the seed section.
func isSeedSectionEnd(line string) bool {
	return strings.Contains(line, "END USER NKEY SEED") || strings.Contains(line, "END NKEY SEED")
}

// extractToken extracts the JWT token from the authorization request
// The token should be provided by the client in the connection options
func (c *Client) extractToken(req *jwt.AuthorizationRequest) string {
	c.logger.Debug("extracting token from auth request",
		zap.String("jwt_field", logging.RedactJWT(req.ConnectOptions.JWT)),
		zap.String("token_field", logging.RedactJWT(req.ConnectOptions.Token)),
		zap.String("username", req.ConnectOptions.Username))

	// Check for JWT in connect options (standard field)
	if req.ConnectOptions.JWT != "" {
		c.logger.Debug("token found in JWT field")
		return req.ConnectOptions.JWT
	}

	// Alternative: check for auth_token field
	if req.ConnectOptions.Token != "" {
		c.logger.Debug("token found in Token field")
		return req.ConnectOptions.Token
	}

	c.logger.Debug("no token found in auth request")
	return ""
}
