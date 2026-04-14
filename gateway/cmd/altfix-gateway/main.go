// altfix-gateway connects Telegram/Slack channels to the altcode daemon API.
//
// Usage:
//
//	altfix-gateway \
//	  --daemon-url http://localhost:4080 \
//	  --auth-token <token> \
//	  --repo-url https://github.com/org/repo \
//	  --telegram-token <token> \
//	  --slack-bot-token <token> \
//	  --slack-app-token <token>
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/altcode-ai/altcode/gateway"
	"github.com/altcode-ai/altcode/gateway/slack"
	"github.com/altcode-ai/altcode/gateway/telegram"
)

func main() {
	var (
		daemonURL     string
		authToken     string
		repoURL       string
		telegramToken string
		slackBotToken string
		slackAppToken string
	)

	flag.StringVar(&daemonURL, "daemon-url",
		env("ALTFIX_DAEMON_URL", "http://localhost:4080"),
		"altcode daemon URL")
	flag.StringVar(&authToken, "auth-token",
		env("ALTFIX_AUTH_TOKEN", ""),
		"daemon auth token")
	flag.StringVar(&repoURL, "repo-url",
		env("ALTFIX_REPO_URL", ""),
		"default repo URL for tasks")
	flag.StringVar(&telegramToken, "telegram-token",
		env("TELEGRAM_BOT_TOKEN", ""),
		"Telegram bot token")
	flag.StringVar(&slackBotToken, "slack-bot-token",
		env("SLACK_BOT_TOKEN", ""),
		"Slack bot token")
	flag.StringVar(&slackAppToken, "slack-app-token",
		env("SLACK_APP_TOKEN", ""),
		"Slack app-level token")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	mgr := gateway.NewManager(logger)

	bridge := gateway.NewAltFixBridge(gateway.BridgeConfig{
		DaemonURL: daemonURL,
		AuthToken: authToken,
		RepoURL:   repoURL,
	}, mgr)

	registered := 0

	if telegramToken != "" {
		ch, err := telegram.New(telegram.Config{
			Token: telegramToken,
		}, bridge.HandleMessage)
		if err != nil {
			logger.Error("telegram init failed", "err", err)
		} else {
			mgr.Register(ch)
			registered++
			logger.Info("telegram channel registered")
		}
	}

	if slackBotToken != "" && slackAppToken != "" {
		ch, err := slack.New(slack.Config{
			BotToken: slackBotToken,
			AppToken: slackAppToken,
		}, bridge.HandleMessage)
		if err != nil {
			logger.Error("slack init failed", "err", err)
		} else {
			mgr.Register(ch)
			registered++
			logger.Info("slack channel registered")
		}
	}

	if registered == 0 {
		fmt.Fprintln(os.Stderr,
			"no channels configured; set --telegram-token or "+
				"--slack-bot-token + --slack-app-token")
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(
		context.Background(), syscall.SIGINT, syscall.SIGTERM,
	)
	defer cancel()

	if err := mgr.StartAll(ctx); err != nil {
		logger.Error("start failed", "err", err)
		os.Exit(1)
	}

	logger.Info("gateway running",
		"daemon", daemonURL,
		"channels", registered,
	)

	<-ctx.Done()
	logger.Info("shutting down...")

	_ = mgr.StopAll(context.Background())
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
