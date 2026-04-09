package backends

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/altcode-ai/altcode/internal/workspace"
)

// TmuxRuntime launches agents in tmux split panes with real PTYs.
// Each agent gets its own pane inside a shared tmux session, enabling
// human observation of TUI agents like claude and codex.
type TmuxRuntime struct {
	session string
	panes   map[string]string // handle ID -> tmux pane ID
	mu      sync.Mutex
	counter int // monotonic pane index for handle IDs
}

// NewTmuxRuntime creates a runtime that manages agents inside the
// named tmux session. The session is created lazily on first Spawn.
func NewTmuxRuntime(sessionName string) *TmuxRuntime {
	return &TmuxRuntime{
		session: sessionName,
		panes:   make(map[string]string),
	}
}

func (t *TmuxRuntime) Name() string { return "tmux" }

func (t *TmuxRuntime) Spawn(
	ctx context.Context,
	argv []string,
	env []string,
	workdir string,
) (workspace.RuntimeHandle, error) {
	if len(argv) == 0 {
		return workspace.RuntimeHandle{},
			fmt.Errorf("empty command")
	}
	if err := t.ensureTmux(); err != nil {
		return workspace.RuntimeHandle{}, err
	}
	if err := t.ensureSession(workdir); err != nil {
		return workspace.RuntimeHandle{}, err
	}

	// Split a new pane inside the session.
	out, err := t.run(
		"split-window", "-t", t.session,
		"-h", "-c", workdir, "-P", "-F", "#{pane_id}",
	)
	if err != nil {
		return workspace.RuntimeHandle{},
			fmt.Errorf("split-window: %w", err)
	}
	paneID := strings.TrimSpace(out)

	// Inject environment variables.
	for _, kv := range env {
		t.sendKeys(paneID,
			fmt.Sprintf("export %s", shellEscape(kv)))
	}

	// Launch the command. Each arg is shell-escaped to prevent
	// interpretation of special chars (parentheses, quotes, etc.)
	var quoted []string
	for _, a := range argv {
		quoted = append(quoted, shellEscape(a))
	}
	t.sendKeys(paneID, strings.Join(quoted, " "))

	// Auto-layout so panes tile evenly.
	_, _ = t.run("select-layout", "-t", t.session, "tiled")

	t.mu.Lock()
	idx := t.counter
	t.counter++
	handleID := fmt.Sprintf("tmux:%s:%d", t.session, idx)
	t.panes[handleID] = paneID
	t.mu.Unlock()

	return workspace.RuntimeHandle{
		ID:        handleID,
		StartedAt: time.Now(),
	}, nil
}

func (t *TmuxRuntime) Attach(
	ctx context.Context, h workspace.RuntimeHandle,
) (<-chan string, error) {
	t.mu.Lock()
	paneID, ok := t.panes[h.ID]
	t.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("unknown handle: %s", h.ID)
	}

	ch := make(chan string, 100)
	go func() {
		defer close(ch)
		var lastLen int
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				out, err := t.run(
					"capture-pane", "-t", paneID, "-p",
				)
				if err != nil {
					return
				}
				lines := strings.Split(out, "\n")
				if len(lines) > lastLen {
					for _, line := range lines[lastLen:] {
						select {
						case ch <- line:
						default:
						}
					}
					lastLen = len(lines)
				}
			}
		}
	}()
	return ch, nil
}

func (t *TmuxRuntime) Kill(
	h workspace.RuntimeHandle,
) error {
	t.mu.Lock()
	paneID, ok := t.panes[h.ID]
	t.mu.Unlock()
	if !ok {
		return nil
	}
	_, err := t.run("kill-pane", "-t", paneID)
	if err == nil {
		t.mu.Lock()
		delete(t.panes, h.ID)
		t.mu.Unlock()
	}
	return err
}

func (t *TmuxRuntime) IsRunning(
	h workspace.RuntimeHandle,
) (bool, error) {
	t.mu.Lock()
	paneID, ok := t.panes[h.ID]
	t.mu.Unlock()
	if !ok {
		return false, nil
	}
	out, err := t.run(
		"list-panes", "-t", t.session,
		"-F", "#{pane_id} #{pane_pid}",
	)
	if err != nil {
		return false, nil
	}
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) >= 2 && fields[0] == paneID {
			return true, nil
		}
	}
	return false, nil
}

// KillAll destroys the entire tmux session.
func (t *TmuxRuntime) KillAll() {
	_, _ = t.run("kill-session", "-t", t.session)
	t.mu.Lock()
	t.panes = make(map[string]string)
	t.mu.Unlock()
}

// ensureTmux checks that the tmux binary is on PATH.
func (t *TmuxRuntime) ensureTmux() error {
	_, err := exec.LookPath("tmux")
	if err != nil {
		return fmt.Errorf(
			"tmux not found on PATH; " +
				"install tmux to use --tmux mode")
	}
	return nil
}

// ensureSession creates the tmux session if it does not exist.
func (t *TmuxRuntime) ensureSession(workdir string) error {
	// has-session exits 0 if the session exists.
	_, err := t.run("has-session", "-t", t.session)
	if err == nil {
		return nil
	}
	_, err = t.run(
		"new-session", "-d", "-s", t.session,
		"-x", "200", "-y", "50", "-c", workdir,
	)
	if err != nil {
		return fmt.Errorf("create tmux session: %w", err)
	}
	return nil
}

// sendKeys sends a command string to a pane followed by Enter.
func (t *TmuxRuntime) sendKeys(paneID, text string) {
	_, _ = t.run("send-keys", "-t", paneID, text, "Enter")
}

// run executes a tmux subcommand and returns trimmed stdout.
func (t *TmuxRuntime) run(args ...string) (string, error) {
	cmd := exec.Command("tmux", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf(
			"tmux %s: %s: %w",
			args[0], strings.TrimSpace(stderr.String()), err)
	}
	return strings.TrimSpace(stdout.String()), nil
}
