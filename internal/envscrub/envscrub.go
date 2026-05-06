// Package envscrub strips credential-bearing and CLI-internal env vars
// before handing an environment to a child process spawned by altcode.
//
// Three call sites use this:
//   - internal/hooks/exec.go : sh -c hooks
//   - internal/tool/bash.go  : bash -c tool invocations
//   - internal/mcp/client.go : MCP stdio servers
//
// Codex round-R adversarial review: bash + MCP previously inherited
// the full parent env (including OTEL_*, CLAUDE_*, ALTCODE_*, and
// any provider API keys); claude-code 2.1.128 fixed the same class
// for OTEL_*. Centralising the scrub list here keeps the three call
// sites consistent and easy to audit.
package envscrub

import "strings"

// substrings are case-insensitive substrings of the variable NAME
// (uppercased) that mark a credential-bearing entry. A single match
// drops the whole `KEY=VALUE` pair.
var substrings = []string{
	"API_KEY", "APIKEY",
	"SECRET", "TOKEN", "PASSWORD", "PASSWD",
	"PRIVATE_KEY", "PRIVATEKEY",
	"SESSION", "AUTH",
	"CREDENTIAL", "CREDS",
	"ACCESS_KEY", "ACCESSKEY",
}

// prefixes are uppercased name prefixes that mark CLI-internal
// telemetry/config not safe to leak to children. OTEL_* prevents an
// instrumented child from picking up our parent OTLP endpoint;
// CLAUDE_*/ALTCODE_*/ANTHROPIC_LOG block internal session config.
var prefixes = []string{
	"OTEL_",
	"CLAUDE_",
	"ALTCODE_",
	"ANTHROPIC_LOG",
	"ANTHROPIC_DEBUG",
}

// Scrub returns env with credential/internal entries removed.
// The order of remaining entries is preserved.
func Scrub(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			out = append(out, kv)
			continue
		}
		name := strings.ToUpper(kv[:eq])
		if !shouldDrop(name) {
			out = append(out, kv)
		}
	}
	return out
}

func shouldDrop(name string) bool {
	for _, s := range substrings {
		if strings.Contains(name, s) {
			return true
		}
	}
	for _, p := range prefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}
