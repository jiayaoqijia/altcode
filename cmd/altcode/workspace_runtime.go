package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/jiayaoqijia/altcode/internal/workspace"
)

// processRuntime is a minimal Runtime that spawns OS processes.
// Tracks spawned processes and captures output for one-shot agents.
// Supports fan-out streaming via subscribers for Attach().
type processRuntime struct {
	mu          sync.Mutex
	procs       map[string]*os.Process
	outputs     map[string]*bytes.Buffer
	exits       map[string]int
	callbacks   map[string]func(string)
	subscribers map[string][]chan string
}

func (p *processRuntime) Name() string { return "process" }

// OnOutput registers a per-line callback for a spawned process.
func (p *processRuntime) OnOutput(
	handleID string, cb func(string),
) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.callbacks == nil {
		p.callbacks = make(map[string]func(string))
	}
	p.callbacks[handleID] = cb
}

func (p *processRuntime) Spawn(
	ctx context.Context,
	argv []string,
	env []string,
	workdir string,
) (workspace.RuntimeHandle, error) {
	if len(argv) == 0 {
		return workspace.RuntimeHandle{},
			fmt.Errorf("empty command")
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = workdir
	cmd.Env = env
	setSysProcAttr(cmd)

	var buf bytes.Buffer
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		pw.Close()
		pr.Close()
		return workspace.RuntimeHandle{},
			fmt.Errorf("start: %w", err)
	}
	h := workspace.RuntimeHandle{
		ID:        fmt.Sprintf("pid:%d", cmd.Process.Pid),
		StartedAt: time.Now(),
	}
	p.mu.Lock()
	if p.procs == nil {
		p.procs = make(map[string]*os.Process)
	}
	if p.outputs == nil {
		p.outputs = make(map[string]*bytes.Buffer)
	}
	if p.exits == nil {
		p.exits = make(map[string]int)
	}
	if p.callbacks == nil {
		p.callbacks = make(map[string]func(string))
	}
	p.procs[h.ID] = cmd.Process
	p.mu.Unlock()

	// Scanner goroutine: reads lines, calls callback,
	// fans out to subscribers, fills buffer.
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		scanner := bufio.NewScanner(pr)
		scanner.Buffer(
			make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			buf.WriteString(line + "\n")
			p.mu.Lock()
			cb := p.callbacks[h.ID]
			subs := append(
				[]chan string(nil),
				p.subscribers[h.ID]...)
			p.mu.Unlock()
			if cb != nil {
				cb(line)
			}
			for _, sub := range subs {
				select {
				case sub <- line:
				default: // drop if subscriber is slow
				}
			}
		}
		pr.Close()
	}()

	// Wait goroutine: waits for exit, drains reader, stores result.
	go func() {
		werr := cmd.Wait()
		pw.Close()
		<-readerDone
		p.mu.Lock()
		delete(p.procs, h.ID)
		p.outputs[h.ID] = &buf
		if werr != nil {
			if exitErr, ok := werr.(*exec.ExitError); ok {
				p.exits[h.ID] = exitErr.ExitCode()
			} else {
				p.exits[h.ID] = 1
			}
		} else {
			p.exits[h.ID] = 0
		}
		p.mu.Unlock()
	}()
	return h, nil
}

// Attach returns a channel that receives all future output
// lines from the given handle. Multiple callers can attach
// concurrently; each gets its own channel (fan-out). The
// channel is closed when the context is cancelled.
func (p *processRuntime) Attach(
	ctx context.Context, h workspace.RuntimeHandle,
) (<-chan string, error) {
	ch := make(chan string, 100)
	p.mu.Lock()
	if p.subscribers == nil {
		p.subscribers = make(map[string][]chan string)
	}
	p.subscribers[h.ID] = append(
		p.subscribers[h.ID], ch)
	p.mu.Unlock()

	go func() {
		<-ctx.Done()
		p.mu.Lock()
		p.removeSubscriber(h.ID, ch)
		p.mu.Unlock()
		close(ch)
	}()
	return ch, nil
}

// removeSubscriber removes ch from the subscriber list.
// Caller must hold p.mu.
func (p *processRuntime) removeSubscriber(
	id string, ch chan string,
) {
	subs := p.subscribers[id]
	for i, s := range subs {
		if s == ch {
			p.subscribers[id] = append(
				subs[:i], subs[i+1:]...)
			return
		}
	}
}

func (p *processRuntime) Kill(
	h workspace.RuntimeHandle,
) error {
	p.mu.Lock()
	proc, ok := p.procs[h.ID]
	p.mu.Unlock()
	if !ok {
		return nil
	}
	return proc.Kill()
}

// KillAll terminates all tracked agent processes.
func (p *processRuntime) KillAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, proc := range p.procs {
		proc.Kill() //nolint:errcheck
	}
}

func (p *processRuntime) IsRunning(
	h workspace.RuntimeHandle,
) (bool, error) {
	var pid int
	if _, err := fmt.Sscanf(
		h.ID, "pid:%d", &pid); err != nil {
		return false, nil
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false, nil
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil, nil
}

// IsStillRunning checks a handle ID directly.
func (p *processRuntime) IsStillRunning(
	handleID string,
) bool {
	alive, _ := p.IsRunning(
		workspace.RuntimeHandle{ID: handleID})
	return alive
}

// GetOutput returns captured output for a completed agent.
func (p *processRuntime) GetOutput(
	handleID string,
) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if buf, ok := p.outputs[handleID]; ok {
		return buf.String()
	}
	return ""
}

// GetExitCode returns the exit code for a completed agent.
// Returns -1 if the agent hasn't exited yet.
func (p *processRuntime) GetExitCode(
	handleID string,
) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if code, ok := p.exits[handleID]; ok {
		return code
	}
	return -1
}
