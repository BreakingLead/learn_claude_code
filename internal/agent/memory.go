package agent

// 设计说明：
// 这个文件实现跨会话持久记忆，和上下文压缩不同。压缩只让当前会话继续跑；
// 记忆则把稳定的用户偏好、项目事实、决策和约束写入 .memory/，供后续会话复用。
//
// 数据布局：
//   - .memory/MEMORY.md 是索引，方便人读和 system prompt 按需加载。
//   - .memory/*.md 是单条记忆，使用 YAML frontmatter 保存 id、title、tags、summary。
//
// 运行流程：
//   1. 每轮开始前，用当前用户请求检索相关记忆并注入一条内部 <memory> 消息。
//   2. 每轮停止后，请模型从最近对话中提取 durable memories。
//   3. 新记忆写成独立 markdown 文件，索引随写入和低频整理重建。
//
// 状态边界：
// 所有路径、计数器和日志出口都来自 agentRuntime，不使用包级可变变量。

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

type memoryRecord struct {
	ID      string
	Title   string
	Tags    []string
	Summary string
	Content string
	Path    string
}

type memoryCandidate struct {
	Title   string   `json:"title"`
	Tags    []string `json:"tags"`
	Summary string   `json:"summary"`
	Content string   `json:"content"`
}

// injectRelevantMemories 将与当前请求相关的持久记忆注入到消息历史中。
func (rt *agentRuntime) injectRelevantMemories(messages *[]anthropic.MessageParam, query string) {
	memories := rt.relevantMemories(query, 4)
	if len(memories) == 0 {
		return
	}

	var lines []string
	lines = append(lines, "<memory>")
	lines = append(lines, "Relevant persistent memories:")
	for _, memory := range memories {
		lines = append(lines, fmt.Sprintf("- %s: %s\n%s", memory.Title, memory.Summary, memory.Content))
	}
	lines = append(lines, "</memory>")
	*messages = append(*messages, anthropic.NewUserMessage(anthropic.NewTextBlock(strings.Join(lines, "\n"))))
	rt.emitLine("[memory] loaded %d relevant memories", len(memories))
}

// latestUserText 从消息尾部找到最近一条真实用户输入，跳过内部 memory 注入消息。
func latestUserText(messages []anthropic.MessageParam) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "user" {
			continue
		}
		text := strings.TrimSpace(extractResponseText(messages[i]))
		if text == "" || isInjectedContext(text) {
			continue
		}
		return text
	}
	return ""
}

// relevantMemories 用简单关键词匹配从记忆库中取出最相关的若干条。
func (rt *agentRuntime) relevantMemories(query string, limit int) []memoryRecord {
	memories := rt.loadMemoryRecords()
	if len(memories) == 0 {
		return nil
	}

	terms := keywordSet(query)
	type scoredMemory struct {
		memory memoryRecord
		score  int
	}
	var scored []scoredMemory
	for _, memory := range memories {
		haystack := strings.ToLower(strings.Join([]string{
			memory.Title,
			strings.Join(memory.Tags, " "),
			memory.Summary,
			memory.Content,
		}, " "))
		score := 0
		for term := range terms {
			if strings.Contains(haystack, term) {
				score++
			}
		}
		if score > 0 {
			scored = append(scored, scoredMemory{memory: memory, score: score})
		}
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			return scored[i].memory.Title < scored[j].memory.Title
		}
		return scored[i].score > scored[j].score
	})

	if len(scored) > limit {
		scored = scored[:limit]
	}
	result := make([]memoryRecord, 0, len(scored))
	for _, item := range scored {
		result = append(result, item.memory)
	}
	return result
}

// keywordSet 把一段文本拆成用于检索的去重关键词集合。
func keywordSet(text string) map[string]struct{} {
	re := regexp.MustCompile(`[[:alnum:]_]+`)
	words := re.FindAllString(strings.ToLower(text), -1)
	result := make(map[string]struct{})
	for _, word := range words {
		if len(word) < 3 {
			continue
		}
		result[word] = struct{}{}
	}
	return result
}

// loadMemoryRecords 读取 .memory/ 下除 MEMORY.md 外的所有单条记忆文件。
func (rt *agentRuntime) loadMemoryRecords() []memoryRecord {
	entries, err := os.ReadDir(rt.config.MemoryDir)
	if err != nil {
		return nil
	}

	var records []memoryRecord
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "MEMORY.md" || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(rt.config.MemoryDir, entry.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		record, ok := parseMemoryRecord(path, string(raw))
		if ok {
			records = append(records, record)
		}
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].Title < records[j].Title
	})
	return records
}

// parseMemoryRecord 将一份带 frontmatter 的 markdown 解析为 memoryRecord。
func parseMemoryRecord(path string, raw string) (memoryRecord, bool) {
	meta, body := parseFrontmatter(raw)
	title := strings.TrimSpace(meta["title"])
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(path), ".md")
	}
	content := strings.TrimSpace(body)
	if content == "" {
		return memoryRecord{}, false
	}
	return memoryRecord{
		ID:      strings.TrimSpace(meta["id"]),
		Title:   title,
		Tags:    splitTags(meta["tags"]),
		Summary: strings.TrimSpace(meta["summary"]),
		Content: content,
		Path:    path,
	}, true
}

// splitTags 解析 frontmatter 中简化格式的 tags 字段。
func splitTags(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '[' || r == ']'
	})
	var tags []string
	for _, field := range fields {
		tag := strings.Trim(strings.TrimSpace(field), `"'`)
		if tag != "" {
			tags = append(tags, tag)
		}
	}
	return tags
}

