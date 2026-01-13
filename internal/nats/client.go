// Package nats provides NATS authentication callout service integration.
package nats

import (
	"bufio"
	"context"
	"fmt"
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
	credsFile   string
	authHandler AuthHandler
	conn        *natsclient.Conn
	service     *callout.AuthorizationService
	signingKey  nkeys.KeyPair
	logger      *zap.Logger
}

// NewClient creates a new NATS auth callout client.
//
// Authentication Strategy:
// The client supports two NATS authentication methods:
//  1. User credentials file (recommended): Pass a non-empty credsFile path.
//     The file will be used for NATS connection authentication via UserCredentials().
//  2. Username/password: Include credentials in the NATS URL (nats://user:pass@host:port).
//     Pass an empty credsFile ("") to skip credential file authentication.
//
// If both methods are provided, the credentials file takes precedence in the NATS client.
// The signing key must be loaded separately using SetSigningKey() before calling Start().
func NewClient(url, credsFile string, authHandler AuthHandler, logger *zap.Logger) (*Client, error) {
	// Validate credentials file if provided
	if credsFile != "" {
		// Clean and validate the path to prevent path traversal attacks
		cleanPath := filepath.Clean(credsFile)
		if cleanPath != credsFile {
			return nil, fmt.Errorf("invalid credentials file path: potential path traversal attempt")
		}

		// Check if file exists and is accessible
		fileInfo, err := os.Stat(credsFile)
		if err != nil {
			return nil, fmt.Errorf("credentials file validation failed: %w", err)
		}

		// Verify it's a regular file (not a directory or special file)
		if !fileInfo.Mode().IsRegular() {
			return nil, fmt.Errorf("credentials file is not a regular file: %s", credsFile)
		}
	}

	return &Client{
		url:         url,
		credsFile:   credsFile,
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

	// Build connection options
	opts := []natsclient.Option{
		natsclient.Timeout(5 * time.Second),
		natsclient.Name("nats-k8s-oidc-callout"),
	}

	// Add credentials file authentication if provided
	if c.credsFile != "" {
		c.logger.Debug("using credentials file for NATS authentication",
			zap.String("creds_file", c.credsFile))
		opts = append(opts, natsclient.UserCredentials(c.credsFile))
	} else {
		c.logger.Debug("using URL-based authentication for NATS (no credentials file)")
	}

	// Connect to NATS
	conn, err := natsclient.Connect(c.url, opts...)
	if err != nil {
		return fmt.Errorf("failed to connect to NATS (url=%s, creds=%s): %w", c.url, c.credsFile, err)
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
func LoadSigningKeyFromCredsFile(path string) (nkeys.KeyPair, error) {
	file, err := os.Open(path) //nolint:gosec // path comes from configuration
	if err != nil {
		return nil, fmt.Errorf("failed to open credentials file: %w", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			// File close errors are not critical for read operations
			_ = err
		}
	}()

	seed, err := extractSeedFromFile(file)
	if err != nil {
		return nil, err
	}

	// Parse the seed into a KeyPair
	kp, err := nkeys.FromSeed([]byte(seed))
	if err != nil {
		return nil, fmt.Errorf("failed to parse seed: %w", err)
	}

	return kp, nil
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
		zap.String("jwt_field", req.ConnectOptions.JWT),
		zap.String("token_field", req.ConnectOptions.Token),
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
