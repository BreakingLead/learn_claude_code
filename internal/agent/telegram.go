package agent

// 模块说明：
// telegram connector 是 messaging adapter 之上的真实平台接入层。它使用 Telegram
// long polling 拉取 update，复用 telegramAdapter 归一化 payload，再把统一消息交给
// agent runner；回复通过 Telegram sendMessage 发回原 chat。第一版作为 Telegram-only
// 入口运行，避免 TUI 和 Telegram 同时驱动同一个 runtime。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

type telegramConfig struct {
	Enabled      bool
	Token        string
	BaseURL      string
	AllowedChats map[string]bool
	PollInterval time.Duration
	Timeout      time.Duration
}

type telegramConnector struct {
	config   telegramConfig
	client   *http.Client
	adapter  telegramAdapter
	rt       *agentRuntime
	llm      anthropic.Client
	history  map[string][]anthropic.MessageParam
	lastSeen int64
}

type telegramAPIResponse struct {
	OK          bool              `json:"ok"`
	Description string            `json:"description"`
	Result      []json.RawMessage `json:"result"`
}

type telegramUpdateHeader struct {
	UpdateID int64 `json:"update_id"`
	Message  struct {
		Chat struct {
			ID int64 `json:"id"`
		} `json:"chat"`
	} `json:"message"`
}

func runTelegram(ctx context.Context, client anthropic.Client) {
	config, err := newAgentConfig()
	if err != nil {
		fmt.Println(colorRed("Config error: " + err.Error()))
		return
	}
	rt := newAgentRuntime(config, nil, nil)
	rt.startCronScheduler()
	telegramConfig := newTelegramConfigFromEnv()
	if strings.TrimSpace(telegramConfig.Token) == "" {
		fmt.Println(colorRed("Telegram error: TELEGRAM_BOT_TOKEN is required"))
		return
	}
	connector := newTelegramConnector(telegramConfig, rt, client, nil)
	rt.emitLine("[telegram] connector started")
	if err := connector.Run(ctx); err != nil {
		fmt.Println(colorRed("Telegram error: " + err.Error()))
	}
}

func newTelegramConfigFromEnv() telegramConfig {
	pollInterval := parseDurationEnv("TELEGRAM_POLL_INTERVAL", 2*time.Second)
	timeout := parseDurationEnv("TELEGRAM_TIMEOUT", 30*time.Second)
	baseURL := strings.TrimRight(getEnvOr("TELEGRAM_BASE_URL", "https://api.telegram.org"), "/")
	return telegramConfig{
		Enabled:      truthyEnv("BEE_AGENT_TELEGRAM"),
		Token:        os.Getenv("TELEGRAM_BOT_TOKEN"),
		BaseURL:      baseURL,
		AllowedChats: parseAllowedChats(os.Getenv("TELEGRAM_ALLOWED_CHATS")),
		PollInterval: pollInterval,
		Timeout:      timeout,
	}
}

func newTelegramConnector(config telegramConfig, rt *agentRuntime, llm anthropic.Client, httpClient *http.Client) *telegramConnector {
	if config.BaseURL == "" {
		config.BaseURL = "https://api.telegram.org"
	}
	config.BaseURL = strings.TrimRight(config.BaseURL, "/")
	if config.PollInterval <= 0 {
		config.PollInterval = 2 * time.Second
	}
	if config.Timeout <= 0 {
		config.Timeout = 30 * time.Second
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: config.Timeout + 5*time.Second}
	}
	return &telegramConnector{
		config:  config,
		client:  httpClient,
		adapter: telegramAdapter{},
		rt:      rt,
		llm:     llm,
		history: map[string][]anthropic.MessageParam{},
	}
}

func (c *telegramConnector) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		updates, err := c.getUpdates(ctx)
		if err != nil {
			c.log("[telegram] getUpdates: %v", err)
			if !telegramSleepContext(ctx, c.config.PollInterval) {
				return ctx.Err()
			}
			continue
		}
		for _, raw := range updates {
			if err := c.handleUpdate(ctx, raw); err != nil {
				c.log("[telegram] update: %v", err)
			}
		}
		if !telegramSleepContext(ctx, c.config.PollInterval) {
			return ctx.Err()
		}
	}
}

