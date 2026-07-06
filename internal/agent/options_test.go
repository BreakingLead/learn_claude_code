package agent

import (
	"io"
	"testing"
	"time"
)

func TestParseRunOptions(t *testing.T) {
	options, err := ParseRunOptions([]string{
		"--run-mode", "node-editor",
		"--node-editor-addr", ":9999",
		"--api-key", "key",
		"--base-url", "https://example.test",
		"--model", "model-a",
		"--fallback-model", "model-b",
		"--mode", "plan",
		"--use-blueprint",
		"--blueprint-id", "time-aware-agent",
		"--disable-modules", "memory,cron",
		"--resume-prompt=false",
		"--telegram-token", "token",
		"--telegram-base-url", "https://telegram.test/",
		"--telegram-allowed-chats", "1,2",
		"--telegram-poll-interval", "5s",
		"--telegram-timeout", "45s",
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	if options.RunMode != RunModeNodeEditor || options.NodeEditorAddr != ":9999" {
		t.Fatalf("unexpected run mode options: %+v", options)
	}
	if !options.UseBlueprint || options.BlueprintID != "time-aware-agent" || options.Mode != "plan" {
		t.Fatalf("unexpected agent options: %+v", options)
	}
	if options.APIKey != "key" || options.BaseURL != "https://example.test" || options.Model != "model-a" || options.FallbackModel != "model-b" {
		t.Fatalf("unexpected model options: %+v", options)
	}
	if options.ResumePrompt {
		t.Fatalf("expected resume prompt to be disabled: %+v", options)
	}
	if options.TelegramToken != "token" || options.TelegramBaseURL != "https://telegram.test/" || options.TelegramAllowedChats != "1,2" {
		t.Fatalf("unexpected telegram string options: %+v", options)
	}
	if options.TelegramPollInterval != 5*time.Second || options.TelegramTimeout != 45*time.Second {
		t.Fatalf("unexpected telegram duration options: %+v", options)
	}
}
