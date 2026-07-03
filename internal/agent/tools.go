package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

// ToolHandler 统一的工具处理函数签名，接收原始 JSON 输入，返回字符串结果
type ToolHandler func(input json.RawMessage) string

// toolHandlers 返回当前运行时绑定的工具处理函数。
func (rt *agentRuntime) toolHandlers() map[string]ToolHandler {
	handlers := map[string]ToolHandler{
		"bash":              rt.runBash,
		"read_file":         rt.runRead,
		"write_file":        rt.runWrite,
		"edit_file":         rt.runEdit,
		"glob":              rt.runGlob,
		"task":              rt.spawnSubagent,
		"load_skill":        rt.loadSkill,
		"task_create":       rt.runTaskCreate,
		"task_list":         rt.runTaskList,
		"task_get":          rt.runTaskGet,
		"task_claim":        rt.runTaskClaim,
		"task_complete":     rt.runTaskComplete,
		"background_bash":   rt.runBackgroundBash,
		"background_status": rt.runBackgroundStatus,
		"background_list":   rt.runBackgroundList,
	}
	if rt.modules != nil {
		for name, handler := range rt.modules.toolHandlers() {
			handlers[name] = handler
		}
	}
	return handlers
}

// ── 工具实现 ──────────────────────────────────────────

func (rt *agentRuntime) runBash(raw json.RawMessage) string {
	var input struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	cmd := exec.Command("bash", "-c", input.Command)
	cmd.Dir = rt.config.Workdir
	out, err := cmd.CombinedOutput()
	result := strings.TrimSpace(string(out))

	if err != nil {
		if result == "" {
			return fmt.Sprintf("Error: %v", err)
		}
		return result
	}
	if result == "" {
		return "(no output)"
	}
	// 截断过长输出
	if len(result) > 50000 {
		return result[:50000]
	}
	return result
}

