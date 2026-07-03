package agent

// 设计说明：
// agent 和 subagent 本质上都是“带配置的执行体”：输入一组消息、拿 system prompt、
// 调模型、执行允许的工具，再把 tool_result 送回模型。二者不应该各自维护一套循环。
//
// 这里用 agentSpec 保存差异：
//   - ToolNames 决定能看见和调用哪些工具。
//   - MaxTurns/UseRecovery 决定生命周期和错误恢复策略。
//   - 若干布尔开关决定是否接入记忆、后台通知、todo reminder、Stop hook 等主 agent 能力。
//
// 后续如果要做“不同系统提示词、不同工具集、不同模块”的按需加载，可以继续扩展
// agentSpec，而不是复制 agent loop。

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

type agentSpec struct {
	ID                            string
	DisplayName                   string
	ToolNames                     []string
	MaxTurns                      int
	MaxTokens                     int64
	UseRecovery                   bool
	InjectRelevantMemories        bool
	InjectBackgroundNotifications bool
	UseTodoReminder               bool
	UseCompaction                 bool
	UseStopHooks                  bool
	ExtractMemoriesOnStop         bool
	UnknownToolResultIsError      bool
	ToolLogPrefix                 string
	ToolPreviewLimit              int
}

type agentRunResult struct {
	FinalText string
	Turns     int
	StoppedBy string
	Error     string
}

func (rt *agentRuntime) mainAgentSpec() agentSpec {
	return agentSpec{
		ID:                            "main",
		DisplayName:                   "Agent",
		ToolNames:                     toolNames(buildTools()),
		UseRecovery:                   true,
		InjectRelevantMemories:        true,
		InjectBackgroundNotifications: true,
		UseTodoReminder:               true,
		UseCompaction:                 true,
		UseStopHooks:                  true,
		ExtractMemoriesOnStop:         true,
		ToolPreviewLimit:              200,
	}
}

func (rt *agentRuntime) subagentSpec() agentSpec {
	return agentSpec{
		ID:                       "subagent",
		DisplayName:              "Subagent",
		ToolNames:                []string{"bash", "read_file", "write_file", "edit_file", "glob"},
		MaxTurns:                 30,
		MaxTokens:                rt.config.DefaultTokens,
		UnknownToolResultIsError: true,
		ToolLogPrefix:            colorDim("  │ "),
		ToolPreviewLimit:         100,
	}
}

func (rt *agentRuntime) runAgent(ctx context.Context, client anthropic.Client, spec agentSpec, messages *[]anthropic.MessageParam) agentRunResult {
	tools := filterToolsByName(buildTools(), spec.ToolNames)
	names := toolNames(tools)
	handlers := filterToolHandlers(rt.toolHandlers(), names)

	if spec.InjectRelevantMemories {
		if query := latestUserText(*messages); query != "" {
			rt.injectRelevantMemories(messages, query)
		}
	}

	for turn := 0; spec.MaxTurns <= 0 || turn < spec.MaxTurns; turn++ {
		if spec.UseCompaction {
			*messages = rt.maybeCompactHistory(*messages)
		}
		if spec.InjectBackgroundNotifications {
			rt.injectBackgroundNotifications(messages)
		}
		if spec.UseTodoReminder && rt.roundsSinceTodo >= 3 && len(*messages) > 0 {
			*messages = append(*messages, anthropic.NewUserMessage(
				anthropic.NewTextBlock("<reminder>Update your todos.</reminder>"),
			))
			rt.roundsSinceTodo = 0
		}

		resp, err := rt.callAgentModel(ctx, client, spec, tools, names, messages)
		if err != nil {
			errText := fmt.Sprintf("%s error: %v", spec.DisplayName, err)
			rt.emitLine("%s", colorRed(errText))
			return agentRunResult{Turns: turn, StoppedBy: "error", Error: errText}
		}

		*messages = append(*messages, resp.ToParam())

		if spec.UseRecovery && resp.StopReason == anthropic.StopReasonMaxTokens && rt.continueAfterMaxTokens(messages) {
			continue
		}

		if resp.StopReason != anthropic.StopReasonToolUse {
			if spec.UseStopHooks {
				forceContinue := rt.triggerHooks(EventStop, *messages)
				if forceContinue != nil {
					*messages = append(*messages, anthropic.NewUserMessage(
						anthropic.NewTextBlock(*forceContinue),
					))
					continue
				}
			}
			if spec.ExtractMemoriesOnStop {
				rt.extractMemories(ctx, client, *messages)
			}
			return agentRunResult{FinalText: latestAssistantText(*messages), Turns: turn + 1, StoppedBy: string(resp.StopReason)}
		}

		if spec.UseTodoReminder {
			rt.roundsSinceTodo++
		}

		toolResults := rt.runAgentTools(resp, handlers, spec)
		*messages = append(*messages, anthropic.NewUserMessage(toolResults...))
		if spec.UseCompaction {
			*messages = rt.maybeCompactHistory(*messages)
		}
	}

	return agentRunResult{FinalText: latestAssistantText(*messages), Turns: spec.MaxTurns, StoppedBy: "max_turns"}
}

