package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"testing"
)

func TestGeneratePKCE_Length(t *testing.T) {
	p, err := GeneratePKCE()
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Verifier) < 80 || len(p.Verifier) > 90 {
		t.Errorf("verifier length = %d, want ~86", len(p.Verifier))
	}
	if len(p.Challenge) != 43 {
		t.Errorf("challenge length = %d, want 43", len(p.Challenge))
	}
}

func TestGeneratePKCE_ChallengeIsS256OfVerifier(t *testing.T) {
	p, err := GeneratePKCE()
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(p.Verifier))
	want := base64.RawURLEncoding.EncodeToString(digest[:])
	if p.Challenge != want {
		t.Errorf("challenge mismatch")
	}
}

func TestGeneratePKCE_Unique(t *testing.T) {
	a, _ := GeneratePKCE()
	b, _ := GeneratePKCE()
	if a.Verifier == b.Verifier {
		t.Error("verifiers should be unique")
	}
}

func TestGenerateState_Length(t *testing.T) {
	s, err := GenerateState()
	if err != nil {
		t.Fatal(err)
	}
	if len(s) != 43 {
		t.Errorf("state length = %d, want 43", len(s))
	}
}
