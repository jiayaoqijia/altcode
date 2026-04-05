package oauth

import (
	"net/url"
	"strings"
	"testing"
)

func TestBuildAuthURL_ContainsRequiredParams(t *testing.T) {
	pkce := &PKCECodes{Verifier: "v", Challenge: "c"}
	raw := BuildAuthURL(pkce, "state123")
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}

	if u.Host != "auth.openai.com" {
		t.Errorf("host = %q, want auth.openai.com", u.Host)
	}
	if u.Path != "/oauth/authorize" {
		t.Errorf("path = %q", u.Path)
	}

	q := u.Query()
	wants := map[string]string{
		"response_type":             "code",
		"client_id":                 DefaultClientID,
		"redirect_uri":              DefaultRedirect,
		"code_challenge":            "c",
		"code_challenge_method":     "S256",
		"state":                     "state123",
		"originator":                "altcode_cli",
		"id_token_add_organizations": "true",
	}
	for k, v := range wants {
		if got := q.Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if !strings.Contains(q.Get("scope"), "openid") {
		t.Errorf("scope missing openid: %q", q.Get("scope"))
	}
}
