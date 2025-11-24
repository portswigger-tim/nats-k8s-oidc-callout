package jwt

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewValidatorFromFile_LoadsJWKS(t *testing.T) {
	// Test loading JWKS from file (for testing)
	jwksPath := filepath.Join("..", "..", "testdata", "jwks.json")

	validator, err := NewValidatorFromFile(jwksPath, "https://test-issuer.com", "test-audience")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if validator == nil {
		t.Fatal("expected validator to be created")
	}
}

func TestNewValidatorFromURL_FetchesJWKS(t *testing.T) {
	// RED: Test loading JWKS from HTTP URL (for production)
	// This test would require a mock HTTP server or will be skipped for now
	// In production, this will fetch from https://kubernetes.default.svc/openid/v1/jwks
	t.Skip("Requires mock HTTP server - will implement when needed")
}

func TestNewValidatorFromFile_FailsWithInvalidPath(t *testing.T) {
	// Test for error handling with invalid JWKS file
	validator, err := NewValidatorFromFile("/nonexistent/path/jwks.json", "https://test-issuer.com", "test-audience")

	if err == nil {
		t.Fatal("expected error for invalid JWKS path, got nil")
	}

	if validator != nil {
		t.Fatal("expected nil validator on error")
	}
}

func TestValidateToken_ValidToken(t *testing.T) {
	// RED: Test signature validation with our real token
	jwksPath := filepath.Join("..", "..", "testdata", "jwks.json")
	tokenPath := filepath.Join("..", "..", "testdata", "token.jwt")

	// Read the token
	tokenBytes, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("failed to read test token: %v", err)
	}
	tokenString := string(tokenBytes)

	// Note: The real token has a different issuer and audience
	// We'll need to use the actual values from the token
	validator, err := NewValidatorFromFile(
		jwksPath,
		"https://oidc.eks.eu-west-1.amazonaws.com/id/B88E7287E54DB073AC9CDC2FD1BE0969",
		"sts.amazonaws.com",
	)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	claims, err := validator.ValidateToken(tokenString)
	if err != nil {
		t.Fatalf("expected valid token, got error: %v", err)
	}

	if claims == nil {
		t.Fatal("expected claims to be returned")
	}

	// Verify K8s-specific claims
	if claims.Namespace != "hakawai" {
		t.Errorf("expected namespace 'hakawai', got %q", claims.Namespace)
	}

	if claims.ServiceAccount != "hakawai-litellm-proxy" {
		t.Errorf("expected service account 'hakawai-litellm-proxy', got %q", claims.ServiceAccount)
	}
}

func TestValidateToken_ExpiredToken(t *testing.T) {
	// RED: Test expired token by mocking time to be after token expiration
	jwksPath := filepath.Join("..", "..", "testdata", "jwks.json")
	tokenPath := filepath.Join("..", "..", "testdata", "token.jwt")

	tokenBytes, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("failed to read test token: %v", err)
	}
	tokenString := string(tokenBytes)

	validator, err := NewValidatorFromFile(
		jwksPath,
		"https://oidc.eks.eu-west-1.amazonaws.com/id/B88E7287E54DB073AC9CDC2FD1BE0969",
		"sts.amazonaws.com",
	)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	// Mock time to be after the token expires (token expires at 1764056278 = 2034-11-24)
	// Set time to 2035-01-01
	futureTime := time.Unix(1767225600, 0) // 2035-01-01
	validator.SetTimeFunc(func() time.Time {
		return futureTime
	})

	_, err = validator.ValidateToken(tokenString)
	if err == nil {
		t.Fatal("expected error for expired token, got nil")
	}

	if !IsExpiredError(err) {
		t.Errorf("expected expired error, got %v", err)
	}
}

func TestValidateToken_InvalidSignature(t *testing.T) {
	// Test for invalid signature detection
	jwksPath := filepath.Join("..", "..", "testdata", "jwks.json")

	validator, err := NewValidatorFromFile(jwksPath, "https://test-issuer.com", "test-audience")
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	// Token with valid structure but invalid signature
	invalidToken := "eyJhbGciOiJSUzI1NiIsImtpZCI6ImUzYjFkMTg1ZTBkNzk0MDU4YTYzNDZjMzJiMjU3NWFjMGVmYjYyMmUiLCJ0eXAiOiJKV1QifQ.eyJpc3MiOiJodHRwczovL3Rlc3QtaXNzdWVyLmNvbSIsImF1ZCI6InRlc3QtYXVkaWVuY2UiLCJleHAiOjk5OTk5OTk5OTksImlhdCI6MTUxNjIzOTAyMiwia3ViZXJuZXRlcy5pbyI6eyJuYW1lc3BhY2UiOiJ0ZXN0Iiwic2VydmljZWFjY291bnQiOnsibmFtZSI6InRlc3Qtc2EifX19.invalidsignature"

	_, err = validator.ValidateToken(invalidToken)
	if err == nil {
		t.Fatal("expected error for invalid signature, got nil")
	}

	if !IsSignatureError(err) {
		t.Errorf("expected signature error, got %v", err)
	}
}

func TestValidateToken_WrongIssuer(t *testing.T) {
	// Test for issuer validation
	jwksPath := filepath.Join("..", "..", "testdata", "jwks.json")
	tokenPath := filepath.Join("..", "..", "testdata", "token.jwt")

	tokenBytes, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("failed to read test token: %v", err)
	}

	// Create validator with wrong issuer
	validator, err := NewValidatorFromFile(jwksPath, "https://wrong-issuer.com", "sts.amazonaws.com")
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	_, err = validator.ValidateToken(string(tokenBytes))
	if err == nil {
		t.Fatal("expected error for wrong issuer, got nil")
	}

	if !IsClaimsError(err) {
		t.Errorf("expected claims validation error, got %v", err)
	}
}

func TestValidateToken_MissingK8sClaims(t *testing.T) {
	// RED: Test for missing Kubernetes-specific claims
	// This would need a token without kubernetes.io claims
	// For now, we'll skip this and implement it later with a mock token
	t.Skip("Need to create test token without K8s claims")
}