func (rt *agentRuntime) runRead(raw json.RawMessage) string {
	var input struct {
		Path  string `json:"path"`
		Limit *int   `json:"limit,omitempty"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	p, err := rt.safePath(input.Path)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	data, err := os.ReadFile(p)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	lines := strings.Split(string(data), "\n")
	if input.Limit != nil && *input.Limit > 0 && *input.Limit < len(lines) {
		lines = lines[:*input.Limit]
	}
	return strings.Join(lines, "\n")
}

func (rt *agentRuntime) runWrite(raw json.RawMessage) string {
	var input struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	p, err := rt.safePath(input.Path)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	// 确保父目录存在
	os.MkdirAll(filepath.Dir(p), 0o755)

	if err := os.WriteFile(p, []byte(input.Content), 0o644); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return fmt.Sprintf("Wrote %d bytes to %s", len(input.Content), input.Path)
}

func (rt *agentRuntime) runEdit(raw json.RawMessage) string {
	var input struct {
		Path    string `json:"path"`
		OldText string `json:"old_text"`
		NewText string `json:"new_text"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	p, err := rt.safePath(input.Path)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	data, err := os.ReadFile(p)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	text := string(data)
	if !strings.Contains(text, input.OldText) {
		return "Error: text not found"
	}

	newText := strings.Replace(text, input.OldText, input.NewText, 1)
	if err := os.WriteFile(p, []byte(newText), 0o644); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return fmt.Sprintf("Edited %s", input.Path)
}

func (rt *agentRuntime) runGlob(raw json.RawMessage) string {
	var input struct {
		Pattern string `json:"pattern"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	pattern := filepath.Join(rt.config.Workdir, input.Pattern)
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	// 过滤并转为相对路径
	var results []string
	for _, m := range matches {
		rel, err := filepath.Rel(rt.config.Workdir, m)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		results = append(results, rel)
	}

	if len(results) == 0 {
		return "(no matches)"
	}
	return strings.Join(results, "\n")
}

// ── 工具 Schema 定义 ──────────────────────────────────

func (rt *agentRuntime) buildTools() []anthropic.ToolUnionParam {
	toolParams := buildBaseToolParams()
	if rt != nil && rt.modules != nil {
		toolParams = append(toolParams, rt.modules.toolDefinitions()...)
	}
	tools := make([]anthropic.ToolUnionParam, len(toolParams))
	for i, tp := range toolParams {
		tools[i] = anthropic.ToolUnionParam{OfTool: &tp}
	}
	return tools
}

func buildBaseToolParams() []anthropic.ToolParam {
	toolParams := []anthropic.ToolParam{
		{
			Name:        "bash",
			Description: anthropic.String("Run a shell command."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]any{
					"command": map[string]any{"type": "string"},
				},
				Required: []string{"command"},
			},
		},
		{
			Name:        "read_file",
			Description: anthropic.String("Read file contents."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]any{
					"path":  map[string]any{"type": "string"},
					"limit": map[string]any{"type": "integer"},
				},
				Required: []string{"path"},
			},
		},
		{
			Name:        "write_file",
			Description: anthropic.String("Write content to a file."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]any{
					"path":    map[string]any{"type": "string"},
					"content": map[string]any{"type": "string"},
				},
				Required: []string{"path", "content"},
			},
		},
		{
			Name:        "edit_file",
			Description: anthropic.String("Replace exact text in a file once."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]any{
					"path":     map[string]any{"type": "string"},
					"old_text": map[string]any{"type": "string"},
					"new_text": map[string]any{"type": "string"},
				},
				Required: []string{"path", "old_text", "new_text"},
			},
		},
		{
			Name:        "glob",
			Description: anthropic.String("Find files matching a glob pattern."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]any{
					"pattern": map[string]any{"type": "string"},
				},
				Required: []string{"pattern"},
			},
		},
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
		{
			Name:        "load_skill",
			Description: anthropic.String("Load the full instructions for an available skill by name."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]any{
					"name": map[string]any{"type": "string"},
				},
				Required: []string{"name"},
			},
		},
		{
			Name:        "task_create",
			Description: anthropic.String("Create a persistent JSON task in .agents/.tasks/. Use blockedBy for dependency IDs."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]any{
					"subject":     map[string]any{"type": "string"},
					"description": map[string]any{"type": "string"},
					"blockedBy": map[string]any{
						"type":  "array",
						"items": map[string]any{"type": "string"},
					},
				},
				Required: []string{"subject"},
			},
		},
		{
			Name:        "task_list",
			Description: anthropic.String("List persistent tasks from .agents/.tasks/TASKS.md."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]any{},
			},
		},
		{
			Name:        "task_get",
			Description: anthropic.String("Get full task JSON by id."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]any{
					"id": map[string]any{"type": "string"},
				},
				Required: []string{"id"},
			},
		},
		{
			Name:        "task_claim",
			Description: anthropic.String("Claim a pending task when all blockedBy dependencies are completed."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]any{
					"id":    map[string]any{"type": "string"},
					"owner": map[string]any{"type": "string"},
				},
				Required: []string{"id"},
			},
		},
		{
			Name:        "task_complete",
			Description: anthropic.String("Mark a task completed and report newly unblocked pending tasks."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]any{
					"id": map[string]any{"type": "string"},
				},
				Required: []string{"id"},
			},
		},
		{
			Name:        "background_bash",
			Description: anthropic.String("Run a shell command in the background and return a job id."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]any{
					"command": map[string]any{"type": "string"},
				},
				Required: []string{"command"},
			},
		},
		{
			Name:        "background_status",
			Description: anthropic.String("Inspect a background job by id."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]any{
					"id": map[string]any{"type": "string"},
				},
				Required: []string{"id"},
			},
		},
		{
			Name:        "background_list",
			Description: anthropic.String("List background shell jobs."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]any{},
			},
		},
	}

	return toolParams
}

func toolNames(tools []anthropic.ToolUnionParam) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		if tool.OfTool != nil {
			names = append(names, tool.OfTool.Name)
		}
	}
	return names
}
