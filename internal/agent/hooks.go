package agent

import (
	"encoding/json"

	"github.com/anthropics/anthropic-sdk-go"
)

// HookEvent 事件类型
type HookEvent string

const (
	EventUserPromptSubmit HookEvent = "UserPromptSubmit"
	EventPreToolUse       HookEvent = "PreToolUse"
	EventPostToolUse      HookEvent = "PostToolUse"
	EventStop             HookEvent = "Stop"
)

// HookCallback 钩子回调函数，返回 nil 表示继续，返回 *string 表示中断
type HookCallback func(args ...any) *string

// registerHook 注册钩子到指定事件
func (rt *agentRuntime) registerHook(event HookEvent, cb HookCallback) {
	rt.hooks[event] = append(rt.hooks[event], cb)
}

// triggerHooks 触发事件的所有钩子，任一钩子返回非 nil 则中断并返回该值
func (rt *agentRuntime) triggerHooks(event HookEvent, args ...any) *string {
	for _, cb := range rt.hooks[event] {
		if result := cb(args...); result != nil {
			return result
		}
	}
	return nil
}

// ── 默认钩子 ──────────────────────────────────────────

// logHook 记录每次工具调用（PreToolUse）
func (rt *agentRuntime) logHook(args ...any) *string {
	block, ok := args[0].(anthropic.ToolUseBlock)
	if !ok {
		return nil
	}
	inputJSON, _ := json.Marshal(block.Input)
	preview := truncate(string(inputJSON), 60)
	rt.emitLine("[HOOK] Used tool %s: (%s)", block.Name, preview)
	return nil
}

// largeOutputHook 警告过大的工具输出（PostToolUse）
func (rt *agentRuntime) largeOutputHook(args ...any) *string {
	if len(args) < 2 {
		return nil
	}
	block, ok := args[0].(anthropic.ToolUseBlock)
	if !ok {
		return nil
	}
	output, ok := args[1].(string)
	if !ok {
		return nil
	}
	if len(output) > 100000 {
		rt.emitLine("%s [HOOK] ⚠ Large output from %s: %d chars", colorYellow("WARNING"), block.Name, len(output))
	}
	return nil
}

// summaryHook 在 agent 循环结束时打印工具调用统计（Stop）
func (rt *agentRuntime) summaryHook(args ...any) *string {
	if len(args) < 1 {
		return nil
	}
	messages, ok := args[0].([]anthropic.MessageParam)
	if !ok {
		return nil
	}

	toolCount := 0
	for _, m := range messages {
		for _, block := range m.Content {
			if block.OfToolResult != nil {
				toolCount++
			}
		}
	}
	rt.emitLine("[HOOK] Stop: session used %d tool calls", toolCount)
	return nil
}

// initHooks 注册所有默认钩子
func (rt *agentRuntime) initHooks() {
	rt.registerHook(EventPreToolUse, rt.permissionHook)
	rt.registerHook(EventPreToolUse, rt.logHook)
	rt.registerHook(EventPostToolUse, rt.largeOutputHook)
	rt.registerHook(EventStop, rt.summaryHook)
}
