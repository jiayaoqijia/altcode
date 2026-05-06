package provider

import (
	"bufio"
	"io"
	"strings"
)

// SSEDecoder reads Server-Sent Events from a reader.
type SSEDecoder struct {
	scanner *bufio.Scanner
}

// NewSSEDecoder creates a new SSEDecoder reading from r.
//
// The default bufio.Scanner buffer is 64 KiB, which silently aborted
// any stream containing a single SSE line larger than that — common
// for Anthropic input_json_delta blobs (large tool inputs, base64
// images, verbose error bodies). Bumped to 16 MiB so realistic
// payloads don't kill the stream mid-turn with bufio.ErrTooLong.
func NewSSEDecoder(r io.Reader) *SSEDecoder {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 64*1024), 16*1024*1024)
	return &SSEDecoder{scanner: s}
}

// Next returns the next SSE event type and data payload.
// Returns io.EOF when the stream is exhausted.
//
// Per the SSE spec, multiple "data:" lines in a single event are
// concatenated with newlines. The previous implementation overwrote
// evtData on each "data:" line, so multi-line payloads (some provider
// error bodies, multi-line tool result text) lost everything but the
// last line.
func (d *SSEDecoder) Next() (eventType string, data string, err error) {
	var evtType string
	var evtData strings.Builder
	for d.scanner.Scan() {
		// Trim a trailing \r so CRLF-encoded SSE streams (the
		// spec-compliant line ending — used by most real servers) parse
		// correctly. Without this, a separator line arrives as "\r"
		// instead of "" and events get buffered until EOF; data: lines
		// keep a trailing \r that corrupts the downstream JSON parse.
		// Found by Codex round-J adversarial review.
		line := strings.TrimSuffix(d.scanner.Text(), "\r")
		if line == "" {
			if evtType != "" || evtData.Len() > 0 {
				return evtType, strings.TrimRight(evtData.String(), "\n"), nil
			}
			continue
		}
		// SSE spec allows both "event: value" and "event:value"
		if after, ok := strings.CutPrefix(line, "event:"); ok {
			evtType = strings.TrimLeft(after, " ")
			continue
		}
		if after, ok := strings.CutPrefix(line, "data:"); ok {
			evtData.WriteString(strings.TrimLeft(after, " "))
			evtData.WriteByte('\n')
			continue
		}
	}
	if scanErr := d.scanner.Err(); scanErr != nil {
		return "", "", scanErr
	}
	if evtType != "" || evtData.Len() > 0 {
		return evtType, strings.TrimRight(evtData.String(), "\n"), nil
	}
	return "", "", io.EOF
}
