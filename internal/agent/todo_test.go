package agent

import (
	"context"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

func TestTodoModuleInjectsReminderAfterThreeToolTurns(t *testing.T) {
	module := &todoModule{}
	for range 3 {
		module.AfterToolRound(context.Background(), ToolRoundEvent{AgentID: "main", ToolNames: []string{"bash", "read_file"}})
	}

	messages := module.BeforeModel(context.Background(), TurnRequest{
		AgentID:   "main",
		ToolNames: []string{"todo_write"},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("work")),
		},
	})
	if len(messages) != 1 {
		t.Fatalf("expected reminder message, got %d", len(messages))
	}
	if got := extractResponseText(messages[0]); got != "<reminder>Update your todos.</reminder>" {
		t.Fatalf("unexpected reminder: %q", got)
	}
}

func TestTodoModuleDoesNotIncrementWhenTodoWriteRuns(t *testing.T) {
	module := &todoModule{roundsSinceTodo: 2}
	module.AfterToolRound(context.Background(), ToolRoundEvent{AgentID: "main", ToolNames: []string{"bash", "todo_write"}})
	if module.roundsSinceTodo != 2 {
		t.Fatalf("expected todo_write round not to increment reminder count, got %d", module.roundsSinceTodo)
	}
	module.AfterToolUse(context.Background(), ToolUseEvent{AgentID: "main", Name: "todo_write"})
	if module.roundsSinceTodo != 0 {
		t.Fatalf("expected todo_write to reset reminder count, got %d", module.roundsSinceTodo)
	}
}

func TestTodoModuleSkipsReminderWhenToolUnavailable(t *testing.T) {
	module := &todoModule{roundsSinceTodo: 3}
	messages := module.BeforeModel(context.Background(), TurnRequest{
		AgentID:   "subagent",
		ToolNames: []string{"bash"},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("work")),
		},
	})
	if len(messages) != 0 {
		t.Fatalf("expected no reminder without todo_write, got %d", len(messages))
	}
}
