package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// tokenResponse matches OpenAI's /oauth/token response.
type tokenResponse struct {
	IDToken      string `json:"id_token"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

// ExchangeCode swaps an authorization code for tokens using PKCE.
func ExchangeCode(ctx context.Context, code, verifier string) (*TokenData, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", DefaultRedirect)
	form.Set("client_id", DefaultClientID)
	form.Set("code_verifier", verifier)
	return postToken(ctx, form)
}

// RefreshToken trades a refresh_token for a fresh access_token.
func RefreshToken(ctx context.Context, refreshToken string) (*TokenData, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("client_id", DefaultClientID)
	form.Set("refresh_token", refreshToken)
	td, err := postToken(ctx, form)
	if err != nil {
		return nil, err
	}
	// Refresh may not rotate the refresh token; preserve it if absent.
	if td.RefreshToken == "" {
		td.RefreshToken = refreshToken
	}
	return td, nil
}

// issuerOverride lets tests point the token endpoint at a mock server.
var issuerOverride = ""

func postToken(ctx context.Context, form url.Values) (*TokenData, error) {
	issuer := DefaultIssuer
	if issuerOverride != "" {
		issuer = issuerOverride
	}
	endpoint := issuer + DefaultTokenPath

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint,
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("token endpoint %d: %s", resp.StatusCode, string(body))
	}

	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return &TokenData{
		IDToken:      tr.IDToken,
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
	}, nil
}
