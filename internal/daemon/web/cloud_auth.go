package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// TicketClaims holds the JWT payload from a Cloud Claw session
// ticket.
type TicketClaims struct {
	Issuer       string `json:"iss"`
	Subject      string `json:"sub"`
	PortalUserID string `json:"portal_user_id"`
	VMName       string `json:"vm_name"`
	JTI          string `json:"jti"`
	Exp          int64  `json:"exp"`
}

// verifySessionTicket parses a JWT (HMAC-SHA256), verifies the
// signature with key, and checks expiry. No external JWT library
// is used -- this is stdlib-only.
func verifySessionTicket(
	tokenStr string, key []byte,
) (*TicketClaims, error) {
	parts := strings.SplitN(tokenStr, ".", 3)
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid token format")
	}

	// Verify HMAC-SHA256 signature.
	signingInput := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(signingInput))
	expectedSig := base64.RawURLEncoding.EncodeToString(
		mac.Sum(nil),
	)
	if subtle.ConstantTimeCompare(
		[]byte(parts[2]), []byte(expectedSig),
	) != 1 {
		return nil, fmt.Errorf("invalid signature")
	}

	// Decode claims.
	claimsJSON, err := base64.RawURLEncoding.DecodeString(
		parts[1],
	)
	if err != nil {
		return nil, fmt.Errorf("decode claims: %w", err)
	}
	var claims TicketClaims
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		return nil, fmt.Errorf("unmarshal claims: %w", err)
	}

	// Check expiry.
	if time.Now().Unix() > claims.Exp {
		return nil, fmt.Errorf("ticket expired")
	}

	return &claims, nil
}

// HandleSessionTicket implements GET /auth/session?ticket={jwt}
// for Cloud Claw proxy authentication.
func (h *WebHandler) HandleSessionTicket(
	w http.ResponseWriter, r *http.Request,
) {
	if !h.cfg.CloudMode {
		http.Error(
			w, "cloud mode not enabled", http.StatusNotFound,
		)
		return
	}

	ticket := r.URL.Query().Get("ticket")
	if ticket == "" {
		http.Error(w, "missing ticket", http.StatusBadRequest)
		return
	}

	claims, err := verifySessionTicket(
		ticket, h.cfg.SessionSigningKey,
	)
	if err != nil {
		http.Error(
			w, "invalid ticket", http.StatusUnauthorized,
		)
		return
	}

	// Validate claims.
	if claims.Issuer != "cloud-claw" ||
		claims.Subject != "altcode-ui" {
		http.Error(
			w, "invalid ticket claims",
			http.StatusUnauthorized,
		)
		return
	}

	// Atomically claim the JTI for replay prevention.
	exp := time.Unix(claims.Exp, 0)
	if !h.sessions.TryUseJTI(claims.JTI, exp) {
		http.Error(
			w, "ticket already used",
			http.StatusUnauthorized,
		)
		return
	}

	// Create authenticated session.
	sid := h.sessions.Create(&SessionUser{
		Login:   claims.PortalUserID,
		IsAdmin: false,
	})
	h.sessions.SetAuthenticated(sid)

	// Set cookie with BasePath-scoped path.
	http.SetCookie(w, &http.Cookie{
		Name:     "altfix_session",
		Value:    sid,
		Path:     h.cookiePath(),
		MaxAge:   28800, // 8 hours
		HttpOnly: true,
		Secure:   isSecure(r, h.cfg.TrustProxy),
		SameSite: http.SameSiteLaxMode,
	})

	http.Redirect(
		w, r, h.cfg.BasePath+"/ui/", http.StatusFound,
	)
}
