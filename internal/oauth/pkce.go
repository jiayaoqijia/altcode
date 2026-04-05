// Package oauth implements OAuth 2.0 Authorization Code + PKCE flow
// compatible with OpenAI ChatGPT subscription login, mirroring Codex CLI.
package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// PKCECodes holds the verifier/challenge pair for RFC 7636 PKCE.
type PKCECodes struct {
	Verifier  string
	Challenge string // SHA256(verifier), base64url no-padding
}

// GeneratePKCE creates a new PKCE code pair.
// 64 random bytes → base64url verifier, SHA256 → challenge.
func GeneratePKCE() (*PKCECodes, error) {
	buf := make([]byte, 64)
	if _, err := rand.Read(buf); err != nil {
		return nil, fmt.Errorf("read random: %w", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(buf)
	digest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(digest[:])
	return &PKCECodes{Verifier: verifier, Challenge: challenge}, nil
}

// GenerateState returns a random CSRF state token (32 bytes → base64url).
func GenerateState() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("read random: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
