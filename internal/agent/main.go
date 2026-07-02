package agent

import (
	"context"
	"encoding/json"
	"os"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/joho/godotenv"
)

// agentLoop 核心 agent 循环：调用 API → 处理 tool_use → 发送 tool_result → 循环
func (rt *agentRuntime) agentLoop(ctx context.Context, client anthropic.Client, messages *[]anthropic.MessageParam) {
	tools := buildTools()
	names := toolNames(tools)
	handlers := rt.toolHandlers()
	if query := latestUserText(*messages); query != "" {
		rt.injectRelevantMemories(messages, query)
	}

	for {
		*messages = rt.maybeCompactHistory(*messages)
		rt.injectBackgroundNotifications(messages)

		// nag reminder：如果 3 轮没更新 todo，注入提醒
		if rt.roundsSinceTodo >= 3 && len(*messages) > 0 {
			*messages = append(*messages, anthropic.NewUserMessage(
				anthropic.NewTextBlock("<reminder>Update your todos.</reminder>"),
			))
			rt.roundsSinceTodo = 0
		}

		systemPrompt := rt.getSystemPrompt(names)
		params := anthropic.MessageNewParams{
			System:   []anthropic.TextBlockParam{{Text: systemPrompt}},
			Tools:    tools,
			Messages: *messages,
		}
		resp, err := rt.callModelWithRecovery(ctx, client, params, messages)

		if err != nil {
			rt.emitLine("%s", colorRed("Error: "+err.Error()))
			return
		}

		// 将 assistant 回复追加到历史
		*messages = append(*messages, resp.ToParam())

		if resp.StopReason == anthropic.StopReasonMaxTokens && rt.continueAfterMaxTokens(messages) {
			continue
		}

		// 非 tool_use 停止原因 → 触发 Stop 钩子，结束循环
		if resp.StopReason != anthropic.StopReasonToolUse {
			forceContinue := rt.triggerHooks(EventStop, *messages)
			if forceContinue != nil {
				*messages = append(*messages, anthropic.NewUserMessage(
					anthropic.NewTextBlock(*forceContinue),
				))
				continue
			}
			rt.extractMemories(ctx, client, *messages)
			return
		}

		rt.roundsSinceTodo++

		// 处理工具调用
		var toolResults []anthropic.ContentBlockParamUnion

		for _, block := range resp.Content {
			tb, ok := block.AsAny().(anthropic.ToolUseBlock)
			if !ok {
				continue
			}

			// PreToolUse 钩子（权限检查）
			if denied := rt.triggerHooks(EventPreToolUse, tb); denied != nil {
				toolResults = append(toolResults,
					anthropic.NewToolResultBlock(tb.ID, *denied, true),
				)
				continue
			}

			handler, exists := handlers[tb.Name]
			if !exists {
				rt.emitLine("%s Unknown tool: %s", colorYellow("WARNING"), tb.Name)
				continue
			}

			inputJSON, _ := json.Marshal(tb.Input)
			output := handler(inputJSON)
			rt.emitLine("%s", colorDim("Tool Output: "+truncate(output, 200)))

			// PostToolUse 钩子
			rt.triggerHooks(EventPostToolUse, tb, output)

			// 重置 todo 计数器
			if tb.Name == "todo_write" {
				rt.roundsSinceTodo = 0
			}

			toolResults = append(toolResults,
				anthropic.NewToolResultBlock(tb.ID, output, false),
			)
		}

		*messages = append(*messages, anthropic.NewUserMessage(toolResults...))
		*messages = rt.maybeCompactHistory(*messages)
	}
}

// ── REPL 入口 ──────────────────────────────────────────

func Run() {
	godotenv.Load()

	// 创建 Anthropic 客户端
	opts := []option.RequestOption{}
	if base := os.Getenv("ANTHROPIC_BASE_URL"); base != "" {
		opts = append(opts, option.WithBaseURL(base))
	}
	client := anthropic.NewClient(opts...)
	ctx := context.Background()

	runTUI(ctx, client)
}
