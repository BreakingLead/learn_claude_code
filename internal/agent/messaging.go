package agent

// 模块说明：
// messaging 模块是外部消息平台的适配中间层。平台 adapter 负责把 Feishu、
// Telegram 等平台 payload 转成统一消息格式；agent 和其它模块只依赖 UnifiedMessage，
// 不直接依赖某个平台的字段结构。第一版只做格式归一化和 outbound payload 构造，
// 网络收发由后续 connector 层接入。

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

type messagingModule struct {
	registry *messagePlatformRegistry
}

type unifiedMessage struct {
	ID          string         `json:"id"`
	Platform    string         `json:"platform"`
	ChatID      string         `json:"chat_id"`
	SenderID    string         `json:"sender_id"`
	SenderName  string         `json:"sender_name,omitempty"`
	Text        string         `json:"text"`
	MessageType string         `json:"message_type"`
	Timestamp   string         `json:"timestamp,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

type messagePlatformAdapter interface {
	Name() string
	Capabilities() []string
	NormalizeInbound(raw json.RawMessage) (unifiedMessage, error)
	BuildOutbound(msg unifiedMessage) (json.RawMessage, error)
}

type messagePlatformRegistry struct {
	adapters map[string]messagePlatformAdapter
}

type feishuAdapter struct{}
type telegramAdapter struct{}

func (m *messagingModule) ID() string {
	return "messaging"
}

func (m *messagingModule) Init(ctx ModuleContext) error {
	m.registry = newMessagePlatformRegistry()
	return nil
}

func (m *messagingModule) PromptBlocks(ctx context.Context, req PromptRequest) ([]PromptBlock, error) {
	if m.registry == nil {
		return nil, nil
	}
	if !hasString(req.ToolNames, "messaging_normalize") && !hasString(req.ToolNames, "messaging_build_outbound") {
		return nil, nil
	}
	return []PromptBlock{{
		Module:  m.ID(),
		Name:    "Messaging Middleware",
		Source:  "internal adapters",
		Content: "Use messaging_normalize to convert Feishu or Telegram webhook payloads into the unified message schema. Use messaging_build_outbound to construct platform-specific outbound payloads from a unified chat_id/text pair. New platforms should implement the messagePlatformAdapter interface instead of leaking platform fields into agent logic.",
	}}, nil
}

func (m *messagingModule) ToolDefinitions() []anthropic.ToolParam {
	return messagingToolDefinitions()
}

func (m *messagingModule) ToolHandlers() map[string]ToolHandler {
	return map[string]ToolHandler{
		"messaging_platforms":      m.runMessagingPlatforms,
		"messaging_normalize":      m.runMessagingNormalize,
		"messaging_build_outbound": m.runMessagingBuildOutbound,
	}
}

func (m *messagingModule) RuntimeSnapshot() any {
	if m.registry == nil {
		return nil
	}
	return map[string]any{
		"platforms": m.registry.names(),
	}
}

func newMessagePlatformRegistry() *messagePlatformRegistry {
	registry := &messagePlatformRegistry{adapters: map[string]messagePlatformAdapter{}}
	registry.register(feishuAdapter{})
	registry.register(telegramAdapter{})
	return registry
}

func (r *messagePlatformRegistry) register(adapter messagePlatformAdapter) {
	if adapter == nil {
		return
	}
	name := normalizePlatformName(adapter.Name())
	if name == "" {
		return
	}
	r.adapters[name] = adapter
}

func (r *messagePlatformRegistry) adapter(name string) (messagePlatformAdapter, bool) {
	adapter, ok := r.adapters[normalizePlatformName(name)]
	return adapter, ok
}

func (r *messagePlatformRegistry) names() []string {
	names := make([]string, 0, len(r.adapters))
	for name := range r.adapters {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func messagingToolDefinitions() []anthropic.ToolParam {
	return []anthropic.ToolParam{
		{
			Name:        "messaging_platforms",
			Description: anthropic.String("List supported external messaging platforms and adapter capabilities."),
			InputSchema: anthropic.ToolInputSchemaParam{Properties: map[string]any{}},
		},
		{
			Name:        "messaging_normalize",
			Description: anthropic.String("Normalize a Feishu or Telegram webhook payload into the unified message schema."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]any{
					"platform": map[string]any{"type": "string", "description": "feishu or telegram"},
					"payload":  map[string]any{"type": "object"},
				},
				Required: []string{"platform", "payload"},
			},
		},
		{
			Name:        "messaging_build_outbound",
			Description: anthropic.String("Build a platform-specific outbound payload from chat_id and text."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]any{
					"platform": map[string]any{"type": "string", "description": "feishu or telegram"},
					"chat_id":  map[string]any{"type": "string"},
					"text":     map[string]any{"type": "string"},
				},
				Required: []string{"platform", "chat_id", "text"},
			},
		},
	}
}

func (m *messagingModule) runMessagingPlatforms(raw json.RawMessage) string {
	if m.registry == nil {
		m.registry = newMessagePlatformRegistry()
	}
	var lines []string
	for _, name := range m.registry.names() {
		adapter, _ := m.registry.adapter(name)
		lines = append(lines, fmt.Sprintf("- %s: %s", name, strings.Join(adapter.Capabilities(), ", ")))
	}
	return strings.Join(lines, "\n")
}

func (m *messagingModule) runMessagingNormalize(raw json.RawMessage) string {
	if m.registry == nil {
		m.registry = newMessagePlatformRegistry()
	}
	var input struct {
		Platform string          `json:"platform"`
		Payload  json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	adapter, ok := m.registry.adapter(input.Platform)
	if !ok {
		return fmt.Sprintf("Error: unsupported platform: %s", input.Platform)
	}
	msg, err := adapter.NormalizeInbound(input.Payload)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return marshalCompactJSON(msg)
}

func (m *messagingModule) runMessagingBuildOutbound(raw json.RawMessage) string {
	if m.registry == nil {
		m.registry = newMessagePlatformRegistry()
	}
	var input struct {
		Platform string `json:"platform"`
		ChatID   string `json:"chat_id"`
		Text     string `json:"text"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	adapter, ok := m.registry.adapter(input.Platform)
	if !ok {
		return fmt.Sprintf("Error: unsupported platform: %s", input.Platform)
	}
	outbound, err := adapter.BuildOutbound(unifiedMessage{Platform: normalizePlatformName(input.Platform), ChatID: input.ChatID, Text: input.Text, MessageType: "text"})
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return string(outbound)
}

