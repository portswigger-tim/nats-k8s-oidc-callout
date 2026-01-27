package logging

import (
	"testing"
)

func TestRedactNATSURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "URL with password",
			input:    "nats://user:password@localhost:4222",
			expected: "nats://user:***@localhost:4222",
		},
		{
			name:     "URL without password",
			input:    "nats://localhost:4222",
			expected: "nats://localhost:4222",
		},
		{
			name:     "URL with special chars in password",
			input:    "nats://user:p@ss!w0rd@localhost:4222",
			expected: "nats://user:***@localhost:4222",
		},
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RedactNATSURL(tt.input)
			if result != tt.expected {
				t.Errorf("RedactNATSURL(%q) = %q; want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestRedactJWT(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Valid JWT token",
			input:    "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c",
			expected: "eyJhbGci...adQssw5c (redacted)",
		},
		{
			name:     "Short token (less than 16 chars)",
			input:    "shorttoken",
			expected: "shorttoken",
		},
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "16 character token (boundary case)",
			input:    "1234567890123456",
			expected: "12345678...90123456 (redacted)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RedactJWT(tt.input)
			if result != tt.expected {
				t.Errorf("RedactJWT(%q) = %q; want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestRedactSensitiveFields(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]interface{}
		expected map[string]interface{}
	}{
		{
			name: "Redact password field",
			input: map[string]interface{}{
				"username": "user1",
				"password": "secret123",
				"email":    "user@example.com",
			},
			expected: map[string]interface{}{
				"username": "user1",
				"password": "***",
				"email":    "user@example.com",
			},
		},
		{
			name: "Redact JWT token field",
			input: map[string]interface{}{
				"user":  "john",
				"token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.payload.signature",
			},
			expected: map[string]interface{}{
				"user":  "john",
				"token": "***",
			},
		},
		{
			name: "No sensitive fields",
			input: map[string]interface{}{
				"name": "John Doe",
				"age":  30,
			},
			expected: map[string]interface{}{
				"name": "John Doe",
				"age":  30,
			},
		},
		{
			name:     "Empty map",
			input:    map[string]interface{}{},
			expected: map[string]interface{}{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RedactSensitiveFields(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("RedactSensitiveFields() returned map of length %d; want %d", len(result), len(tt.expected))
			}
			for key, expectedValue := range tt.expected {
				if result[key] != expectedValue {
					t.Errorf("RedactSensitiveFields()[%q] = %v; want %v", key, result[key], expectedValue)
				}
			}
		})
	}
}