func (c *telegramConnector) getUpdates(ctx context.Context) ([]json.RawMessage, error) {
	params := map[string]any{
		"timeout": c.config.TimeoutSeconds(),
	}
	if c.lastSeen > 0 {
		params["offset"] = c.lastSeen + 1
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	resp, err := c.postJSON(ctx, "getUpdates", raw)
	if err != nil {
		return nil, err
	}
	var api telegramAPIResponse
	if err := json.Unmarshal(resp, &api); err != nil {
		return nil, err
	}
	if !api.OK {
		return nil, fmt.Errorf("%s", telegramAPIDescription(api.Description))
	}
	return api.Result, nil
}

func (c *telegramConnector) handleUpdate(ctx context.Context, raw json.RawMessage) error {
	var header telegramUpdateHeader
	if err := json.Unmarshal(raw, &header); err != nil {
		return err
	}
	if header.UpdateID > c.lastSeen {
		c.lastSeen = header.UpdateID
	}
	if header.Message.Chat.ID == 0 {
		return nil
	}
	chatID := strconv.FormatInt(header.Message.Chat.ID, 10)
	if !c.chatAllowed(chatID) {
		c.log("[telegram] ignored chat %s", chatID)
		return nil
	}
	msg, err := c.adapter.NormalizeInbound(raw)
	if err != nil {
		return err
	}
	if strings.TrimSpace(msg.Text) == "" {
		return nil
	}
	reply, err := c.runAgentForMessage(ctx, msg)
	if err != nil {
		return err
	}
	if strings.TrimSpace(reply) == "" {
		return nil
	}
	return c.sendText(ctx, msg.ChatID, reply)
}

func (c *telegramConnector) runAgentForMessage(ctx context.Context, msg unifiedMessage) (string, error) {
	if c.rt == nil {
		return "", fmt.Errorf("agent runtime is not initialized")
	}
	chatID := msg.ChatID
	history := append([]anthropic.MessageParam(nil), c.history[chatID]...)
	prompt := fmt.Sprintf("[Telegram chat_id=%s sender=%s sender_name=%s]\n%s", msg.ChatID, msg.SenderID, msg.SenderName, msg.Text)
	history = append(history, anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)))
	result := c.rt.runAgent(ctx, c.llm, c.rt.mainAgentSpec(), &history)
	c.history[chatID] = history
	if result.Error != "" {
		return "", fmt.Errorf("%s", result.Error)
	}
	return result.FinalText, nil
}

func (c *telegramConnector) sendText(ctx context.Context, chatID string, text string) error {
	outbound, err := c.adapter.BuildOutbound(unifiedMessage{ChatID: chatID, Text: text, MessageType: "text"})
	if err != nil {
		return err
	}
	resp, err := c.postJSON(ctx, "sendMessage", outbound)
	if err != nil {
		return err
	}
	var api struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(resp, &api); err != nil {
		return err
	}
	if !api.OK {
		return fmt.Errorf("%s", telegramAPIDescription(api.Description))
	}
	return nil
}

func (c *telegramConnector) postJSON(ctx context.Context, method string, body []byte) ([]byte, error) {
	url := fmt.Sprintf("%s/bot%s/%s", c.config.BaseURL, c.config.Token, method)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("telegram http %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return data, nil
}

func (c *telegramConnector) chatAllowed(chatID string) bool {
	if len(c.config.AllowedChats) == 0 {
		return true
	}
	return c.config.AllowedChats[chatID]
}

func (c *telegramConnector) log(format string, args ...any) {
	if c.rt != nil {
		c.rt.emitLine(format, args...)
		return
	}
	fmt.Printf(format+"\n", args...)
}

func (c telegramConfig) TimeoutSeconds() int {
	seconds := int(c.Timeout.Seconds())
	if seconds <= 0 {
		return 30
	}
	return seconds
}

func parseAllowedChats(raw string) map[string]bool {
	allowed := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		chat := strings.TrimSpace(part)
		if chat == "" {
			continue
		}
		allowed[chat] = true
	}
	return allowed
}

func telegramAPIDescription(description string) string {
	description = strings.TrimSpace(description)
	if description == "" {
		return "telegram api returned ok=false"
	}
	return description
}

func parseDurationEnv(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	duration, err := time.ParseDuration(raw)
	if err != nil {
		return fallback
	}
	return duration
}

func truthyEnv(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func telegramSleepContext(ctx context.Context, duration time.Duration) bool {
	if duration <= 0 {
		return true
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
