package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/joho/godotenv"
)

// ── 压缩阈值 ──────────────────────────────────────────

const (
	contextLimit     = 50_000 // 触发自动摘要压缩的粗略上下文阈值（字符数）
	keepRecent       = 3      // L2 压缩时保留最近几个 tool_result 的完整内容
	persistThreshold = 30_000 // 单个工具结果超过此阈值时落盘
	maxBudget        = 200_000
)

// ── 辅助函数 ──────────────────────────────────────────

// estimateSize 粗略估算消息列表占用的上下文大小
func estimateSize(messages []anthropic.MessageParam) int {
	data, _ := json.Marshal(messages)
	return len(data)
}

// safeFilename 将 tool_use_id 转换为安全文件名
func safeFilename(value string) string {
	var sb strings.Builder
	for _, ch := range value {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_' {
			sb.WriteRune(ch)
		} else {
			sb.WriteRune('_')
		}
	}
	if sb.Len() == 0 {
		return "unknown"
	}
	return sb.String()
}

// ── 消息类型判断 ──────────────────────────────────────

// messageHasToolUse 判断 assistant 消息是否包含 tool_use block
func messageHasToolUse(m anthropic.MessageParam) bool {
	if m.Role != "assistant" {
		return false
	}
	for _, b := range m.Content {
		if b.OfToolUse != nil {
			return true
		}
	}
	return false
}

// isToolResultMessage 判断 user 消息是否包含 tool_result block
func isToolResultMessage(m anthropic.MessageParam) bool {
	if m.Role != "user" {
		return false
	}
	for _, b := range m.Content {
		if b.OfToolResult != nil {
			return true
		}
	}
	return false
}

// ── L1: snipCompact — 裁剪中间消息 ──────────────────

func snipCompact(messages []anthropic.MessageParam, maxMessages int) []anthropic.MessageParam {
	if maxMessages <= 0 {
		maxMessages = 50
	}
	if len(messages) <= maxMessages {
		return messages
	}

	keepHead := 3
	keepTail := maxMessages - keepHead
	headEnd := keepHead
	tailStart := len(messages) - keepTail

	// 如果头部最后一条是 tool_use，把紧随其后的 tool_result 也保留
	if headEnd > 0 && messageHasToolUse(messages[headEnd-1]) {
		for headEnd < len(messages) && isToolResultMessage(messages[headEnd]) {
			headEnd++
		}
	}

	// 如果尾部第一条是 tool_result，把它前面的 tool_use 也保留
	if tailStart > 0 && tailStart < len(messages) &&
		isToolResultMessage(messages[tailStart]) &&
		tailStart-1 >= 0 && messageHasToolUse(messages[tailStart-1]) {
		tailStart--
	}

	if headEnd >= tailStart {
		return messages
	}

	snipped := tailStart - headEnd
	result := make([]anthropic.MessageParam, 0, headEnd+1+len(messages)-tailStart)
	result = append(result, messages[:headEnd]...)
	result = append(result, anthropic.NewUserMessage(
		anthropic.NewTextBlock(fmt.Sprintf("[snipped %d messages]", snipped)),
	))
	result = append(result, messages[tailStart:]...)
	return result
}

// ── L2: microCompact — 压缩旧 tool_result ──────────

func microCompact(messages []anthropic.MessageParam) []anthropic.MessageParam {
	// 收集所有 tool_result block 的位置
	type toolResultRef struct {
		msgIdx   int
		blockIdx int
	}
	var refs []toolResultRef

	for i, m := range messages {
		if m.Role != "user" {
			continue
		}
		for j, b := range m.Content {
			if b.OfToolResult != nil {
				refs = append(refs, toolResultRef{i, j})
			}
		}
	}

	if len(refs) <= keepRecent {
		return messages
	}

	// 压缩较旧的 tool_result（保留最近 keepRecent 个）
	for _, ref := range refs[:len(refs)-keepRecent] {
		block := messages[ref.msgIdx].Content[ref.blockIdx].OfToolResult
		if block == nil {
			continue
		}
		content, _ := json.Marshal(block.Content)
		if len(content) > 120 {
			block.Content = []anthropic.ToolResultBlockParamContentUnion{
				{OfText: &anthropic.TextBlockParam{Text: "[Earlier tool result compacted. Re-run if needed.]"}},
			}
		}
	}
	return messages
}

// ── L3: toolResultBudget — 大结果落盘 ────────────────

// persistLargeOutput 将超大工具输出写入文件，返回包含路径和预览的轻量内容
func (rt *agentRuntime) persistLargeOutput(toolUseID string, output string) string {
	if len(output) <= persistThreshold {
		return output
	}

	os.MkdirAll(rt.config.ToolResultsDir, 0o755)
	path := filepath.Join(rt.config.ToolResultsDir, safeFilename(toolUseID)+".txt")

	// 避免重复写入
	if _, err := os.Stat(path); os.IsNotExist(err) {
		os.WriteFile(path, []byte(output), 0o644)
	}

	preview := output
	if len(preview) > 2000 {
		preview = preview[:2000]
	}

	return fmt.Sprintf("<persisted-output>\nFull output: %s\nPreview:\n%s\n</persisted-output>", path, preview)
}

