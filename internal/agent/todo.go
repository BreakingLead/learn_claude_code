package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

// Todo 表示一个任务项
type Todo struct {
	Content string `json:"content"`
	Status  string `json:"status"` // "pending" | "in_progress" | "completed"
}

// todoModule 管理当前会话内 todo_write 状态和提醒注入。
type todoModule struct {
	log             func(format string, args ...any)
	currentTodos    []Todo
	roundsSinceTodo int
}

// ID 返回 todo 模块标识。
func (m *todoModule) ID() string {
	return "todo"
}

// Init 保存模块需要的日志出口。
func (m *todoModule) Init(ctx ModuleContext) error {
	m.log = ctx.Log
	return nil
}

// ToolDefinitions 注册 todo_write 工具 schema。
func (m *todoModule) ToolDefinitions() []anthropic.ToolParam {
	return []anthropic.ToolParam{
		{
			Name:        "todo_write",
			Description: anthropic.String("Create and manage a task list."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]any{
					"todos": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"content": map[string]any{"type": "string"},
								"status": map[string]any{
									"type":    "string",
									"enum":    []string{"pending", "in_progress", "completed"},
									"default": "pending",
								},
							},
							"required": []string{"content", "status"},
						},
					},
				},
			},
		},
	}
}

// ToolHandlers 注册 todo_write 的处理函数。
func (m *todoModule) ToolHandlers() map[string]ToolHandler {
	return map[string]ToolHandler{
		"todo_write": m.runTodoWrite,
	}
}

// RuntimeSnapshot 暴露当前会话 todo 状态给 Debug tab。
func (m *todoModule) RuntimeSnapshot() any {
	return map[string]any{
		"currentTodos":    m.currentTodos,
		"roundsSinceTodo": m.roundsSinceTodo,
	}
}

// BeforeModel 在长时间未更新 todo 时注入提醒；只有暴露 todo_write 的 agent 会触发。
func (m *todoModule) BeforeModel(ctx context.Context, req TurnRequest) []anthropic.MessageParam {
	if !hasString(req.ToolNames, "todo_write") || len(req.Messages) == 0 {
		return nil
	}
	if m.roundsSinceTodo < 3 {
		return nil
	}
	m.roundsSinceTodo = 0
	return []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock("<reminder>Update your todos.</reminder>")),
	}
}

// AfterToolUse 在 todo_write 执行后清零提醒计数。
func (m *todoModule) AfterToolUse(ctx context.Context, event ToolUseEvent) {
	if event.Name == "todo_write" {
		m.roundsSinceTodo = 0
	}
}

// AfterToolRound 统计主 agent 连续多少个工具轮没有更新 todo。
func (m *todoModule) AfterToolRound(ctx context.Context, event ToolRoundEvent) {
	if event.AgentID == "main" && !hasString(event.ToolNames, "todo_write") {
		m.roundsSinceTodo++
	}
}

// runTodoWrite 更新并展示任务列表。
func (m *todoModule) runTodoWrite(raw json.RawMessage) string {
	// 解析 input: { "todos": [...] }
	var input struct {
		Todos []Todo `json:"todos"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	m.currentTodos = input.Todos

	// 生成展示文本
	var lines []string
	lines = append(lines, "\n## Current Tasks")
	for _, t := range m.currentTodos {
		icon := map[string]string{
			"pending":     " ",
			"in_progress": "▸",
			"completed":   "✓",
		}[t.Status]
		if icon == "" {
			icon = "?"
		}
		lines = append(lines, fmt.Sprintf("- [%s] %s", icon, t.Content))
	}
	if m.log != nil {
		m.log("%s", strings.Join(lines, "\n"))
	}

	return fmt.Sprintf("Updated %d tasks", len(m.currentTodos))
}
