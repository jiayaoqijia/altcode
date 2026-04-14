package signal

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/altcode-ai/altcode/gateway"
)

// SSE envelope types from signal-cli

type sseEnvelope struct {
	Envelope signalEnvelope `json:"envelope"`
	Account  string         `json:"account"`
}

type signalEnvelope struct {
	Source       string       `json:"source"`
	SourceUUID   string       `json:"sourceUuid"`
	SourceName   string       `json:"sourceName"`
	Timestamp    int64        `json:"timestamp"`
	DataMessage  *dataMessage `json:"dataMessage"`
	SyncMessage  *syncMessage `json:"syncMessage"`
	SourceNumber string       `json:"sourceNumber"`
}

type syncMessage struct {
	SentMessage *sentMessage `json:"sentMessage"`
}

type sentMessage struct {
	Timestamp   int64      `json:"timestamp"`
	Message     string     `json:"message"`
	GroupInfo   *groupInfo `json:"groupInfo"`
	Mentions    []mention  `json:"mentions"`
	Destination string     `json:"destination"`
}

type dataMessage struct {
	Timestamp int64      `json:"timestamp"`
	Message   string     `json:"message"`
	GroupInfo *groupInfo `json:"groupInfo"`
	Mentions  []mention  `json:"mentions"`
}

type groupInfo struct {
	GroupID string `json:"groupId"`
	Type    string `json:"type"`
}

type mention struct {
	Start  int    `json:"start"`
	Length int    `json:"length"`
	UUID   string `json:"uuid"`
}

func (c *Channel) sseLoop() {
	backoff := time.Second
	maxBackoff := 30 * time.Second

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		err := c.connectSSE()
		if err != nil {
			if c.ctx.Err() != nil {
				return
			}

			select {
			case <-c.ctx.Done():
				return
			case <-time.After(backoff):
			}

			backoff = backoff * 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}

		backoff = time.Second
	}
}

func (c *Channel) connectSSE() error {
	url := c.baseURL + "/api/v1/events"

	req, err := http.NewRequestWithContext(
		c.ctx, http.MethodGet, url, nil,
	)
	if err != nil {
		return fmt.Errorf("create SSE request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")

	sseClient := &http.Client{Timeout: 0}
	resp, err := sseClient.Do(req)
	if err != nil {
		return fmt.Errorf("SSE connect: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf(
			"SSE returned status %d", resp.StatusCode,
		)
	}

	scanner := bufio.NewScanner(resp.Body)
	var dataLines []string

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "data:") {
			data := strings.TrimPrefix(line, "data:")
			data = strings.TrimSpace(data)
			dataLines = append(dataLines, data)
			continue
		}

		if line == "" && len(dataLines) > 0 {
			fullData := strings.Join(dataLines, "")
			dataLines = nil
			c.handleEnvelope(fullData)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("SSE read: %w", err)
	}

	return fmt.Errorf("SSE connection closed")
}

func (c *Channel) handleEnvelope(data string) {
	var env sseEnvelope
	if err := json.Unmarshal([]byte(data), &env); err != nil {
		return
	}

	if env.Envelope.DataMessage != nil {
		c.handleDataMessage(&env, env.Envelope.DataMessage)
		return
	}

	if env.Envelope.SyncMessage != nil &&
		env.Envelope.SyncMessage.SentMessage != nil {
		c.handleSyncSentMessage(
			&env, env.Envelope.SyncMessage.SentMessage,
		)
	}
}

func (c *Channel) handleDataMessage(
	env *sseEnvelope, dm *dataMessage,
) {
	if dm.Message == "" {
		return
	}

	if c.config.AccountUUID != "" &&
		env.Envelope.SourceUUID == c.config.AccountUUID {
		return
	}

	senderID := env.Envelope.Source
	if senderID == "" {
		senderID = env.Envelope.SourceUUID
	}

	if !c.isAllowed(senderID) {
		return
	}

	var chatID string
	content := dm.Message

	if dm.GroupInfo != nil && dm.GroupInfo.GroupID != "" {
		chatID = "group:" + dm.GroupInfo.GroupID
		// In groups, only respond if bot is mentioned
		if !c.isBotMentioned(dm.Mentions) {
			return
		}
	} else {
		chatID = senderID
	}

	messageID := fmt.Sprintf("%d", env.Envelope.Timestamp)

	c.Handler()(c.ctx, gateway.InboundMessage{
		ChannelName: "signal",
		ChatID:      chatID,
		SenderID:    senderID,
		SenderName:  env.Envelope.SourceName,
		Text:        content,
		Timestamp:   time.Now(),
		MessageID:   messageID,
	})
}

func (c *Channel) handleSyncSentMessage(
	env *sseEnvelope, sm *sentMessage,
) {
	if sm.Message == "" {
		return
	}

	if sm.GroupInfo == nil || sm.GroupInfo.GroupID == "" {
		return
	}

	senderID := env.Envelope.Source
	if senderID == "" {
		senderID = env.Envelope.SourceUUID
	}

	chatID := "group:" + sm.GroupInfo.GroupID
	content := sm.Message

	if !c.isBotMentioned(sm.Mentions) {
		return
	}

	messageID := fmt.Sprintf("%d", env.Envelope.Timestamp)

	c.Handler()(c.ctx, gateway.InboundMessage{
		ChannelName: "signal",
		ChatID:      chatID,
		SenderID:    senderID,
		SenderName:  env.Envelope.SourceName,
		Text:        content,
		Timestamp:   time.Now(),
		MessageID:   messageID,
	})
}

func (c *Channel) isBotMentioned(mentions []mention) bool {
	if c.config.AccountUUID == "" {
		return false
	}
	for _, m := range mentions {
		if m.UUID == c.config.AccountUUID {
			return true
		}
	}
	return false
}
