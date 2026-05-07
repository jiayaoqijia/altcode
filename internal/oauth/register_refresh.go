package oauth

import (
	"context"
	"time"

	"github.com/jiayaoqijia/altcode/internal/auth"
)

// init registers a refresh function with the auth package so
// loadAltcodeAuth can rotate expired ChatGPT access tokens without
// the auth package importing oauth (keeps auth a leaf dependency).
func init() {
	auth.RegisterRefresh(refreshAndPersist)
}

// refreshAndPersist trades the stored refresh_token for a fresh access
// token and writes the updated AuthJSON back to `path`. Returns the
// new access token, or "" on any failure (network error, invalid
// grant, disk write failure) — caller falls through to the cached
// (possibly expired) token so behaviour is never worse than a miss.
func refreshAndPersist(path, refreshToken string) string {
	ctx, cancel := context.WithTimeout(
		context.Background(), 15*time.Second,
	)
	defer cancel()

	td, err := RefreshToken(ctx, refreshToken)
	if err != nil || td == nil || td.AccessToken == "" {
		return ""
	}

	// Load existing file to preserve unrelated fields (auth_mode,
	// OpenAIKey) and overwrite tokens + last_refresh only.
	existing, err := Load(path)
	if err != nil || existing == nil {
		existing = &AuthJSON{}
	}
	existing.Tokens = td
	now := time.Now().UTC()
	existing.LastRefresh = &now

	if saveErr := Save(path, existing); saveErr != nil {
		// Even if we can't persist, we still have a valid token for
		// this session — return it so the provider call succeeds.
		return td.AccessToken
	}
	return td.AccessToken
}
