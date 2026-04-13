package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Clock skew grace period for share link expiry checks.
const shareClockSkew = 60 * time.Second

// Errors returned by VerifyShareURL.
var (
	ErrShareExpired     = errors.New("share link expired")
	ErrShareInvalidHMAC = errors.New("invalid share signature")
)

// secretPatterns are compiled regexes used by RedactSecrets
// to strip credential-like strings from event data.
var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(
		`(?i)(api[_-]?key|token|secret|password|auth)\s*[:=]\s*\S+`,
	),
	regexp.MustCompile(`sk-[a-zA-Z0-9]{20,}`),
	regexp.MustCompile(`ghp_[a-zA-Z0-9]{36}`),
	regexp.MustCompile(`-----BEGIN [A-Z ]+ KEY-----`),
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
}

// SignShareURL produces an HMAC-SHA256 signature over
// taskID and expiry, using a NUL byte as separator to
// prevent concatenation ambiguity.
func SignShareURL(
	taskID string, expiry int64, secret []byte,
) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(taskID))
	mac.Write([]byte{0x00})
	mac.Write([]byte(strconv.FormatInt(expiry, 10)))
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyShareURL recomputes the HMAC and compares it with
// the provided token using constant-time comparison. It
// also checks expiry, allowing shareClockSkew grace.
func VerifyShareURL(
	taskID, token string, expiry int64, secret []byte,
) error {
	now := time.Now().Unix()
	if now > expiry+int64(shareClockSkew.Seconds()) {
		return ErrShareExpired
	}
	expected := SignShareURL(taskID, expiry, secret)
	if subtle.ConstantTimeCompare(
		[]byte(token), []byte(expected),
	) != 1 {
		return ErrShareInvalidHMAC
	}
	return nil
}

// RedactSecrets replaces known secret patterns in data
// with "[REDACTED]".
func RedactSecrets(data string) string {
	for _, pat := range secretPatterns {
		data = pat.ReplaceAllString(data, "[REDACTED]")
	}
	return data
}

// GenerateShareLink creates a share URL path of the form
// /share/{taskID}.{hmac}?exp={expiry}.
func GenerateShareLink(
	taskID string, secret []byte, ttl time.Duration,
) string {
	expiry := time.Now().Add(ttl).Unix()
	sig := SignShareURL(taskID, expiry, secret)
	return fmt.Sprintf(
		"/share/%s.%s?exp=%d", taskID, sig, expiry,
	)
}

// shareContentData carries fields for share.html rendering.
type shareContentData struct {
	Task      *TaskView
	PhaseData phaseBarData
	Events    []*EventView
}

// HandleShareView serves the public read-only share page.
// The path format is /share/{taskID}.{hmac}?exp={unix}.
func (h *WebHandler) HandleShareView(
	w http.ResponseWriter, r *http.Request,
) {
	// If no signing key is configured, shared URLs are disabled —
	// an empty key would let any HMAC pass validation.
	if len(h.cfg.SigningKey) == 0 {
		http.NotFound(w, r)
		return
	}

	raw := r.PathValue("token")
	dot := strings.LastIndex(raw, ".")
	if dot < 0 {
		http.Error(w, "invalid share link format", http.StatusBadRequest)
		return
	}
	taskID := raw[:dot]
	sig := raw[dot+1:]

	expStr := r.URL.Query().Get("exp")
	if expStr == "" {
		http.Error(w, "missing expiry", http.StatusBadRequest)
		return
	}
	expiry, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid expiry", http.StatusBadRequest)
		return
	}

	if err := VerifyShareURL(
		taskID, sig, expiry, h.cfg.SigningKey,
	); err != nil {
		if errors.Is(err, ErrShareExpired) {
			http.Error(w, "share link expired", http.StatusForbidden)
			return
		}
		http.Error(w, "invalid share link", http.StatusForbidden)
		return
	}

	es := h.eventStore()
	if es == nil {
		http.Error(
			w, "store not configured",
			http.StatusInternalServerError,
		)
		return
	}

	task, err := es.GetTask(taskID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	events, _ := es.ListEvents(taskID, 0)
	for _, ev := range events {
		ev.Data = RedactSecrets(ev.Data)
	}

	content := shareContentData{
		Task: task,
		PhaseData: phaseBarData{
			Phases:       defaultPhases,
			CurrentPhase: task.Status,
		},
		Events: events,
	}

	data := PageData{
		Title:   "Shared Task",
		ShowNav: false,
		Content: content,
	}

	if err := Render(w, h.tmpl, "share", data); err != nil {
		http.Error(
			w, "internal error",
			http.StatusInternalServerError,
		)
	}
}
