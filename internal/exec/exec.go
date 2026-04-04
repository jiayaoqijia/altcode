package exec

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/altcode-ai/altcode/internal/engine"
	"github.com/altcode-ai/altcode/internal/event"
)

// Params configures a headless execution run.
type Params struct {
	EngineParams engine.EngineParams
	Engine       *engine.Engine // if set, use this engine (skips New)
	Prompt       string
	JSON         bool      // emit JSONL events to Writer
	Quiet        bool      // suppress banner
	Model        string    // for banner display
	Auth         string    // credential source for banner
	Writer       io.Writer // defaults to os.Stdout
}

// Run executes a single prompt headlessly and writes output.
func Run(ctx context.Context, p Params) error {
	eng := p.Engine
	if eng == nil {
		var err error
		eng, err = engine.New(p.EngineParams)
		if err != nil {
			return fmt.Errorf("create engine: %w", err)
		}
	}

	w := p.Writer
	if w == nil {
		w = os.Stdout
	}

	if !p.JSON && !p.Quiet && isTerminal(w) {
		printBanner(w, p)
	}

	start := time.Now()
	ch := eng.Run(ctx, p.Prompt)

	var err error
	if p.JSON {
		err = drainJSON(ch, w)
	} else {
		err = drainText(ch, w)
	}

	if !p.JSON && !p.Quiet && isTerminal(w) {
		printFooter(w, time.Since(start))
	}
	return err
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	stat, err := f.Stat()
	if err != nil {
		return false
	}
	return stat.Mode()&os.ModeCharDevice != 0
}

const (
	dim    = "\033[2m"
	bold   = "\033[1m"
	green  = "\033[32m"
	purple = "\033[35m"
	cyan   = "\033[36m"
	reset  = "\033[0m"
)

func printBanner(w io.Writer, p Params) {
	model := p.Model
	if model == "" {
		model = "auto"
	}
	fmt.Fprintf(w, "%s╭─ %saltcode%s %s%s%s", dim, bold+purple, reset, dim, model, reset)
	if p.Auth != "" {
		fmt.Fprintf(w, " %s(%s)%s", dim, p.Auth, reset)
	}
	fmt.Fprintf(w, "\n%s│%s\n", dim, reset)
}

func printFooter(w io.Writer, elapsed time.Duration) {
	ms := elapsed.Milliseconds()
	var timing string
	if ms < 1000 {
		timing = fmt.Sprintf("%dms", ms)
	} else {
		timing = fmt.Sprintf("%.1fs", elapsed.Seconds())
	}
	fmt.Fprintf(w, "%s│%s\n", dim, reset)
	fmt.Fprintf(w, "%s╰─ %s%s%s %s\n", dim, green, timing, reset, dim+reset)
}

func truncatePrompt(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

func drainText(ch <-chan event.Event, w io.Writer) error {
	var lastErr string
	for ev := range ch {
		switch ev.Type {
		case event.TextDelta:
			fmt.Fprint(w, ev.Text)
		case event.ErrorEvent:
			lastErr = ev.Error
		case event.Done:
			fmt.Fprintln(w)
		}
	}
	if lastErr != "" {
		return fmt.Errorf("%s", lastErr)
	}
	return nil
}

func drainJSON(ch <-chan event.Event, w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	var lastErr string
	for ev := range ch {
		enc.Encode(ev)
		if ev.Type == event.ErrorEvent {
			lastErr = ev.Error
		}
	}
	if lastErr != "" {
		return fmt.Errorf("%s", lastErr)
	}
	return nil
}