func (rt *agentRuntime) callAgentModel(ctx context.Context, client anthropic.Client, spec agentSpec, tools []anthropic.ToolUnionParam, names []string, messages *[]anthropic.MessageParam) (*anthropic.Message, error) {
	params := anthropic.MessageNewParams{
		System:   []anthropic.TextBlockParam{{Text: rt.getSystemPrompt(names)}},
		Tools:    tools,
		Messages: *messages,
	}
	if spec.UseRecovery {
		return rt.callModelWithRecovery(ctx, client, params, messages)
	}
	params.Model = anthropic.Model(rt.config.Model)
	params.MaxTokens = spec.MaxTokens
	if params.MaxTokens == 0 {
		params.MaxTokens = rt.config.DefaultTokens
	}
	return client.Messages.New(ctx, params)
}

func (rt *agentRuntime) runAgentTools(resp *anthropic.Message, handlers map[string]ToolHandler, spec agentSpec) []anthropic.ContentBlockParamUnion {
	var toolResults []anthropic.ContentBlockParamUnion
	for _, block := range resp.Content {
		tb, ok := block.AsAny().(anthropic.ToolUseBlock)
		if !ok {
			continue
		}

		if denied := rt.triggerHooks(EventPreToolUse, tb); denied != nil {
			toolResults = append(toolResults, anthropic.NewToolResultBlock(tb.ID, *denied, true))
			continue
		}

		handler, exists := handlers[tb.Name]
		if !exists {
			rt.emitLine("%s Unknown tool: %s", colorYellow("WARNING"), tb.Name)
			if spec.UnknownToolResultIsError {
				toolResults = append(toolResults, anthropic.NewToolResultBlock(tb.ID, fmt.Sprintf("Unknown: %s", tb.Name), true))
			}
			continue
		}

		inputJSON, _ := json.Marshal(tb.Input)
		output := handler(inputJSON)
		rt.triggerHooks(EventPostToolUse, tb, output)

		if tb.Name == "todo_write" {
			rt.roundsSinceTodo = 0
		}
		rt.logAgentToolOutput(tb.Name, output, spec)

		toolResults = append(toolResults, anthropic.NewToolResultBlock(tb.ID, output, false))
	}
	return toolResults
}

func (rt *agentRuntime) logAgentToolOutput(name string, output string, spec agentSpec) {
	limit := spec.ToolPreviewLimit
	if limit <= 0 {
		limit = 200
	}
	preview := strings.ReplaceAll(truncate(output, limit), "\n", " ")
	if spec.ToolLogPrefix != "" {
		rt.emitLine("%s%s: %s", spec.ToolLogPrefix, colorDim(name), preview)
		return
	}
	rt.emitLine("%s", colorDim("Tool Output: "+truncate(output, limit)))
}

func filterToolHandlers(handlers map[string]ToolHandler, names []string) map[string]ToolHandler {
	allowed := make(map[string]struct{}, len(names))
	for _, name := range names {
		allowed[name] = struct{}{}
	}
	filtered := make(map[string]ToolHandler, len(names))
	for name, handler := range handlers {
		if _, ok := allowed[name]; ok {
			filtered[name] = handler
		}
	}
	return filtered
}

func filterToolsByName(tools []anthropic.ToolUnionParam, names []string) []anthropic.ToolUnionParam {
	allowed := make(map[string]struct{}, len(names))
	for _, name := range names {
		allowed[name] = struct{}{}
	}
	filtered := make([]anthropic.ToolUnionParam, 0, len(names))
	for _, tool := range tools {
		if tool.OfTool == nil {
			continue
		}
		if _, ok := allowed[tool.OfTool.Name]; ok {
			filtered = append(filtered, tool)
		}
	}
	return filtered
}

func latestAssistantText(messages []anthropic.MessageParam) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "assistant" {
			continue
		}
		if text := extractText(messages[i]); text != "" {
			return text
		}
	}
	return ""
}
