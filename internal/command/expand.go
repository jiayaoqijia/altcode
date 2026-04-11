package command

import (
	"os/exec"
	"regexp"
	"strings"
)

var backtickRe = regexp.MustCompile("!`([^`]+)`")

// Expand replaces !`cmd` with command output and $ARGUMENTS with args.
//
// Order matters: backtick expansion runs FIRST, with $ARGUMENTS inside
// command bodies single-quote-escaped before being handed to /bin/sh.
// Without escaping, an args string like `abc"; rm -rf ~; #` would
// turn `!\`grep "$ARGUMENTS" file\`` into a shell injection.
//
// Outside of !`...` blocks, $ARGUMENTS is substituted as raw text since
// the markdown body is fed to the model as a prompt, not to a shell.
func (c *Command) Expand(args string) (string, error) {
	body := c.Body
	escapedArgs := shellEscape(args)

	// Pass 1: expand backticks. Substitute the *escaped* args before
	// invoking sh so the user's input cannot break out of the quoting.
	var lastErr error
	body = backtickRe.ReplaceAllStringFunc(body, func(match string) string {
		cmd := backtickRe.FindStringSubmatch(match)[1]
		cmd = strings.ReplaceAll(cmd, "$ARGUMENTS", escapedArgs)
		out, err := exec.Command("sh", "-c", cmd).Output()
		if err != nil {
			lastErr = err
			return "(error: " + err.Error() + ")"
		}
		return strings.TrimSpace(string(out))
	})

	// Pass 2: substitute $ARGUMENTS in the remaining markdown body.
	// This text is fed to the model as a prompt, never to a shell.
	body = strings.ReplaceAll(body, "$ARGUMENTS", args)

	return body, lastErr
}

// shellEscape wraps s in single quotes for safe POSIX shell use,
// turning embedded single quotes into '\'' sequences.
func shellEscape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
