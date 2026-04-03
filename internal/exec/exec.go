package exec

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/altcode-ai/altcode/internal/engine"
	"github.com/altcode-ai/altcode/internal/event"
)

// Params configures a headless execution run.
type Params struct {
	EngineParams engine.EngineParams
	Engine       *engine.Engine // if set, use this engine (skips New)
	Prompt       string
	JSON         bool      // emit JSONL events to Writer
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

	ch := eng.Run(ctx, p.Prompt)

	if p.JSON {
		return drainJSON(ch, w)
	}
	return drainText(ch, w)
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
