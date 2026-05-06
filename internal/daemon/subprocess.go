package daemon

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// AgentConfig describes how to spawn an agent subprocess.
type AgentConfig struct {
	Binary string   // "codex", "claude", "altcode", or any binary
	Args   []string // command-line arguments
	Dir    string   // working directory (worktree path)
	Env    []string // extra environment variables
	Role   string   // "lead", "implementer", "reviewer", "tester"
}

// AgentProcess wraps a running agent subprocess.
type AgentProcess struct {
	Cmd       *exec.Cmd
	Stdin     io.WriteCloser
	stdoutBuf *bytes.Buffer // buffered stdout (avoids pipe-close race with Wait)
	stderrBuf *bytes.Buffer // buffered stderr
	exitErr   error
	closeOnce sync.Once
	closed    chan struct{} // closed when process exits
}

// SpawnAgent starts an agent binary as a child process with its
// own process group (Setpgid) so Kill() can tear down the entire
// tree. The process inherits the daemon's env plus any extras
// from cfg.Env.
func SpawnAgent(ctx context.Context, cfg AgentConfig) (
	*AgentProcess, error,
) {
	cmd := exec.CommandContext(ctx, cfg.Binary, cfg.Args...)
	cmd.Dir = cfg.Dir
	cmd.Env = append(os.Environ(), cfg.Env...)

	// Own process group so Kill(-pgid) tears down grandchildren.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Cancel handler: kill the entire process group on ctx cancel.
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		pgid, err := syscall.Getpgid(cmd.Process.Pid)
		if err != nil {
			return cmd.Process.Kill()
		}
		return syscall.Kill(-pgid, syscall.SIGTERM)
	}
	cmd.WaitDelay = 5 * time.Second

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	// Use buffers instead of pipes for stdout/stderr so cmd.Wait()
	// doesn't close them before ReadAll can read (race with fast commands like echo).
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		// Close stdin pipe we allocated before Start — otherwise repeated
		// spawn failures leak fds. The reader half is owned by the child
		// (which never launched), and the writer half is ours to clean up.
		_ = stdin.Close()
		return nil, fmt.Errorf("start %s: %w", cfg.Binary, err)
	}

	proc := &AgentProcess{
		Cmd:       cmd,
		Stdin:     stdin,
		stdoutBuf: &stdoutBuf,
		stderrBuf: &stderrBuf,
		closed:    make(chan struct{}),
	}
	go func() {
		proc.exitErr = cmd.Wait()
		proc.Stdin.Close() // prevent fd leak
		proc.closeOnce.Do(func() { close(proc.closed) })
	}()
	return proc, nil
}

// ReadAll blocks until the process exits, then returns buffered stdout.
func (p *AgentProcess) ReadAll() (string, error) {
	<-p.closed
	return strings.TrimSpace(p.stdoutBuf.String()), nil
}

// SendMessage writes a line to the agent's stdin.
func (p *AgentProcess) SendMessage(msg string) error {
	if !strings.HasSuffix(msg, "\n") {
		msg += "\n"
	}
	_, err := io.WriteString(p.Stdin, msg)
	return err
}

// Wait blocks until the process exits and returns the exit error.
func (p *AgentProcess) Wait() error {
	<-p.closed
	return p.exitErr
}

// Kill sends SIGTERM to the process group, then SIGKILL after 5s.
// Safe to call multiple times and on already-exited processes.
func (p *AgentProcess) Kill() error {
	// Already exited — nothing to do.
	if !p.IsRunning() {
		return nil
	}
	if p.Cmd.Process == nil {
		return nil
	}
	pgid, err := syscall.Getpgid(p.Cmd.Process.Pid)
	if err != nil {
		// Process already gone — treat as success.
		<-p.closed
		return nil
	}
	// SIGTERM first for graceful shutdown.
	_ = syscall.Kill(-pgid, syscall.SIGTERM)

	// Wait up to 5s, then SIGKILL. Use NewTimer + Stop so the happy
	// path (process exits quickly) doesn't leak the timer fd.
	t := time.NewTimer(5 * time.Second)
	defer t.Stop()
	select {
	case <-p.closed:
		return nil
	case <-t.C:
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		<-p.closed
		return nil
	}
}

// IsRunning reports whether the process is still alive.
func (p *AgentProcess) IsRunning() bool {
	select {
	case <-p.closed:
		return false
	default:
		return true
	}
}
