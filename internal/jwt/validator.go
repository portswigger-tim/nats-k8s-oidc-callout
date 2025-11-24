package jwt

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/MicahParks/keyfunc/v2"
	"github.com/golang-jwt/jwt/v5"
)

// Validator handles JWT validation using JWKS keys.
type Validator struct {
	jwks     *keyfunc.JWKS
	issuer   string
	audience string
	timeFunc func() time.Time // Injectable time function for testing
}

// Claims represents the validated JWT claims including Kubernetes-specific fields.
type Claims struct {
	Namespace      string
	ServiceAccount string
	Issuer         string
	Audience       []string
	ExpiresAt      time.Time
	IssuedAt       time.Time
	NotBefore      time.Time
}

// Custom error types for different validation failures
var (
	ErrExpiredToken     = errors.New("token has expired")
	ErrInvalidSignature = errors.New("invalid token signature")
	ErrInvalidClaims    = errors.New("invalid token claims")
	ErrMissingK8sClaims = errors.New("missing kubernetes claims")
)

// NewValidatorFromURL creates a new JWT validator that fetches JWKS from an HTTP URL.
// This is the production constructor that fetches JWKS with automatic refresh.
// The keyfunc library handles caching and periodic refresh automatically.
func NewValidatorFromURL(jwksURL, issuer, audience string) (*Validator, error) {
	// Fetch JWKS from URL with automatic refresh
	// keyfunc.Get() handles:
	// - HTTP fetching
	// - Automatic refresh (default 1 hour)
	// - Caching
	// - Error handling and retries
	jwks, err := keyfunc.Get(jwksURL, keyfunc.Options{
		RefreshInterval:   time.Hour,        // Refresh keys every hour
		RefreshRateLimit:  time.Minute * 5,  // Rate limit refreshes to once per 5 minutes
		RefreshTimeout:    time.Second * 10, // Timeout for refresh requests
		RefreshUnknownKID: true,             // Refresh if we encounter an unknown key ID
	})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch JWKS from URL: %w", err)
	}

	return &Validator{
		jwks:     jwks,
		issuer:   issuer,
		audience: audience,
		timeFunc: time.Now, // Default to real time
	}, nil
}

// NewValidatorFromFile creates a new JWT validator that loads JWKS from a file.
// This is primarily for testing purposes. In production, use NewValidatorFromURL.
func NewValidatorFromFile(jwksPath, issuer, audience string) (*Validator, error) {
	// Read JWKS file
	jwksData, err := os.ReadFile(jwksPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read JWKS file: %w", err)
	}

	// Parse JWKS
	jwks, err := keyfunc.NewJSON(jwksData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse JWKS: %w", err)
	}

	return &Validator{
		jwks:     jwks,
		issuer:   issuer,
		audience: audience,
		timeFunc: time.Now, // Default to real time
	}, nil
}

// SetTimeFunc sets a custom time function for testing purposes.
func (v *Validator) SetTimeFunc(fn func() time.Time) {
	v.timeFunc = fn
}

// ValidateToken validates a JWT token and returns the extracted claims.
func (v *Validator) ValidateToken(tokenString string) (*Claims, error) {
	// Parse and validate the token with custom time function
	token, err := jwt.Parse(tokenString, v.jwks.Keyfunc, jwt.WithTimeFunc(v.timeFunc))
	if err != nil {
		// Check for specific error types
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, fmt.Errorf("%w: %v", ErrExpiredToken, err)
		}
		if errors.Is(err, jwt.ErrTokenSignatureInvalid) {
			return nil, fmt.Errorf("%w: %v", ErrInvalidSignature, err)
		}
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	if !token.Valid {
		return nil, ErrInvalidSignature
	}

	// Extract claims
	mapClaims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("failed to extract claims")
	}

	// Validate standard claims
	if err := v.validateStandardClaims(mapClaims); err != nil {
		return nil, err
	}

	// Extract and validate Kubernetes-specific claims
	claims, err := v.extractK8sClaims(mapClaims)
	if err != nil {
		return nil, err
	}

	return claims, nil
}

