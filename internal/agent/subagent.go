package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

type subagentModule struct {
	rt *agentRuntime
}

// ID 返回子 agent 模块标识。
func (m *subagentModule) ID() string {
	return "subagent"
}

// Init 子 agent 模块没有额外初始化状态；runtime 在构造时显式注入。
func (m *subagentModule) Init(ctx ModuleContext) error {
	return nil
}

// ToolDefinitions 注册启动子 agent 的 task 工具。
func (m *subagentModule) ToolDefinitions() []anthropic.ToolParam {
	return subagentToolDefinitions()
}

// ToolHandlers 绑定 task 工具到当前 runtime。
func (m *subagentModule) ToolHandlers() map[string]ToolHandler {
	if m.rt == nil {
		return map[string]ToolHandler{}
	}
	return m.rt.subagentToolHandlers()
}

// RuntimeSnapshot 暴露子 agent 的固定能力边界。
func (m *subagentModule) RuntimeSnapshot() any {
	if m.rt == nil {
		return nil
	}
	return map[string]any{
		"tool":              "task",
		"subagentTools":     m.rt.subagentSpec().ToolNames,
		"subagentMaxTurns":  m.rt.subagentSpec().MaxTurns,
		"subagentMaxTokens": m.rt.subagentSpec().MaxTokens,
	}
}

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

	client := newAnthropicClient(rt.config)
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
