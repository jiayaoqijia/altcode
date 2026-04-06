package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// DeviceCode holds the user code and verification URL for device code flow.
type DeviceCode struct {
	VerificationURL string
	UserCode        string
	deviceAuthID    string
	interval        int
}

// RequestDeviceCode initiates the device code flow.
// Returns a code the user must enter at the verification URL.
func RequestDeviceCode(ctx context.Context) (*DeviceCode, error) {
	endpoint := DefaultIssuer + "/api/accounts/deviceauth/usercode"
	if issuerOverride != "" {
		endpoint = issuerOverride + "/api/accounts/deviceauth/usercode"
	}

	body := fmt.Sprintf(`{"client_id":"%s"}`, DefaultClientID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint,
		strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request user code: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("device code login not enabled on this server")
	}
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("device code request failed (%d): %s", resp.StatusCode, string(b))
	}

	var result struct {
		DeviceAuthID string          `json:"device_auth_id"`
		UserCode     string          `json:"user_code"`
		Interval     json.RawMessage `json:"interval"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	issuer := DefaultIssuer
	if issuerOverride != "" {
		issuer = issuerOverride
	}

	// Interval may be string or int depending on server version
	interval := 5
	if len(result.Interval) > 0 {
		s := strings.Trim(string(result.Interval), `"`)
		if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && n > 0 {
			interval = n
		}
	}

	return &DeviceCode{
		VerificationURL: issuer + "/codex/device",
		UserCode:        result.UserCode,
		deviceAuthID:    result.DeviceAuthID,
		interval:        interval,
	}, nil
}

// PollForToken polls until the user authorizes the device code or timeout.
// Returns the tokens on success.
func (dc *DeviceCode) PollForToken(ctx context.Context, timeout time.Duration) (*TokenData, error) {
	endpoint := DefaultIssuer + "/api/accounts/deviceauth/token"
	if issuerOverride != "" {
		endpoint = issuerOverride + "/api/accounts/deviceauth/token"
	}

	if dc.interval <= 0 {
		dc.interval = 5
	}
	if timeout <= 0 {
		timeout = 15 * time.Minute
	}

	deadline := time.After(timeout)
	client := &http.Client{Timeout: 15 * time.Second}

	for {
		select {
		case <-deadline:
			return nil, fmt.Errorf("device code login timed out after %s", timeout)
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(dc.interval) * time.Second):
		}

		body := fmt.Sprintf(`{"device_auth_id":"%s","user_code":"%s"}`,
			dc.deviceAuthID, dc.UserCode)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint,
			strings.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			continue // network error, retry
		}

		if resp.StatusCode == http.StatusOK {
			var result struct {
				AuthorizationCode string `json:"authorization_code"`
				CodeChallenge     string `json:"code_challenge"`
				CodeVerifier      string `json:"code_verifier"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				resp.Body.Close()
				return nil, fmt.Errorf("parse token response: %w", err)
			}
			resp.Body.Close()

			// Exchange the authorization code for tokens
			return ExchangeCode(ctx, result.AuthorizationCode, result.CodeVerifier)
		}

		resp.Body.Close()

		// 403/404 = not yet authorized, keep polling
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
			continue
		}

		return nil, fmt.Errorf("device auth failed with status %d", resp.StatusCode)
	}
}
