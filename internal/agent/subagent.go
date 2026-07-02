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

	// 子 agent 可用的工具处理函数
	subHandlers := map[string]ToolHandler{
		"bash":       rt.runBash,
		"read_file":  rt.runRead,
		"write_file": rt.runWrite,
		"edit_file":  rt.runEdit,
		"glob":       rt.runGlob,
	}
	// 子 agent 只暴露自己能处理的工具，避免递归 task 或后台任务进入子循环。
	allTools := buildTools()
	var subTools []anthropic.ToolUnionParam
	for _, t := range allTools {
		if t.OfTool != nil {
			if _, ok := subHandlers[t.OfTool.Name]; ok {
				subTools = append(subTools, t)
			}
		}
	}

	prefix := colorDim("  │ ")
	rt.emitLine("%s ┌── %s spawned ──", colorDim(""), colorCyan("Subagent"))

	messages := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock(input.Description)),
	}

	systemPrompt := rt.getSystemPrompt(toolNames(subTools))

	// 子 agent 循环，最多 30 轮
	for turn := 0; turn < 30; turn++ {
		resp, err := client.Messages.New(ctx, anthropic.MessageNewParams{
			Model:     anthropic.Model(rt.config.Model),
			System:    []anthropic.TextBlockParam{{Text: systemPrompt}},
			Messages:  messages,
			Tools:     subTools,
			MaxTokens: 8000,
		})
		if err != nil {
			return fmt.Sprintf("Subagent error: %v", err)
		}

		messages = append(messages, resp.ToParam())

		if resp.StopReason != anthropic.StopReasonToolUse {
			break
		}

		// 处理工具调用
		var toolResults []anthropic.ContentBlockParamUnion
		for _, block := range resp.Content {
			tb, ok := block.AsAny().(anthropic.ToolUseBlock)
			if !ok {
				continue
			}

			// 子 agent 同样受 hook 管控
			if denied := rt.triggerHooks(EventPreToolUse, tb); denied != nil {
				toolResults = append(toolResults,
					anthropic.NewToolResultBlock(tb.ID, *denied, true),
				)
				continue
			}

			handler, exists := subHandlers[tb.Name]
			if !exists {
				toolResults = append(toolResults,
					anthropic.NewToolResultBlock(tb.ID, fmt.Sprintf("Unknown: %s", tb.Name), true),
				)
				continue
			}

			inputJSON, _ := json.Marshal(tb.Input)
			output := handler(inputJSON)
			rt.triggerHooks(EventPostToolUse, tb, output)

			// 带缩进的工具调用日志
			preview := strings.ReplaceAll(truncate(output, 100), "\n", " ")
			rt.emitLine("%s%s: %s", prefix, colorDim(tb.Name), preview)

			toolResults = append(toolResults,
				anthropic.NewToolResultBlock(tb.ID, output, false),
			)
		}

		messages = append(messages, anthropic.NewUserMessage(toolResults...))
	}

	// 提取最终回复文本
	result := extractText(messages[len(messages)-1])
	if result == "" {
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Role == "assistant" {
				result = extractText(messages[i])
				if result != "" {
					break
				}
			}
		}
	}
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
