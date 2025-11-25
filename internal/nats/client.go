package nats

import (
	"context"
	"fmt"
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
	authHandler AuthHandler
	conn        *natsclient.Conn
	service     *callout.AuthorizationService
	signingKey  nkeys.KeyPair
	logger      *zap.Logger
}

// NewClient creates a new NATS auth callout client
func NewClient(url string, authHandler AuthHandler, logger *zap.Logger) (*Client, error) {
	// Generate signing key for responses
	signingKey, err := nkeys.CreateAccount()
	if err != nil {
		return nil, fmt.Errorf("failed to create signing key: %w", err)
	}

	return &Client{
		url:         url,
		authHandler: authHandler,
		signingKey:  signingKey,
		logger:      logger,
	}, nil
}

// SetSigningKey sets the signing key for the client (useful for testing)
func (c *Client) SetSigningKey(key nkeys.KeyPair) {
	c.signingKey = key
}

// Start connects to NATS and starts the auth callout service
func (c *Client) Start(ctx context.Context) error {
	// Connect to NATS with timeout
	conn, err := natsclient.Connect(c.url,
		natsclient.Timeout(5*time.Second),
		natsclient.Name("nats-k8s-oidc-callout"),
	)
	if err != nil {
		return fmt.Errorf("failed to connect to NATS: %w", err)
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

		// Set the account this user belongs to
		// Use "$G" for the global account (NATS special value)
		uc.Audience = "$G"

		uc.Pub.Allow.Add(authResp.PublishPermissions...)
		uc.Sub.Allow.Add(authResp.SubscribePermissions...)
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
		c.service.Stop()
	}

	if c.conn != nil {
		c.conn.Close()
	}

	return nil
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