func (feishuAdapter) Name() string { return "feishu" }

func (feishuAdapter) Capabilities() []string {
	return []string{"normalize_inbound_text", "build_outbound_text"}
}

func (feishuAdapter) NormalizeInbound(raw json.RawMessage) (unifiedMessage, error) {
	var envelope struct {
		Header struct {
			EventID    string `json:"event_id"`
			CreateTime string `json:"create_time"`
		} `json:"header"`
		Event struct {
			Message struct {
				MessageID   string `json:"message_id"`
				ChatID      string `json:"chat_id"`
				MessageType string `json:"message_type"`
				Content     string `json:"content"`
				CreateTime  string `json:"create_time"`
			} `json:"message"`
			Sender struct {
				SenderID struct {
					OpenID string `json:"open_id"`
					UserID string `json:"user_id"`
				} `json:"sender_id"`
				SenderType string `json:"sender_type"`
			} `json:"sender"`
		} `json:"event"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return unifiedMessage{}, err
	}
	text := feishuTextContent(envelope.Event.Message.Content)
	msgType := envelope.Event.Message.MessageType
	if msgType == "" {
		msgType = "text"
	}
	messageID := envelope.Event.Message.MessageID
	if messageID == "" {
		messageID = envelope.Header.EventID
	}
	if envelope.Event.Message.ChatID == "" {
		return unifiedMessage{}, fmt.Errorf("feishu payload missing chat_id")
	}
	return unifiedMessage{
		ID:          messageID,
		Platform:    "feishu",
		ChatID:      envelope.Event.Message.ChatID,
		SenderID:    firstNonEmpty(envelope.Event.Sender.SenderID.OpenID, envelope.Event.Sender.SenderID.UserID),
		Text:        text,
		MessageType: msgType,
		Timestamp:   normalizeMillisTimestamp(firstNonEmpty(envelope.Event.Message.CreateTime, envelope.Header.CreateTime)),
		Metadata: map[string]any{
			"sender_type": envelope.Event.Sender.SenderType,
		},
	}, nil
}

func (feishuAdapter) BuildOutbound(msg unifiedMessage) (json.RawMessage, error) {
	chatID := strings.TrimSpace(msg.ChatID)
	text := strings.TrimSpace(msg.Text)
	if chatID == "" || text == "" {
		return nil, fmt.Errorf("chat_id and text are required")
	}
	content, _ := json.Marshal(map[string]string{"text": text})
	return json.Marshal(map[string]any{
		"receive_id": chatID,
		"msg_type":   "text",
		"content":    string(content),
	})
}

func (telegramAdapter) Name() string { return "telegram" }

func (telegramAdapter) Capabilities() []string {
	return []string{"normalize_inbound_text", "build_outbound_text"}
}

func (telegramAdapter) NormalizeInbound(raw json.RawMessage) (unifiedMessage, error) {
	var update struct {
		UpdateID int64 `json:"update_id"`
		Message  struct {
			MessageID int64  `json:"message_id"`
			Date      int64  `json:"date"`
			Text      string `json:"text"`
			Caption   string `json:"caption"`
			Chat      struct {
				ID    int64  `json:"id"`
				Title string `json:"title"`
				Type  string `json:"type"`
			} `json:"chat"`
			From struct {
				ID        int64  `json:"id"`
				Username  string `json:"username"`
				FirstName string `json:"first_name"`
				LastName  string `json:"last_name"`
			} `json:"from"`
		} `json:"message"`
	}
	if err := json.Unmarshal(raw, &update); err != nil {
		return unifiedMessage{}, err
	}
	if update.Message.Chat.ID == 0 {
		return unifiedMessage{}, fmt.Errorf("telegram payload missing message.chat.id")
	}
	text := firstNonEmpty(update.Message.Text, update.Message.Caption)
	messageID := fmt.Sprintf("%d", update.Message.MessageID)
	if update.Message.MessageID == 0 {
		messageID = fmt.Sprintf("%d", update.UpdateID)
	}
	return unifiedMessage{
		ID:          messageID,
		Platform:    "telegram",
		ChatID:      fmt.Sprintf("%d", update.Message.Chat.ID),
		SenderID:    fmt.Sprintf("%d", update.Message.From.ID),
		SenderName:  strings.TrimSpace(strings.Join([]string{update.Message.From.FirstName, update.Message.From.LastName}, " ")),
		Text:        text,
		MessageType: "text",
		Timestamp:   time.Unix(update.Message.Date, 0).UTC().Format(time.RFC3339),
		Metadata: map[string]any{
			"username":   update.Message.From.Username,
			"chat_type":  update.Message.Chat.Type,
			"chat_title": update.Message.Chat.Title,
		},
	}, nil
}

func (telegramAdapter) BuildOutbound(msg unifiedMessage) (json.RawMessage, error) {
	chatID := strings.TrimSpace(msg.ChatID)
	text := strings.TrimSpace(msg.Text)
	if chatID == "" || text == "" {
		return nil, fmt.Errorf("chat_id and text are required")
	}
	return json.Marshal(map[string]any{
		"chat_id": chatID,
		"text":    text,
	})
}

func feishuTextContent(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	var parsed struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(content), &parsed); err == nil && parsed.Text != "" {
		return parsed.Text
	}
	return content
}

func normalizeMillisTimestamp(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return value
	}
	if n > 1_000_000_000_000 {
		return time.UnixMilli(n).UTC().Format(time.RFC3339)
	}
	return time.Unix(n, 0).UTC().Format(time.RFC3339)
}

func normalizePlatformName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func marshalCompactJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return string(raw)
}
