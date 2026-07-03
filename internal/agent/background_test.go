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

func TestBackgroundBashUsesDefaultTimeout(t *testing.T) {
	workdir := t.TempDir()
	config := testConfig(workdir)
	config.BackgroundTimeout = 50 * time.Millisecond
	rt := newAgentRuntime(config, nil, nil)

	result := rt.runBackgroundBash([]byte(`{"command":"sleep 1"}`))
	if !strings.Contains(result, "Started background job") {
		t.Fatalf("unexpected start result: %s", result)
	}
	waitForBackground(t, rt)

	jobs := rt.background.list()
	if len(jobs) != 1 {
		t.Fatalf("expected one background job, got %d", len(jobs))
	}
	if jobs[0].Status != "timeout" {
		t.Fatalf("expected timeout status, got %+v", jobs[0])
	}
	if !strings.Contains(jobs[0].Error, "timed out") {
		t.Fatalf("expected timeout error, got %q", jobs[0].Error)
	}
}

func TestBackgroundBashAllowsTimeoutOverride(t *testing.T) {
	workdir := t.TempDir()
	config := testConfig(workdir)
	config.BackgroundTimeout = 50 * time.Millisecond
	rt := newAgentRuntime(config, nil, nil)

	result := rt.runBackgroundBash([]byte(`{"command":"sleep 0.1; printf done","timeout_seconds":1}`))
	if !strings.Contains(result, "timeout 1s") {
		t.Fatalf("expected override timeout in start result, got %s", result)
	}
	waitForBackground(t, rt)

	jobs := rt.background.list()
	if len(jobs) != 1 {
		t.Fatalf("expected one background job, got %d", len(jobs))
	}
	if jobs[0].Status != "completed" || jobs[0].Output != "done" {
		t.Fatalf("expected completed override job, got %+v", jobs[0])
	}
}

func TestBackgroundTimeoutRejectsNonPositiveOverride(t *testing.T) {
	rt := newAgentRuntime(testConfig(t.TempDir()), nil, nil)
	result := rt.runBackgroundBash([]byte(`{"command":"printf done","timeout_seconds":0}`))
	if !strings.Contains(result, "timeout_seconds must be positive") {
		t.Fatalf("unexpected result: %s", result)
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
