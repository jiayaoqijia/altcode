package tui

import (
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

// OSC 8 (hyperlink) — supported by VS Code terminal, kitty, iTerm2,
// gnome-terminal, WezTerm, foot, Alacritty (≥0.11), and others.
// Format: ESC ] 8 ; <params> ; <uri> ESC \ <text> ESC ] 8 ; ; ESC \
//
// Terminals that don't understand the sequence ignore it and render
// only the inner text — safe to emit unconditionally. The constants
// here use the BEL terminator (\a) variant which has the widest
// compatibility (the ST terminator \033\\ trips some old terminals).
const (
	osc8Open      = "\x1b]8;;"
	osc8AfterURI  = "\x07"
	osc8Close     = "\x1b]8;;\x07"
)

// osc8Hyperlink wraps text with an OSC-8 hyperlink to uri. If uri is
// empty the text passes through unchanged.
func osc8Hyperlink(uri, text string) string {
	if uri == "" {
		return text
	}
	return osc8Open + uri + osc8AfterURI + text + osc8Close
}

// fileLineRe matches `path[:line[:col]]` at word boundaries.
//
// Path requirements (must satisfy ALL):
//   - Anchored at start-of-string OR after whitespace/quote/paren so
//     we don't snag URLs (https://foo.com) or option strings (--foo=bar).
//   - Must END with one of the known source extensions, immediately
//     followed by a path-terminator (space, colon, end-of-string, etc).
//     This is the critical fix vs the prior version: `github.com/x/y`
//     previously matched because the regex used `c` as an extension
//     anchor and then lazy-extended through the rest of the path.
//     Requiring the extension at the END of the basename closes that
//     false-positive door.
//
// The path body is captured; line and column come from the optional
// trailing `:NUM[:NUM]` after the extension.
var fileLineRe = regexp.MustCompile(
	`(?:^|[\s"'(\[><])` +
		`([a-zA-Z0-9_./~+-]*[a-zA-Z0-9_-]\.` +
		`(?:go|rs|py|ts|tsx|js|jsx|c|cc|cpp|h|hpp|java|kt|swift|rb|php|sh|md|yaml|yml|json|toml|sql|html|css))` +
		`(?::(\d+)(?::(\d+))?)?` +
		`(?:$|[\s:,.;)\]>"'])`)

// LinkifyFileRefs scans text for `path:line` patterns and wraps each
// hit in an OSC-8 hyperlink (file://<abspath>#L<line>). Plain text
// outside hits is returned unchanged.
//
// Used by tool-output rendering so users can ⌘-click / Ctrl-click
// any path:line in a grep/build-error output to jump straight to
// the source. DeepSeek-TUI #374 parity.
//
// projectRoot is prepended to relative paths so the resulting URI
// is absolute (file:// requires absolute paths). Empty projectRoot
// leaves relative paths un-rewritten.
func LinkifyFileRefs(text, projectRoot string) string {
	return fileLineRe.ReplaceAllStringFunc(text, func(match string) string {
		// Re-extract sub-groups; ReplaceAllStringFunc only gives us
		// the whole match, so we re-run the regex with FindStringSubmatch.
		groups := fileLineRe.FindStringSubmatch(match)
		if len(groups) < 2 {
			return match
		}
		path := groups[1]
		line := ""
		if len(groups) >= 3 {
			line = groups[2]
		}
		// Build absolute path for the file:// URI.
		abs := path
		if strings.HasPrefix(path, "~/") {
			// Leave tilde paths alone — file:// can't resolve them
			// portably and most terminals won't expand them either.
			abs = path
		} else if !filepath.IsAbs(path) && projectRoot != "" {
			abs = filepath.Join(projectRoot, path)
		}
		uri := "file://" + url.PathEscape(abs)
		uri = strings.ReplaceAll(uri, "%2F", "/") // keep path slashes readable
		if line != "" {
			uri += "#L" + line
		}
		// Reconstruct the match: leading delimiter (if any) + linked
		// path + trailing delimiter (if any).
		linked := osc8Hyperlink(uri, path)
		if line != "" {
			linked += ":" + line
			if len(groups) >= 4 && groups[3] != "" {
				linked += ":" + groups[3]
			}
		}
		// Splice back into the original match preserving boundary chars.
		coreStart := strings.Index(match, path)
		if coreStart < 0 {
			return match
		}
		// "core" = path + optional :line[:col]
		coreLen := len(path)
		if line != "" {
			coreLen += 1 + len(line)
			if len(groups) >= 4 && groups[3] != "" {
				coreLen += 1 + len(groups[3])
			}
		}
		return match[:coreStart] + linked + match[coreStart+coreLen:]
	})
}
