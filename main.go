package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/joho/godotenv"
)

// roundsSinceTodo 追踪距离上次 todo 更新过了多少轮
var roundsSinceTodo int

// agentLoop 核心 agent 循环：调用 API → 处理 tool_use → 发送 tool_result → 循环
func agentLoop(ctx context.Context, client anthropic.Client, messages *[]anthropic.MessageParam) {
	tools := buildTools()
	systemPrompt := getSystemPrompt()

	for {
		*messages = maybeCompactHistory(*messages)

		// nag reminder：如果 3 轮没更新 todo，注入提醒
		if roundsSinceTodo >= 3 && len(*messages) > 0 {
			*messages = append(*messages, anthropic.NewUserMessage(
				anthropic.NewTextBlock("<reminder>Update your todos.</reminder>"),
			))
			roundsSinceTodo = 0
		}

		resp, err := client.Messages.New(ctx, anthropic.MessageNewParams{
			Model:     anthropic.Model(MODEL),
			MaxTokens: 8000,
			System:    []anthropic.TextBlockParam{{Text: systemPrompt}},
			Tools:     tools,
			Messages:  *messages,
		})

		if err != nil {
			// 检查是否为 400 错误（上下文超限），尝试紧急压缩后重试
			if apiErr, ok := err.(*anthropic.Error); ok && apiErr.StatusCode == http.StatusBadRequest {
				fmt.Println(colorYellow("[context overflow, applying reactive compact...]"))
				*messages = reactiveCompact(*messages)
				resp, err = client.Messages.New(ctx, anthropic.MessageNewParams{
					Model:     anthropic.Model(MODEL),
					MaxTokens: 8000,
					System:    []anthropic.TextBlockParam{{Text: systemPrompt}},
					Tools:     tools,
					Messages:  *messages,
				})
				if err != nil {
					fmt.Println(colorRed("Error: " + err.Error()))
					return
				}
			} else {
				fmt.Println(colorRed("Error: " + err.Error()))
				return
			}
		}

		// 将 assistant 回复追加到历史
		*messages = append(*messages, resp.ToParam())

		// 非 tool_use 停止原因 → 触发 Stop 钩子，结束循环
		if resp.StopReason != anthropic.StopReasonToolUse {
			forceContinue := triggerHooks(EventStop, *messages)
			if forceContinue != nil {
				*messages = append(*messages, anthropic.NewUserMessage(
					anthropic.NewTextBlock(*forceContinue),
				))
				continue
			}
			return
		}

		roundsSinceTodo++

		// 处理工具调用
		var toolResults []anthropic.ContentBlockParamUnion

		for _, block := range resp.Content {
			tb, ok := block.AsAny().(anthropic.ToolUseBlock)
			if !ok {
				continue
			}

			// PreToolUse 钩子（权限检查）
			if denied := triggerHooks(EventPreToolUse, tb); denied != nil {
				toolResults = append(toolResults,
					anthropic.NewToolResultBlock(tb.ID, *denied, true),
				)
				continue
			}

			handler, exists := TOOL_HANDLERS[tb.Name]
			if !exists {
				fmt.Printf("%s Unknown tool: %s\n", colorYellow("WARNING"), tb.Name)
				continue
			}

			inputJSON, _ := json.Marshal(tb.Input)
			output := handler(inputJSON)
			fmt.Println(colorDim("Tool Output: " + truncate(output, 200)))

			// PostToolUse 钩子
			triggerHooks(EventPostToolUse, tb, output)

			// 重置 todo 计数器
			if tb.Name == "todo_write" {
				roundsSinceTodo = 0
			}

			toolResults = append(toolResults,
				anthropic.NewToolResultBlock(tb.ID, output, false),
			)
		}

		*messages = append(*messages, anthropic.NewUserMessage(toolResults...))
		*messages = maybeCompactHistory(*messages)
	}
}

// ── REPL 入口 ──────────────────────────────────────────

func main() {
	godotenv.Load()

	// 注册默认钩子
	initHooks()

	// 创建 Anthropic 客户端
	opts := []option.RequestOption{}
	if base := os.Getenv("ANTHROPIC_BASE_URL"); base != "" {
		opts = append(opts, option.WithBaseURL(base))
	}
	client := anthropic.NewClient(opts...)
	ctx := context.Background()

	fmt.Println(colorBold("go_agent: coding agent (Go + Anthropic SDK)"))
	fmt.Println("输入问题，回车发送。输入 q 退出。")
	fmt.Println()

	var history []anthropic.MessageParam
	scanner := bufio.NewScanner(os.Stdin)

	for {
		fmt.Print(colorCyan("go_agent >> "))
		if !scanner.Scan() {
			break // EOF / Ctrl+D
		}
		query := strings.TrimSpace(scanner.Text())
		if query == "" || query == "q" || query == "exit" {
			break
		}

		triggerHooks(EventUserPromptSubmit, query)

		history = append(history, anthropic.NewUserMessage(
			anthropic.NewTextBlock(query),
		))

		agentLoop(ctx, client, &history)

		// 打印最终回复
		if len(history) > 0 {
			last := history[len(history)-1]
			if last.Role == "assistant" {
				for _, b := range last.Content {
					if b.OfThinking != nil {
						fmt.Printf("\033[4mThinking: %s\033[0m\n\n", b.OfThinking.Thinking)
					} else if b.OfText != nil {
						fmt.Println(b.OfText.Text)
					}
				}
			}
		}
		fmt.Println()
	}
}
