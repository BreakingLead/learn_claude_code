package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Todo 表示一个任务项
type Todo struct {
	Content string `json:"content"`
	Status  string `json:"status"` // "pending" | "in_progress" | "completed"
}

// currentTodos 全局任务列表
var currentTodos []Todo

// runTodoWrite 更新并展示任务列表
func runTodoWrite(raw json.RawMessage) string {
	// 解析 input: { "todos": [...] }
	var input struct {
		Todos []Todo `json:"todos"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	currentTodos = input.Todos

	// 生成展示文本
	var lines []string
	lines = append(lines, "\n## Current Tasks")
	for _, t := range currentTodos {
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
	fmt.Println(strings.Join(lines, "\n"))

	return fmt.Sprintf("Updated %d tasks", len(currentTodos))
}
