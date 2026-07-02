package agent

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

func latestUserText(messages []anthropic.MessageParam) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "user" {
			continue
		}
		text := strings.TrimSpace(extractResponseText(messages[i]))
		if text == "" || strings.HasPrefix(text, "<memory>") {
			continue
		}
		return text
	}
	return ""
}

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

func recentTranscript(messages []anthropic.MessageParam, limit int) string {
	if len(messages) > limit {
		messages = messages[len(messages)-limit:]
	}
	var lines []string
	for _, message := range messages {
		text := extractResponseText(message)
		if strings.TrimSpace(text) == "" || strings.HasPrefix(strings.TrimSpace(text), "<memory>") {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s: %s", message.Role, text))
	}
	return strings.Join(lines, "\n\n")
}

func extractResponseText(message anthropic.MessageParam) string {
	var parts []string
	for _, block := range message.Content {
		if block.OfText != nil {
			parts = append(parts, block.OfText.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func extractJSONArray(text string) string {
	start := strings.Index(text, "[")
	end := strings.LastIndex(text, "]")
	if start < 0 || end < start {
		return ""
	}
	return text[start : end+1]
}

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

func memoryID(title string, content string) string {
	sum := sha1.Sum([]byte(title + "\n" + content))
	return safeFilename(strings.ToLower(title)) + "-" + fmt.Sprintf("%x", sum)[:8]
}

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
