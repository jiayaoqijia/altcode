package orchestra

import "testing"

func TestParseClaudeStreamJSON_Text(t *testing.T) {
	line := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Hello world"}]}}`
	ev := ParseClaudeStreamJSON(line)
	if ev == nil {
		t.Fatal("expected event")
	}
	if ev.Type != KindText || ev.Text != "Hello world" {
		t.Errorf("got type=%s text=%q", ev.Type, ev.Text)
	}
}

func TestParseClaudeStreamJSON_ToolUse(t *testing.T) {
	line := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Read","id":"call_1","input":{"path":"/tmp"}}]}}`
	ev := ParseClaudeStreamJSON(line)
	if ev == nil {
		t.Fatal("expected event")
	}
	if ev.Type != KindToolStart || ev.Tool != "Read" {
		t.Errorf("got type=%s tool=%q", ev.Type, ev.Tool)
	}
}

func TestParseClaudeStreamJSON_Thinking(t *testing.T) {
	line := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","text":"Let me think..."}]}}`
	ev := ParseClaudeStreamJSON(line)
	if ev == nil {
		t.Fatal("expected event")
	}
	if ev.Type != KindThinking {
		t.Errorf("got type=%s, want thinking", ev.Type)
	}
}

func TestParseClaudeStreamJSON_Result(t *testing.T) {
	line := `{"type":"result","result_text":"Done","session_id":"sess-123"}`
	ev := ParseClaudeStreamJSON(line)
	if ev == nil {
		t.Fatal("expected event")
	}
	if ev.Type != KindPhaseDone || ev.SessionID != "sess-123" {
		t.Errorf("got type=%s session=%q", ev.Type, ev.SessionID)
	}
}

func TestParseClaudeStreamJSON_Invalid(t *testing.T) {
	if ParseClaudeStreamJSON("not json") != nil {
		t.Error("expected nil for invalid JSON")
	}
}

func TestParseClaudeStreamJSON_System(t *testing.T) {
	line := `{"type":"system","session_id":"s1"}`
	ev := ParseClaudeStreamJSON(line)
	if ev == nil || ev.Type != KindText {
		t.Error("expected text event for system message")
	}
}

func TestParseCodexLine(t *testing.T) {
	tests := []struct {
		line string
		kind PhaseEventKind
	}{
		{"[Read] /tmp/foo.go", KindToolStart},
		{"✓", KindToolDone},
		{"✓ done", KindToolDone},
		{"Error: something failed", KindError},
		{"error: oops", KindError},
		{"regular output text", KindText},
		{"", KindText},
	}
	for _, tt := range tests {
		ev := ParseCodexLine(tt.line)
		if ev.Type != tt.kind {
			t.Errorf("ParseCodexLine(%q) = %s, want %s", tt.line, ev.Type, tt.kind)
		}
	}
}

func TestParseCodexLine_ToolName(t *testing.T) {
	ev := ParseCodexLine("[Bash] git status")
	if ev.Tool != "Bash" {
		t.Errorf("tool = %q, want Bash", ev.Tool)
	}
}
