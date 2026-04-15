# altcode gateway

Connects Telegram and Slack to the altcode daemon API. Send `/fix` commands from chat, track task status, steer agents, and monitor costs.

## Attribution

Channel infrastructure (split, rate limiting, manager pattern, Telegram/Slack implementations) adapted from [ottie](https://github.com/jiayaoqijia/ottie) (`pkg/channels/` and `pkg/gateway/`), MIT license. Ottie-specific dependencies (bus, identity, media, config) have been replaced with local types wired to the altcode daemon HTTP API.

## Architecture

```
Telegram/Slack  -->  Channel  -->  Manager  -->  AltFixBridge  -->  Daemon API
                     (recv)        (rate-limit)   (translate)       POST /tasks
                                   (retry)        /fix /status      GET /tasks
                                   (split)        /stop /steer      POST /tasks/:id/stop
```

## Usage

```bash
# Build
cd gateway && go build ./cmd/altfix-gateway/

# Run
./altfix-gateway \
  --daemon-url http://localhost:9200 \
  --auth-token $ALTFIX_AUTH_TOKEN \
  --repo-url https://github.com/org/repo \
  --telegram-token $TELEGRAM_BOT_TOKEN

# Environment variables also work
export ALTFIX_DAEMON_URL=http://localhost:9200
export ALTFIX_AUTH_TOKEN=secret
export ALTFIX_REPO_URL=https://github.com/org/repo
export TELEGRAM_BOT_TOKEN=123:abc
./altfix-gateway
```

## Commands

| Command | Action | Daemon API |
|---------|--------|-----------|
| `/fix <desc>` | Create task | POST /tasks |
| `/status` | List active tasks | GET /tasks |
| `/stop <id>` | Cancel task | POST /tasks/:id/stop |
| `/steer <id> <msg>` | Steer agent | POST /tasks/:id/steer |
| `/cost` | Show total cost | GET /tasks (sum costs) |
| `/help` | Show commands | Local |

Unrecognized messages are treated as `/fix` commands.

## Testing

```bash
cd gateway
go test ./... -race -count=1
```
