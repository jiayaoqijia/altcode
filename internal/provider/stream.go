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
func NewSSEDecoder(r io.Reader) *SSEDecoder {
	return &SSEDecoder{scanner: bufio.NewScanner(r)}
}

// Next returns the next SSE event type and data payload.
// Returns io.EOF when the stream is exhausted.
func (d *SSEDecoder) Next() (eventType string, data string, err error) {
	var evtType, evtData string
	for d.scanner.Scan() {
		line := d.scanner.Text()
		if line == "" {
			if evtType != "" || evtData != "" {
				return evtType, evtData, nil
			}
			continue
		}
		// SSE spec allows both "event: value" and "event:value"
		if after, ok := strings.CutPrefix(line, "event:"); ok {
			evtType = strings.TrimLeft(after, " ")
			continue
		}
		if after, ok := strings.CutPrefix(line, "data:"); ok {
			evtData = strings.TrimLeft(after, " ")
			continue
		}
	}
	if scanErr := d.scanner.Err(); scanErr != nil {
		return "", "", scanErr
	}
	if evtType != "" || evtData != "" {
		return evtType, evtData, nil
	}
	return "", "", io.EOF
}