func (rt *agentRuntime) toolResultBudget(messages []anthropic.MessageParam) []anthropic.MessageParam {
	if len(messages) == 0 {
		return messages
	}
	last := &messages[len(messages)-1]
	if last.Role != "user" {
		return messages
	}

	// 处理单个超大结果
	for i := range last.Content {
		block := last.Content[i].OfToolResult
		if block == nil {
			continue
		}
		content, _ := json.Marshal(block.Content)
		contentStr := string(content)
		if len(contentStr) > persistThreshold {
			toolUseID := block.ToolUseID
			persisted := rt.persistLargeOutput(toolUseID, contentStr)
			block.Content = []anthropic.ToolResultBlockParamContentUnion{
				{OfText: &anthropic.TextBlockParam{Text: persisted}},
			}
		}
	}

	return messages
}

// ── 轻量压缩组合 ──────────────────────────────────────

func (rt *agentRuntime) applyLightweightCompaction(messages []anthropic.MessageParam) []anthropic.MessageParam {
	messages = rt.toolResultBudget(messages)
	messages = snipCompact(messages, 50)
	messages = microCompact(messages)
	return messages
}

// ── L4: autoCompact — LLM 摘要压缩 ──────────────────

// writeTranscript 将对话保存为 JSONL 文件
func (rt *agentRuntime) writeTranscript(messages []anthropic.MessageParam) string {
	os.MkdirAll(rt.config.TranscriptDir, 0o755)
	path := filepath.Join(rt.config.TranscriptDir, fmt.Sprintf("transcript_%d.jsonl", time.Now().Unix()))
	f, err := os.Create(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	for _, m := range messages {
		enc.Encode(m)
	}
	return path
}

// summarizeHistory 调用模型总结历史
func (rt *agentRuntime) summarizeHistory(messages []anthropic.MessageParam) string {
	godotenv.Load()

	opts := []option.RequestOption{}
	if base := os.Getenv("ANTHROPIC_BASE_URL"); base != "" {
		opts = append(opts, option.WithBaseURL(base))
	}

	client := anthropic.NewClient(opts...)
	ctx := context.Background()

	conversation, _ := json.Marshal(messages)
	convStr := string(conversation)
	if len(convStr) > 80000 {
		convStr = convStr[:80000]
	}

	prompt := "Summarize this coding-agent conversation so work can continue.\n" +
		"Preserve: 1. current goal, 2. key findings/decisions, 3. files read/changed, " +
		"4. remaining work, 5. user constraints.\nBe compact but concrete.\n\n" + convStr

	resp, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(rt.config.Model),
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(prompt))},
		MaxTokens: 2000,
	})
	if err != nil {
		return "(summary failed: " + err.Error() + ")"
	}

	var parts []string
	for _, block := range resp.Content {
		if tb, ok := block.AsAny().(anthropic.TextBlock); ok {
			parts = append(parts, tb.Text)
		}
	}
	summary := strings.Join(parts, "\n")
	if summary == "" {
		return "(empty summary)"
	}
	return summary
}

// compactHistory 保存 transcript 后压成一条 summary 消息
func (rt *agentRuntime) compactHistory(messages []anthropic.MessageParam) []anthropic.MessageParam {
	path := rt.writeTranscript(messages)
	if path != "" {
		rt.emitLine("[transcript saved: %s]", path)
	}
	summary := rt.summarizeHistory(messages)
	return []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock("[Compacted]\n\n" + summary)),
	}
}

// maybeCompactHistory 先做轻量压缩，仍超限则升级到 LLM 摘要
func (rt *agentRuntime) maybeCompactHistory(messages []anthropic.MessageParam) []anthropic.MessageParam {
	messages = rt.applyLightweightCompaction(messages)
	if estimateSize(messages) <= contextLimit {
		return messages
	}
	return rt.compactHistory(messages)
}

// ── Emergency: reactiveCompact ──────────────────────

// reactiveCompact API 返回 400 时的紧急压缩
func (rt *agentRuntime) reactiveCompact(messages []anthropic.MessageParam) []anthropic.MessageParam {
	path := rt.writeTranscript(messages)
	if path != "" {
		rt.emitLine("[reactive transcript saved: %s]", path)
	}
	summary := rt.summarizeHistory(messages)

	tailStart := len(messages) - 5
	if tailStart < 0 {
		tailStart = 0
	}

	// 保持 tool_use/tool_result 配对完整
	if tailStart > 0 && tailStart < len(messages) &&
		isToolResultMessage(messages[tailStart]) &&
		tailStart-1 >= 0 && messageHasToolUse(messages[tailStart-1]) {
		tailStart--
	}

	result := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock("[Reactive compact]\n\n" + summary)),
	}
	result = append(result, messages[tailStart:]...)
	return result
}
