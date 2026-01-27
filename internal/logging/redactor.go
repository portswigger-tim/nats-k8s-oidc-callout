// Package logging provides utility functions for secure logging,
// including redaction of sensitive information such as passwords and tokens.
package logging

import (
	"strings"
)

// RedactNATSURL redacts passwords from NATS connection URLs
func RedactNATSURL(url string) string {
	if url == "" {
		return ""
	}

	// Pattern: nats://user:password@host:port
	// Find the last @ in the URL (to handle @ in password)
	lastAt := strings.LastIndex(url, "@")
	if lastAt == -1 {
		return url
	}

	// Find the : before the password
	colonPos := strings.Index(url, "://")
	if colonPos == -1 {
		return url
	}
	colonPos += 3 // Skip ://

	// Find the : after username
	userColonPos := strings.Index(url[colonPos:], ":")
	if userColonPos == -1 {
		return url
	}
	userColonPos += colonPos

	// Rebuild URL with redacted password
	return url[:userColonPos+1] + "***" + url[lastAt:]
}

// RedactJWT redacts JWT tokens by showing only first and last 8 characters
func RedactJWT(token string) string {
	if len(token) < 16 {
		return token
	}

	first := token[:8]
	last := token[len(token)-8:]
	return first + "..." + last + " (redacted)"
}

// RedactSensitiveFields redacts sensitive fields in a map
func RedactSensitiveFields(data map[string]interface{}) map[string]interface{} {
	if data == nil {
		return nil
	}

	// List of sensitive field names to redact
	sensitiveFields := []string{
		"password",
		"passwd",
		"pwd",
		"secret",
		"token",
		"jwt",
		"authorization",
		"auth",
		"api_key",
		"apikey",
	}

	result := make(map[string]interface{})
	for key, value := range data {
		keyLower := strings.ToLower(key)
		isSensitive := false

		for _, sensitiveField := range sensitiveFields {
			if strings.Contains(keyLower, sensitiveField) {
				isSensitive = true
				break
			}
		}

		if isSensitive {
			result[key] = "***"
		} else {
			result[key] = value
		}
	}

	return result
}
