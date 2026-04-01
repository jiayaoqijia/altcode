package command

import (
	"os/exec"
	"regexp"
	"strings"
)

var backtickRe = regexp.MustCompile("!`([^`]+)`")

// Expand replaces $ARGUMENTS with args and !`cmd` with command output.
func (c *Command) Expand(args string) (string, error) {
	body := c.Body

	// Replace $ARGUMENTS
	body = strings.ReplaceAll(body, "$ARGUMENTS", args)

	// Replace !`cmd` with output
	var lastErr error
	body = backtickRe.ReplaceAllStringFunc(body, func(match string) string {
		cmd := backtickRe.FindStringSubmatch(match)[1]
		out, err := exec.Command("sh", "-c", cmd).Output()
		if err != nil {
			lastErr = err
			return "(error: " + err.Error() + ")"
		}
		return strings.TrimSpace(string(out))
	})

	return body, lastErr
}
