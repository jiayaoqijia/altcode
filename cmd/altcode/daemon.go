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
	var githubClientID string
	var githubClientSecret string
	var signingKey string
	var allowedOrgs []string
	var allowedUsers []string
	var adminUsers []string
	var testMode bool

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
				Port:               port,
				DataDir:            dataDir,
				AuthToken:          authToken,
				MaxTasks:           maxTasks,
				GitHubClientID:     githubClientID,
				GitHubClientSecret: githubClientSecret,
				AllowedOrgs:        allowedOrgs,
				AllowedUsers:       allowedUsers,
				AdminUsers:         adminUsers,
				SigningKey:          signingKey,
				TestMode:           testMode,
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

	cmd.Flags().IntVar(&port, "port", 9200, "HTTP server port")
	cmd.Flags().StringVar(&dataDir, "data-dir", "",
		"Data directory (default ~/.altcode/daemon)")
	cmd.Flags().StringVar(&authToken, "auth-token", "",
		"Bearer token for API auth")
	cmd.Flags().IntVar(&maxTasks, "max-concurrent", 2,
		"Max concurrent tasks")
	cmd.Flags().StringVar(&githubClientID, "github-client-id", "",
		"GitHub OAuth App client ID (enables web UI)")
	cmd.Flags().StringVar(&githubClientSecret, "github-client-secret", "",
		"GitHub OAuth App client secret")
	cmd.Flags().StringSliceVar(&allowedOrgs, "allowed-orgs", nil,
		"GitHub orgs allowed to access web UI")
	cmd.Flags().StringSliceVar(&allowedUsers, "allowed-users", nil,
		"GitHub users allowed to access web UI")
	cmd.Flags().StringSliceVar(&adminUsers, "admin-users", nil,
		"GitHub users with admin access")
	cmd.Flags().StringVar(&signingKey, "signing-key", "",
		"HMAC signing key for shared URLs")
	cmd.Flags().BoolVar(&testMode, "test-mode", false,
		"Enable /auth/test-login bypass for E2E tests (NEVER in production)")

	return cmd
}
