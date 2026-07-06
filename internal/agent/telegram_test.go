package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

func TestParseAllowedChats(t *testing.T) {
	allowed := parseAllowedChats("123, -1001, ,abc")
	for _, want := range []string{"123", "-1001", "abc"} {
		if !allowed[want] {
			t.Fatalf("missing allowed chat %q in %#v", want, allowed)
		}
	}
	if allowed[""] {
		t.Fatalf("empty chat should be ignored")
	}
}

func TestTelegramGetUpdatesUsesOffsetAndBaseURL(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":[{"update_id":7,"message":{"message_id":1,"date":1700000000,"text":"hi","chat":{"id":123},"from":{"id":9}}}]}`))
	}))
	defer server.Close()

	connector := newTelegramConnector(telegramConfig{Token: "token", BaseURL: server.URL, Timeout: 30 * time.Second}, nil, zeroAnthropicClient(), server.Client())
	connector.lastSeen = 5
	updates, err := connector.getUpdates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) != 1 {
		t.Fatalf("expected one update, got %d", len(updates))
	}
	if gotPath != "/bottoken/getUpdates" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	if gotBody["offset"].(float64) != 6 || gotBody["timeout"].(float64) != 30 {
		t.Fatalf("unexpected getUpdates body: %#v", gotBody)
	}
}

func TestTelegramSendTextUsesAdapterPayload(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	defer server.Close()

	connector := newTelegramConnector(telegramConfig{Token: "token", BaseURL: server.URL}, nil, zeroAnthropicClient(), server.Client())
	if err := connector.sendText(context.Background(), "123", "hello"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/bottoken/sendMessage" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	if gotBody["chat_id"] != "123" || gotBody["text"] != "hello" {
		t.Fatalf("unexpected send body: %#v", gotBody)
	}
}

func TestTelegramHandleUpdateSkipsDisallowedChat(t *testing.T) {
	connector := newTelegramConnector(telegramConfig{AllowedChats: map[string]bool{"456": true}}, nil, zeroAnthropicClient(), nil)
	err := connector.handleUpdate(context.Background(), json.RawMessage(`{"update_id":9,"message":{"message_id":1,"date":1700000000,"text":"hi","chat":{"id":123},"from":{"id":9}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if connector.lastSeen != 9 {
		t.Fatalf("expected offset to advance for ignored chat, got %d", connector.lastSeen)
	}
}

func TestTelegramConfigFromOptions(t *testing.T) {
	config := newTelegramConfigFromOptions(RunOptions{
		TelegramToken:        "abc",
		TelegramBaseURL:      "https://example.test/",
		TelegramAllowedChats: "1,2",
		TelegramPollInterval: 5 * time.Second,
		TelegramTimeout:      45 * time.Second,
	})
	if config.Token != "abc" || config.BaseURL != "https://example.test" || config.PollInterval != 5*time.Second || config.Timeout != 45*time.Second {
		t.Fatalf("unexpected config: %+v", config)
	}
	if !config.AllowedChats["1"] || !config.AllowedChats["2"] {
		t.Fatalf("unexpected allowed chats: %#v", config.AllowedChats)
	}
}

func TestTelegramAPIErrorIncludesDescription(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"description":"bad token"}`))
	}))
	defer server.Close()

	connector := newTelegramConnector(telegramConfig{Token: "token", BaseURL: server.URL}, nil, zeroAnthropicClient(), server.Client())
	err := connector.sendText(context.Background(), "123", "hello")
	if err == nil || !strings.Contains(err.Error(), "bad token") {
		t.Fatalf("expected api error, got %v", err)
	}
}

func zeroAnthropicClient() anthropic.Client {
	return anthropic.Client{}
}
