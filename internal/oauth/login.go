package oauth

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"time"
)

// LoginOptions controls the login flow.
type LoginOptions struct {
	OpenBrowser bool
	Stdout      io.Writer
	Timeout     time.Duration
}

// Login runs the full browser-based OAuth Authorization Code + PKCE flow
// against ChatGPT (auth.openai.com), writes tokens to the given path,
// and returns the saved AuthJSON.
func Login(ctx context.Context, path string, opts LoginOptions) (*AuthJSON, error) {
	if opts.Timeout == 0 {
		opts.Timeout = 5 * time.Minute
	}
	if opts.Stdout == nil {
		opts.Stdout = io.Discard
	}

	pkce, err := GeneratePKCE()
	if err != nil {
		return nil, fmt.Errorf("pkce: %w", err)
	}
	state, err := GenerateState()
	if err != nil {
		return nil, fmt.Errorf("state: %w", err)
	}

	authURL := BuildAuthURL(pkce, state)

	fmt.Fprintln(opts.Stdout, "altcode login")
	fmt.Fprintln(opts.Stdout)
	fmt.Fprintln(opts.Stdout, "Open this URL in a browser if one does not open automatically:")
	fmt.Fprintln(opts.Stdout, "  "+authURL)
	fmt.Fprintln(opts.Stdout)
	fmt.Fprintln(opts.Stdout, "Waiting for login...")

	if opts.OpenBrowser {
		_ = openBrowser(authURL)
	}

	cb, err := RunCallbackServer(ctx, state, opts.Timeout)
	if err != nil {
		return nil, err
	}

	td, err := ExchangeCode(ctx, cb.Code, pkce.Verifier)
	if err != nil {
		return nil, fmt.Errorf("token exchange: %w", err)
	}

	now := time.Now().UTC()
	auth := &AuthJSON{
		AuthMode:    "Chatgpt",
		Tokens:      td,
		LastRefresh: &now,
	}
	if err := Save(path, auth); err != nil {
		return nil, fmt.Errorf("save: %w", err)
	}

	fmt.Fprintln(opts.Stdout, "Login successful. Credentials saved to "+path)
	return auth, nil
}

// Logout deletes the stored auth file.
func Logout(path string) error {
	return removeIfExists(path)
}

func openBrowser(u string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	return cmd.Start()
}