// extractMemories 在一轮对话结束后提取新记忆，并周期性重建索引。
func (rt *agentRuntime) extractMemories(ctx context.Context, client anthropic.Client, messages []anthropic.MessageParam) {
	candidates, err := rt.proposeMemories(ctx, client, messages)
	if err != nil {
		rt.emitLine("[memory] extraction skipped: %v", err)
		return
	}
	written := 0
	for _, candidate := range candidates {
		if rt.writeMemory(candidate) {
			written++
		}
	}
	if written > 0 {
		rt.rebuildMemoryIndex()
		rt.emitLine("[memory] wrote %d memories", written)
	}

	rt.memoryTurns++
	if rt.memoryTurns >= 8 {
		rt.memoryTurns = 0
		rt.rebuildMemoryIndex()
		rt.emitLine("[memory] consolidated index")
	}
}

// proposeMemories 调用模型，把最近对话压成结构化的 memoryCandidate 列表。
func (rt *agentRuntime) proposeMemories(ctx context.Context, client anthropic.Client, messages []anthropic.MessageParam) ([]memoryCandidate, error) {
	transcript := recentTranscript(messages, 12)
	if strings.TrimSpace(transcript) == "" {
		return nil, nil
	}

	prompt := "Extract durable user/project memories from this coding-agent conversation.\n" +
		"Return JSON only: an array of objects with title, tags, summary, content.\n" +
		"Only include stable preferences, project facts, decisions, or constraints worth reusing later.\n" +
		"Return [] if there is nothing worth remembering.\n\n" + transcript

	resp, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(rt.config.Model),
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(prompt))},
		MaxTokens: 1200,
	})
	if err != nil {
		return nil, err
	}

	text := extractResponseText(resp.ToParam())
	jsonText := extractJSONArray(text)
	if jsonText == "" {
		return nil, nil
	}
	var candidates []memoryCandidate
	if err := json.Unmarshal([]byte(jsonText), &candidates); err != nil {
		return nil, err
	}
	return candidates, nil
}

// recentTranscript 截取最近若干条消息，生成供记忆提取模型使用的纯文本 transcript。
func recentTranscript(messages []anthropic.MessageParam, limit int) string {
	if len(messages) > limit {
		messages = messages[len(messages)-limit:]
	}
	var lines []string
	for _, message := range messages {
		text := extractResponseText(message)
		if strings.TrimSpace(text) == "" || isInjectedContext(text) {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s: %s", message.Role, text))
	}
	return strings.Join(lines, "\n\n")
}

// isInjectedContext 判断文本是否为 agent 内部注入的上下文消息。
func isInjectedContext(text string) bool {
	trimmed := strings.TrimSpace(text)
	return strings.HasPrefix(trimmed, "<memory>") || strings.HasPrefix(trimmed, "<background>")
}

// extractResponseText 提取 MessageParam 中所有 text block 的文本内容。
func extractResponseText(message anthropic.MessageParam) string {
	var parts []string
	for _, block := range message.Content {
		if block.OfText != nil {
			parts = append(parts, block.OfText.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// extractJSONArray 从模型输出中截取最外层 JSON array，容忍模型额外输出少量文本。
func extractJSONArray(text string) string {
	start := strings.Index(text, "[")
	end := strings.LastIndex(text, "]")
	if start < 0 || end < start {
		return ""
	}
	return text[start : end+1]
}

// writeMemory 将一条候选记忆写成独立 markdown 文件；已存在同 ID 时跳过。
func (rt *agentRuntime) writeMemory(candidate memoryCandidate) bool {
	content := strings.TrimSpace(candidate.Content)
	if content == "" {
		return false
	}
	title := strings.TrimSpace(candidate.Title)
	if title == "" {
		title = strings.TrimSpace(candidate.Summary)
	}
	if title == "" {
		title = "memory"
	}
	id := memoryID(title, content)
	path := filepath.Join(rt.config.MemoryDir, id+".md")
	if _, err := os.Stat(path); err == nil {
		return false
	}
	if err := os.MkdirAll(rt.config.MemoryDir, 0o755); err != nil {
		return false
	}
	body := fmt.Sprintf("---\nid: %s\ntitle: %q\ntags: [%s]\nsummary: %q\ncreated: %s\n---\n\n%s\n",
		id,
		title,
		formatTags(candidate.Tags),
		strings.TrimSpace(candidate.Summary),
		time.Now().Format(time.RFC3339),
		content,
	)
	return os.WriteFile(path, []byte(body), 0o644) == nil
}

// memoryID 根据标题和内容生成稳定、可读、低碰撞的记忆文件名。
func memoryID(title string, content string) string {
	sum := sha1.Sum([]byte(title + "\n" + content))
	return safeFilename(strings.ToLower(title)) + "-" + fmt.Sprintf("%x", sum)[:8]
}

// formatTags 将 tag 列表格式化为 frontmatter 中的 YAML inline array。
func formatTags(tags []string) string {
	var quoted []string
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag != "" {
			quoted = append(quoted, fmt.Sprintf("%q", tag))
		}
	}
	return strings.Join(quoted, ", ")
}

// rebuildMemoryIndex 根据单条记忆文件重建 .memory/MEMORY.md 索引。
func (rt *agentRuntime) rebuildMemoryIndex() {
	records := rt.loadMemoryRecords()
	if err := os.MkdirAll(rt.config.MemoryDir, 0o755); err != nil {
		return
	}

	var lines []string
	lines = append(lines, "# Memory Index", "")
	if len(records) == 0 {
		lines = append(lines, "(no memories)")
	} else {
		for _, record := range records {
			rel, err := filepath.Rel(rt.config.MemoryDir, record.Path)
			if err != nil {
				rel = filepath.Base(record.Path)
			}
			lines = append(lines, fmt.Sprintf("- [%s](%s): %s", record.Title, rel, record.Summary))
		}
	}
	_ = os.WriteFile(rt.config.MemoryIndex, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}
