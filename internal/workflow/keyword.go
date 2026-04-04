package workflow

import "strings"

// Route maps user input to a workflow mode via keyword detection.
// Returns the matched mode and remaining prompt, or empty if no match.
func Route(input string) (Mode, string) {
	lower := strings.ToLower(input)

	for _, r := range routes {
		for _, kw := range r.keywords {
			if strings.Contains(lower, kw) {
				remaining := removeKeyword(input, kw)
				return r.mode, strings.TrimSpace(remaining)
			}
		}
	}
	return "", input
}

type route struct {
	mode     Mode
	keywords []string
}

var routes = []route{
	{ModeInterview, []string{
		"$interview", "$deep-interview",
		"clarify", "don't assume", "what exactly",
	}},
	{ModePlan, []string{
		"$plan", "$ralplan",
		"consensus plan", "review the plan",
	}},
	{ModeRalph, []string{
		"$ralph", "$persistent",
		"don't stop", "until done", "must complete",
	}},
	{ModeExecute, []string{
		"$execute", "$team",
		"run in parallel",
	}},
}

func removeKeyword(input, keyword string) string {
	lower := strings.ToLower(input)
	idx := strings.Index(lower, keyword)
	if idx < 0 {
		return input
	}
	return input[:idx] + input[idx+len(keyword):]
}
