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
	ID          string
	DisplayName string
	// ToolNames 是能力边界；模型只会看到这些工具，执行时也只绑定这些 handler。
	ToolNames []string
	// MaxTurns <= 0 表示不限制轮数；subagent 用固定上限避免无限递归消耗。
	MaxTurns int
	// MaxTokens 只用于不走恢复状态机的 agent；主 agent 的 token 预算来自 recoveryState。
	MaxTokens int64
	// UseRecovery 决定是否启用 context overflow、max_tokens、rate limit 等恢复策略。
	UseRecovery bool
	// 下面这些开关把主 agent 的生命周期能力显式化，subagent 默认不继承。
	InjectRelevantMemories        bool
	InjectBackgroundNotifications bool
	UseTodoReminder               bool
	UseCompaction                 bool
	UseStopHooks                  bool
	ExtractMemoriesOnStop         bool
	// UnknownToolResultIsError 为 true 时，未知工具会返回 error tool_result 给模型。
	UnknownToolResultIsError bool
	// ToolLogPrefix/ToolPreviewLimit 只影响日志展示，不影响模型输入输出。
	ToolLogPrefix    string
	ToolPreviewLimit int
}

// agentRunResult 是一次 agent 执行结束后的轻量摘要；主 agent 主要靠 messages 返回历史。
type agentRunResult struct {
	FinalText string
	Turns     int
	StoppedBy string
	Error     string
}

// mainAgentSpec 保留完整 agent 能力：恢复、记忆、后台通知、压缩和停止钩子都启用。
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

// subagentSpec 使用同一套 runner，但只暴露基础文件/命令工具，避免递归 task 和后台任务。
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

// runAgent 是 agent/subagent 共用状态机：准备上下文、调用模型、处理工具、判断停止。
func (rt *agentRuntime) runAgent(ctx context.Context, client anthropic.Client, spec agentSpec, messages *[]anthropic.MessageParam) agentRunResult {
	tools := filterToolsByName(buildTools(), spec.ToolNames)
	names := toolNames(tools)
	handlers := filterToolHandlers(rt.toolHandlers(), names)

	// 相关记忆只在一次用户请求开始时注入，避免每个工具轮反复追加同一批记忆。
	if spec.InjectRelevantMemories {
		if query := latestUserText(*messages); query != "" {
			rt.injectRelevantMemories(messages, query)
		}
	}

	for turn := 0; spec.MaxTurns <= 0 || turn < spec.MaxTurns; turn++ {
		// 每轮模型调用前先做运行时维护：压缩历史、注入后台完成通知、提醒 todo。
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

		// max_tokens 续写属于恢复策略，subagent 不启用，避免子任务无限扩张。
		if spec.UseRecovery && resp.StopReason == anthropic.StopReasonMaxTokens && rt.continueAfterMaxTokens(messages) {
			continue
		}

		// 没有 tool_use 就进入收尾阶段；主 agent 可通过 Stop hook 强制继续。
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

		// tool_use 轮会增加 todo 提醒计数，todo_write 工具执行后会在 runAgentTools 中清零。
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

// callAgentModel 根据 spec 选择直接调用模型或走带恢复的调用路径。
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

// runAgentTools 执行本轮所有 tool_use，并统一接入权限 hook、post hook 和日志。
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

// logAgentToolOutput 根据 spec 选择主 agent 或 subagent 的工具日志样式。
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

// filterToolHandlers 按工具名过滤 handler，确保未暴露给模型的工具也不能被执行。
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

// filterToolsByName 按 spec.ToolNames 过滤 schema，这是模型实际可见的工具列表。
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

// latestAssistantText 从消息历史尾部找最后一条 assistant 文本，供 subagent 返回最终答案。
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
