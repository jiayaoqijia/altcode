package exec

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/altcode-ai/altcode/internal/event"
	"github.com/altcode-ai/altcode/internal/tool"
)

// mockPromptTool implements tool.Tool with a scripted response
// body. Used to verify handlePermissionRequest's parse + routing.
type mockPromptTool struct {
	name     string
	response string // JSON body returned by Execute
	execErr  error
}

func (m *mockPromptTool) Name() string                         { return m.name }
func (m *mockPromptTool) Description() string                  { return "mock prompt tool" }
func (m *mockPromptTool) Parameters() json.RawMessage           { return json.RawMessage(`{"type":"object"}`) }
func (m *mockPromptTool) IsConcurrencySafe() bool              { return true }
func (m *mockPromptTool) IsReadOnly() bool                     { return true }
func (m *mockPromptTool) PermissionPattern(input json.RawMessage) string {
	return m.name + ":*"
}
func (m *mockPromptTool) Execute(_ context.Context, _ json.RawMessage) (*tool.Result, error) {
	if m.execErr != nil {
		return nil, m.execErr
	}
	return &tool.Result{Output: m.response}, nil
}

func TestHandlePermissionRequest_AllowVerdict(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Register(&mockPromptTool{
		name:     "mcp__auth__ask",
		response: `{"allow":true,"reason":"ok"}`,
	})

	respCh := make(chan event.PermResponse, 1)
	ev := event.Event{
		Type: event.PermissionRequest,
		Permission: &event.PermReq{
			ToolName: "bash",
			Pattern:  "ls",
			Response: respCh,
		},
	}
	handlePermissionRequest(context.Background(), ev, "mcp__auth__ask", reg)
	select {
	case resp := <-respCh:
		if resp.Action != event.Allow {
			t.Errorf("expected Allow, got %v", resp.Action)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for response")
	}
}

func TestHandlePermissionRequest_DenyVerdict(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Register(&mockPromptTool{
		name:     "mcp__auth__ask",
		response: `{"allow":false,"reason":"blocked by policy"}`,
	})

	respCh := make(chan event.PermResponse, 1)
	ev := event.Event{
		Type: event.PermissionRequest,
		Permission: &event.PermReq{
			ToolName: "bash",
			Pattern:  "rm -rf",
			Response: respCh,
		},
	}
	handlePermissionRequest(context.Background(), ev, "mcp__auth__ask", reg)
	select {
	case resp := <-respCh:
		if resp.Action != event.Deny {
			t.Errorf("expected Deny, got %v", resp.Action)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

func TestHandlePermissionRequest_MalformedJSON_FailsClosed(t *testing.T) {
	// Prompt tool that returns unparseable JSON should be treated
	// as a deny (fail-closed) — a buggy tool must not accidentally
	// allow dangerous operations.
	reg := tool.NewRegistry()
	reg.Register(&mockPromptTool{
		name:     "mcp__auth__ask",
		response: `not valid json`,
	})

	respCh := make(chan event.PermResponse, 1)
	ev := event.Event{
		Type: event.PermissionRequest,
		Permission: &event.PermReq{
			ToolName: "bash",
			Response: respCh,
		},
	}
	handlePermissionRequest(context.Background(), ev, "mcp__auth__ask", reg)
	select {
	case resp := <-respCh:
		if resp.Action != event.Deny {
			t.Errorf("expected Deny on parse failure, got %v", resp.Action)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

func TestHandlePermissionRequest_NoPromptToolFailsClosed(t *testing.T) {
	// Without --permission-prompt-tool set, the handler must
	// fail-closed immediately so headless mode doesn't block.
	respCh := make(chan event.PermResponse, 1)
	ev := event.Event{
		Type: event.PermissionRequest,
		Permission: &event.PermReq{
			ToolName: "bash",
			Response: respCh,
		},
	}
	handlePermissionRequest(context.Background(), ev, "", nil)
	select {
	case resp := <-respCh:
		if resp.Action != event.Deny {
			t.Errorf("expected Deny, got %v", resp.Action)
		}
	default:
		t.Fatal("response not sent for empty prompt tool")
	}
}

func TestHandlePermissionRequest_MissingToolFailsClosed(t *testing.T) {
	reg := tool.NewRegistry()
	// No tool registered with this name
	respCh := make(chan event.PermResponse, 1)
	ev := event.Event{
		Type: event.PermissionRequest,
		Permission: &event.PermReq{
			ToolName: "bash",
			Response: respCh,
		},
	}
	handlePermissionRequest(context.Background(), ev, "mcp__auth__ask", reg)
	select {
	case resp := <-respCh:
		if resp.Action != event.Deny {
			t.Errorf("expected Deny, got %v", resp.Action)
		}
	default:
		t.Fatal("response not sent for missing tool")
	}
}

func TestHandlePermissionRequest_ToolExecError_FailsClosed(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Register(&mockPromptTool{
		name:    "mcp__auth__ask",
		execErr: errors.New("connection refused"),
	})

	respCh := make(chan event.PermResponse, 1)
	ev := event.Event{
		Type: event.PermissionRequest,
		Permission: &event.PermReq{
			ToolName: "bash",
			Response: respCh,
		},
	}
	handlePermissionRequest(context.Background(), ev, "mcp__auth__ask", reg)
	select {
	case resp := <-respCh:
		if resp.Action != event.Deny {
			t.Errorf("expected Deny on exec error, got %v", resp.Action)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

func TestValidatePromptToolName(t *testing.T) {
	cases := []struct {
		name    string
		wantErr bool
	}{
		{"", false}, // empty = unset, ok
		{"mcp__auth__ask", false},
		{"mcp__server__deep__tool", false}, // multi-underscore ok
		{"ask", true},                      // bareword rejected
		{"auth__ask", true},                // missing mcp__ prefix
		{"  mcp__auth__ask  ", false},      // whitespace trimmed
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePromptToolName(tc.name)
			if tc.wantErr && err == nil {
				t.Errorf("expected error for %q", tc.name)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error for %q: %v", tc.name, err)
			}
		})
	}
}

func TestValidate_PromptToolBareword(t *testing.T) {
	p := &Params{PermissionPromptTool: "bareword"}
	err := p.Validate()
	if err == nil {
		t.Fatal("expected error on bareword prompt tool")
	}
	if !strings.Contains(err.Error(), "mcp__") {
		t.Errorf("expected mcp__ mention, got: %v", err)
	}
}

func TestValidate_BypassAndPromptToolConflict(t *testing.T) {
	p := &Params{
		PermissionMode:       "bypass",
		PermissionPromptTool: "mcp__auth__ask",
	}
	if err := p.Validate(); err == nil {
		t.Fatal("expected conflict error")
	}
}

func TestValidate_PromptToolAcceptsEmpty(t *testing.T) {
	// Empty = feature disabled, must not error.
	p := &Params{}
	if err := p.Validate(); err != nil {
		t.Errorf("unexpected: %v", err)
	}
}
