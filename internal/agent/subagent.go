package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/joho/godotenv"
)

func (rt *agentRuntime) subagentToolHandlers() map[string]ToolHandler {
	return map[string]ToolHandler{
		"task": rt.spawnSubagent,
	}
}

func subagentToolDefinitions() []anthropic.ToolParam {
	return []anthropic.ToolParam{
		{
			Name:        "task",
			Description: anthropic.String("Launch a subagent to handle a complex subtask. Returns only the final conclusion."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]any{
					"description": map[string]any{"type": "string"},
				},
				Required: []string{"description"},
			},
		},
	}
}

// spawnSubagent 启动一个子 agent，独立对话历史，仅返回最终摘要
func (rt *agentRuntime) spawnSubagent(raw json.RawMessage) string {
	var input struct {
		Description string `json:"description"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	godotenv.Load()
	opts := []option.RequestOption{}
	if base := os.Getenv("ANTHROPIC_BASE_URL"); base != "" {
		opts = append(opts, option.WithBaseURL(base))
	}
	client := anthropic.NewClient(opts...)
	ctx := context.Background()

	rt.emitLine("%s ┌── %s spawned ──", colorDim(""), colorCyan("Subagent"))

	messages := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock(input.Description)),
	}

	run := rt.runAgent(ctx, client, rt.subagentSpec(), &messages)
	if run.Error != "" {
		return run.Error
	}
	result := run.FinalText
	if result == "" {
		result = "Subagent stopped after 30 turns without final answer."
	}

	rt.emitLine("%s └── %s done ──", colorDim(""), colorCyan("Subagent"))
	return result
}

// extractText 从 MessageParam 的 content blocks 中提取纯文本
func extractText(m anthropic.MessageParam) string {
	var parts []string
	for _, b := range m.Content {
		if b.OfText != nil {
			parts = append(parts, b.OfText.Text)
		}
	}
	return strings.Join(parts, "\n")
}
