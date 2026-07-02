package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

// TestBackgroundRegistryInjectsCompletedNotifications 验证后台任务完成后会注入内部通知消息。
func TestBackgroundRegistryInjectsCompletedNotifications(t *testing.T) {
	workdir := t.TempDir()
	rt := newAgentRuntime(testConfig(workdir), nil, nil)

	result := rt.runBackgroundBash([]byte(`{"command":"printf done"}`))
	if !strings.Contains(result, "Started background job") {
		t.Fatalf("unexpected start result: %s", result)
	}
	waitForBackground(t, rt)

	messages := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock("check background")),
	}
	rt.injectBackgroundNotifications(&messages)
	if len(messages) != 2 {
		t.Fatalf("expected notification message, got %d messages", len(messages))
	}
	text := extractResponseText(messages[1])
	if !strings.Contains(text, "<background>") || !strings.Contains(text, "done") {
		t.Fatalf("unexpected notification: %s", text)
	}
}

// waitForBackground 等待后台任务完成，避免测试依赖固定 sleep。
func waitForBackground(t *testing.T, rt *agentRuntime) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		jobs := rt.background.list()
		if len(jobs) > 0 && jobs[0].Status != "running" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("background job did not finish")
}
