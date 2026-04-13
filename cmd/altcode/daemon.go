package main

import (
	"context"
	"fmt"
	"os"

	"github.com/altcode-ai/altcode/internal/daemon"
	"github.com/spf13/cobra"
)

func newDaemonCmd() *cobra.Command {
	var port int
	var dataDir string
	var authToken string
	var maxTasks int

	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Run altcode as an HTTP daemon for AltFix",
		Long: `Start an HTTP server that accepts coding tasks, spawns
agent subprocesses, and streams progress via SSE/WebSocket.
Designed for AltFix VM deployment.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if authToken == "" {
				authToken = os.Getenv("ALTFIX_AUTH_TOKEN")
			}
			if authToken == "" {
				fmt.Fprintln(os.Stderr,
					"warning: no --auth-token or ALTFIX_AUTH_TOKEN set — "+
						"API endpoints are unauthenticated")
			}

			if port < 1 || port > 65535 {
				return fmt.Errorf("invalid port %d: must be 1-65535", port)
			}
			if maxTasks < 1 {
				return fmt.Errorf("invalid max-concurrent %d: must be >= 1", maxTasks)
			}

			srv, err := daemon.NewServer(daemon.ServerConfig{
				Port:      port,
				DataDir:   dataDir,
				AuthToken: authToken,
				MaxTasks:  maxTasks,
			})
			if err != nil {
				return err
			}

			recovered, err := daemon.RecoverOrphanedTasks(srv.Store())
			if err != nil {
				return fmt.Errorf("crash recovery: %w", err)
			}
			if recovered > 0 {
				fmt.Fprintf(os.Stderr,
					"altcode daemon: recovered %d orphaned tasks\n",
					recovered)
			}

			return srv.Run(context.Background())
		},
	}

	cmd.Flags().IntVar(&port, "port", 9100, "HTTP server port")
	cmd.Flags().StringVar(&dataDir, "data-dir", "",
		"Data directory (default ~/.altcode/daemon)")
	cmd.Flags().StringVar(&authToken, "auth-token", "",
		"Bearer token for API auth")
	cmd.Flags().IntVar(&maxTasks, "max-concurrent", 2,
		"Max concurrent tasks")

	return cmd
}