// validateStandardClaims validates issuer, audience, expiration, etc.
func (v *Validator) validateStandardClaims(claims jwt.MapClaims) error {
	// Validate issuer
	iss, ok := claims["iss"].(string)
	if !ok || iss != v.issuer {
		return fmt.Errorf("%w: issuer mismatch (expected %q, got %q)", ErrInvalidClaims, v.issuer, iss)
	}

	// Validate audience
	aud, ok := claims["aud"]
	if !ok {
		return fmt.Errorf("%w: missing audience", ErrInvalidClaims)
	}

	// Audience can be string or []string
	var audiences []string
	switch a := aud.(type) {
	case string:
		audiences = []string{a}
	case []interface{}:
		for _, item := range a {
			if str, ok := item.(string); ok {
				audiences = append(audiences, str)
			}
		}
	default:
		return fmt.Errorf("%w: invalid audience format", ErrInvalidClaims)
	}

	// Check if expected audience is in the list
	found := false
	for _, a := range audiences {
		if a == v.audience {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("%w: audience mismatch (expected %q)", ErrInvalidClaims, v.audience)
	}

	// Validate expiration (exp)
	exp, ok := claims["exp"].(float64)
	if !ok {
		return fmt.Errorf("%w: missing or invalid exp claim", ErrInvalidClaims)
	}
	if v.timeFunc().Unix() > int64(exp) {
		return ErrExpiredToken
	}

	// Validate not-before (nbf)
	if nbf, ok := claims["nbf"].(float64); ok {
		if v.timeFunc().Unix() < int64(nbf) {
			return fmt.Errorf("%w: token not yet valid", ErrInvalidClaims)
		}
	}

	// Validate issued-at (iat)
	if iat, ok := claims["iat"].(float64); ok {
		// Make sure issued-at is not in the future (with 1 minute tolerance)
		if v.timeFunc().Unix()+60 < int64(iat) {
			return fmt.Errorf("%w: issued-at is in the future", ErrInvalidClaims)
		}
	}

	return nil
}

// extractK8sClaims extracts Kubernetes-specific claims from the token.
func (v *Validator) extractK8sClaims(claims jwt.MapClaims) (*Claims, error) {
	// Extract kubernetes.io claim
	k8sData, ok := claims["kubernetes.io"]
	if !ok {
		return nil, fmt.Errorf("%w: kubernetes.io claim missing", ErrMissingK8sClaims)
	}

	// Convert to map
	k8sMap, ok := k8sData.(map[string]interface{})
	if !ok {
		// Try JSON marshaling/unmarshaling as fallback
		jsonData, err := json.Marshal(k8sData)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid kubernetes.io format", ErrMissingK8sClaims)
		}
		if err := json.Unmarshal(jsonData, &k8sMap); err != nil {
			return nil, fmt.Errorf("%w: invalid kubernetes.io format", ErrMissingK8sClaims)
		}
	}

	// Extract namespace
	namespace, ok := k8sMap["namespace"].(string)
	if !ok || namespace == "" {
		return nil, fmt.Errorf("%w: namespace claim missing or empty", ErrMissingK8sClaims)
	}

	// Extract service account
	saData, ok := k8sMap["serviceaccount"]
	if !ok {
		return nil, fmt.Errorf("%w: serviceaccount claim missing", ErrMissingK8sClaims)
	}

	saMap, ok := saData.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("%w: invalid serviceaccount format", ErrMissingK8sClaims)
	}

	saName, ok := saMap["name"].(string)
	if !ok || saName == "" {
		return nil, fmt.Errorf("%w: serviceaccount name missing or empty", ErrMissingK8sClaims)
	}

	// Build Claims struct
	result := &Claims{
		Namespace:      namespace,
		ServiceAccount: saName,
		Issuer:         claims["iss"].(string),
	}

	// Extract audience
	if aud, ok := claims["aud"]; ok {
		switch a := aud.(type) {
		case string:
			result.Audience = []string{a}
		case []interface{}:
			for _, item := range a {
				if str, ok := item.(string); ok {
					result.Audience = append(result.Audience, str)
				}
			}
		}
	}

	// Extract time claims
	if exp, ok := claims["exp"].(float64); ok {
		result.ExpiresAt = time.Unix(int64(exp), 0)
	}
	if iat, ok := claims["iat"].(float64); ok {
		result.IssuedAt = time.Unix(int64(iat), 0)
	}
	if nbf, ok := claims["nbf"].(float64); ok {
		result.NotBefore = time.Unix(int64(nbf), 0)
	}

	return result, nil
}

// IsExpiredError checks if the error is due to token expiration.
func IsExpiredError(err error) bool {
	return errors.Is(err, ErrExpiredToken)
}

// IsSignatureError checks if the error is due to invalid signature.
func IsSignatureError(err error) bool {
	return errors.Is(err, ErrInvalidSignature)
}

// IsClaimsError checks if the error is due to invalid claims.
func IsClaimsError(err error) bool {
	return errors.Is(err, ErrInvalidClaims)
}
