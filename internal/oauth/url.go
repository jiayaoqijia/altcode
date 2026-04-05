package oauth

import (
	"net/url"
)

// BuildAuthURL returns the authorization URL for the ChatGPT OAuth flow.
// Mirrors Codex CLI's parameter set exactly.
func BuildAuthURL(pkce *PKCECodes, state string) string {
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", DefaultClientID)
	q.Set("redirect_uri", DefaultRedirect)
	q.Set("scope", DefaultScope)
	q.Set("code_challenge", pkce.Challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("id_token_add_organizations", "true")
	q.Set("codex_cli_simplified_flow", "true")
	q.Set("state", state)
	q.Set("originator", "altcode_cli")
	return DefaultIssuer + DefaultAuthPath + "?" + q.Encode()
}
