package signal

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// daemonProcess wraps an os/exec.Cmd for the signal-cli daemon.
type daemonProcess struct {
	cmd *exec.Cmd
}

// startDaemon spawns the signal-cli daemon process.
func startDaemon(cfg Config) (*daemonProcess, error) {
	cliPath := cfg.CLIPath
	if cliPath == "" {
		cliPath = "signal-cli"
	}

	host := cfg.HTTPHost
	if host == "" {
		host = "127.0.0.1"
	}
	port := cfg.HTTPPort
	if port == 0 {
		port = 8080
	}

	args := []string{}
	if cfg.Account != "" {
		args = append(args, "-a", cfg.Account)
	}
	args = append(args, "daemon",
		"--http", fmt.Sprintf("%s:%d", host, port),
		"--receive-mode", "on-start",
	)

	cmd := exec.Command(cliPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start signal-cli: %w", err)
	}

	return &daemonProcess{cmd: cmd}, nil
}

// stopDaemon gracefully shuts down the signal-cli daemon.
func stopDaemon(d *daemonProcess) {
	if d == nil || d.cmd == nil || d.cmd.Process == nil {
		return
	}

	if err := d.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		return
	}

	done := make(chan error, 1)
	go func() {
		_, err := d.cmd.Process.Wait()
		done <- err
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = d.cmd.Process.Kill()
	}
}

// waitForDaemon polls the daemon until ready.
func waitForDaemon(
	ctx context.Context, baseURL string, timeout time.Duration,
) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		body := []byte(
			`{"jsonrpc":"2.0","method":"version","id":0}`,
		)
		req, err := http.NewRequestWithContext(
			ctx, http.MethodPost, baseURL+"/api/v1/rpc",
			bytes.NewReader(body),
		)
		if err != nil {
			return fmt.Errorf(
				"create health check request: %w", err,
			)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}

		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf(
		"signal-cli daemon not ready after %s", timeout,
	)
}
