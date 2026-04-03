// Package diff provides unified diff parsing and rendering with
// side-by-side display and lipgloss coloring.
package diff

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Line represents a single line within a diff hunk.
type Line struct {
	Number  int
	Content string
	Op      rune // '+', '-', ' '
}

// Hunk represents a contiguous section of changes.
type Hunk struct {
	OldStart int
	OldCount int
	NewStart int
	NewCount int
	Lines    []Line
}

// FileDiff represents the parsed diff for one file.
type FileDiff struct {
	OldPath string
	NewPath string
	Hunks   []Hunk
	Adds    int
	Deletes int
}

// --- styles (package-level, reusable) ---

var (
	addedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))  // green
	removedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))  // red
	contextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))  // gray
	lineNumStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))  // gray
	headerStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))  // cyan
	sepStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))  // gray
)

// hunkHeaderRe matches unified diff hunk headers.
var hunkHeaderRe = regexp.MustCompile(
	`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`,
)

// Parse parses a unified diff string into a slice of FileDiff results.
func Parse(unified string) ([]FileDiff, error) {
	lines := strings.Split(unified, "\n")
	var diffs []FileDiff
	var cur *FileDiff
	var curHunk *Hunk
	var oldLine, newLine int

	for _, raw := range lines {
		// File headers.
		if strings.HasPrefix(raw, "--- a/") {
			if cur != nil {
				flushHunk(cur, curHunk)
				diffs = append(diffs, *cur)
			}
			cur = &FileDiff{
				OldPath: strings.TrimPrefix(raw, "--- a/"),
			}
			curHunk = nil
			continue
		}
		if strings.HasPrefix(raw, "+++ b/") && cur != nil {
			cur.NewPath = strings.TrimPrefix(raw, "+++ b/")
			continue
		}

		// Hunk header.
		if m := hunkHeaderRe.FindStringSubmatch(raw); m != nil {
			if cur == nil {
				cur = &FileDiff{}
			}
			flushHunk(cur, curHunk)

			oldStart, _ := strconv.Atoi(m[1])
			oldCount := parseCount(m[2])
			newStart, _ := strconv.Atoi(m[3])
			newCount := parseCount(m[4])

			curHunk = &Hunk{
				OldStart: oldStart,
				OldCount: oldCount,
				NewStart: newStart,
				NewCount: newCount,
			}
			oldLine = oldStart
			newLine = newStart
			continue
		}

		// Skip noise lines.
		if strings.HasPrefix(raw, "\\ No newline") {
			continue
		}

		if curHunk == nil {
			continue
		}

		// Diff content lines.
		if len(raw) == 0 {
			curHunk.Lines = append(curHunk.Lines, Line{
				Number: oldLine, Content: "", Op: ' ',
			})
			oldLine++
			newLine++
			continue
		}

		switch raw[0] {
		case '+':
			curHunk.Lines = append(curHunk.Lines, Line{
				Number: newLine, Content: raw[1:], Op: '+',
			})
			if cur != nil {
				cur.Adds++
			}
			newLine++
		case '-':
			curHunk.Lines = append(curHunk.Lines, Line{
				Number: oldLine, Content: raw[1:], Op: '-',
			})
			if cur != nil {
				cur.Deletes++
			}
			oldLine++
		default:
			content := raw
			if len(raw) > 0 && raw[0] == ' ' {
				content = raw[1:]
			}
			curHunk.Lines = append(curHunk.Lines, Line{
				Number: oldLine, Content: content, Op: ' ',
			})
			oldLine++
			newLine++
		}
	}

	if cur != nil {
		flushHunk(cur, curHunk)
		diffs = append(diffs, *cur)
	}

	return diffs, nil
}

// Render produces a colored inline diff view (traditional unified).
func Render(fd FileDiff, width int) string {
	var sb strings.Builder

	sb.WriteString(headerStyle.Render(
		fmt.Sprintf("--- %s", fd.OldPath),
	))
	sb.WriteByte('\n')
	sb.WriteString(headerStyle.Render(
		fmt.Sprintf("+++ %s", fd.NewPath),
	))
	sb.WriteByte('\n')

	for _, h := range fd.Hunks {
		sb.WriteString(headerStyle.Render(
			fmt.Sprintf("@@ -%d,%d +%d,%d @@",
				h.OldStart, h.OldCount,
				h.NewStart, h.NewCount),
		))
		sb.WriteByte('\n')

		for _, ln := range h.Lines {
			renderLine(&sb, ln, width)
		}
	}
	return sb.String()
}

