package oauth

// OAuth constants mirroring Codex CLI's ChatGPT login flow.
// See vendor/codex/codex-rs/login/src/server.rs.
const (
	DefaultIssuer    = "https://auth.openai.com"
	DefaultPort      = 1455
	DefaultClientID  = "app_EMoamEEZ73f0CkXaXp7hrann"
	DefaultRedirect  = "http://localhost:1455/auth/callback"
	DefaultScope     = "openid profile email offline_access api.connectors.read api.connectors.invoke"
	DefaultTokenPath = "/oauth/token"
	DefaultAuthPath  = "/oauth/authorize"
)
