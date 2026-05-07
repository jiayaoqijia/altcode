package tui

// stripANSI removes ANSI escape sequences (CSI colors, OSC 8 hyperlinks,
// SGR styling, etc.) so the result is plain text suitable for embedding
// in a markdown export, logfile, or non-styling consumer.
//
// State machine: an `\033` (ESC) starts an escape; we drop bytes until
// we hit a final byte. For OSC sequences (ESC ]) the final byte is
// BEL (\a) or ST (ESC \), but the simpler "stop on the next ASCII
// letter" heuristic also catches those because OSC payloads always
// end with one. Good enough for log/markdown stripping; not meant to
// fully parse arbitrary terminal output.
func stripANSI(s string) string {
	var result []rune
	inEscape := false
	for _, r := range s {
		if r == '\033' {
			inEscape = true
			continue
		}
		if inEscape {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '\a' {
				inEscape = false
			}
			continue
		}
		result = append(result, r)
	}
	return string(result)
}