// RenderSideBySide produces a two-column diff view with line numbers.
func RenderSideBySide(fd FileDiff, width int) string {
	if width < 40 {
		width = 40
	}

	var sb strings.Builder
	colW := width / 2

	sb.WriteString(headerStyle.Render(
		fmt.Sprintf("--- %s", fd.OldPath),
	))
	sb.WriteString(strings.Repeat(" ", max(0, colW-len(fd.OldPath)-4)))
	sb.WriteString(headerStyle.Render(
		fmt.Sprintf("+++ %s", fd.NewPath),
	))
	sb.WriteByte('\n')
	sb.WriteString(sepStyle.Render(strings.Repeat("─", width)))
	sb.WriteByte('\n')

	for _, h := range fd.Hunks {
		pairs := pairLines(h.Lines)
		for _, p := range pairs {
			left := formatSideCell(p.left, colW)
			right := formatSideCell(p.right, colW)
			sb.WriteString(left)
			sb.WriteString(sepStyle.Render("│"))
			sb.WriteString(right)
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// --- helpers ---

func parseCount(s string) int {
	if s == "" {
		return 1
	}
	n, _ := strconv.Atoi(s)
	return n
}

func flushHunk(fd *FileDiff, h *Hunk) {
	if h != nil {
		fd.Hunks = append(fd.Hunks, *h)
	}
}

func renderLine(sb *strings.Builder, ln Line, width int) {
	num := lineNumStyle.Render(fmt.Sprintf("%4d ", ln.Number))
	content := truncate(ln.Content, width-6)

	switch ln.Op {
	case '+':
		sb.WriteString(num)
		sb.WriteString(addedStyle.Render("+" + content))
	case '-':
		sb.WriteString(num)
		sb.WriteString(removedStyle.Render("-" + content))
	default:
		sb.WriteString(num)
		sb.WriteString(contextStyle.Render(" " + content))
	}
	sb.WriteByte('\n')
}

type sidePair struct {
	left  *Line
	right *Line
}

func pairLines(lines []Line) []sidePair {
	var pairs []sidePair
	i := 0
	for i < len(lines) {
		switch lines[i].Op {
		case '-':
			if i+1 < len(lines) && lines[i+1].Op == '+' {
				pairs = append(pairs, sidePair{
					left: &lines[i], right: &lines[i+1],
				})
				i += 2
			} else {
				pairs = append(pairs, sidePair{
					left: &lines[i], right: nil,
				})
				i++
			}
		case '+':
			pairs = append(pairs, sidePair{
				left: nil, right: &lines[i],
			})
			i++
		default:
			pairs = append(pairs, sidePair{
				left: &lines[i], right: &lines[i],
			})
			i++
		}
	}
	return pairs
}

func formatSideCell(ln *Line, colW int) string {
	if ln == nil {
		return strings.Repeat(" ", colW-1)
	}

	numW := 5
	contentW := colW - numW - 2 // margin
	num := lineNumStyle.Render(fmt.Sprintf("%4d ", ln.Number))
	content := truncate(ln.Content, contentW)

	var styled string
	switch ln.Op {
	case '+':
		styled = addedStyle.Render(content)
	case '-':
		styled = removedStyle.Render(content)
	default:
		styled = contextStyle.Render(content)
	}

	text := num + styled
	pad := max(0, colW-1-visibleLen(text))
	return text + strings.Repeat(" ", pad)
}

func truncate(s string, maxW int) string {
	if maxW <= 0 {
		return ""
	}
	if len(s) <= maxW {
		return s
	}
	if maxW <= 3 {
		return s[:maxW]
	}
	return s[:maxW-3] + "..."
}

func visibleLen(s string) int {
	// Strip ANSI escape sequences for width calculation.
	re := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	return len(re.ReplaceAllString(s, ""))
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
