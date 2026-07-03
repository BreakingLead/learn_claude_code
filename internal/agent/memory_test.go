package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

func TestRelevantMemoriesLoadsMatchingMarkdownRecords(t *testing.T) {
	workdir := t.TempDir()
	rt := newAgentRuntime(testConfig(workdir), nil, nil)

	writeFile(t, filepath.Join(workdir, ".agents", ".memory", "explicit-state.md"), "---\nid: explicit-state\ntype: feedback\ntitle: \"Explicit State\"\ntags: [go, runtime]\nsummary: \"Prefer explicit runtime state.\"\n---\n\nDo not use global variables for agent state.")
	writeFile(t, filepath.Join(workdir, ".agents", ".memory", "unrelated.md"), "---\nid: unrelated\ntitle: \"Other\"\ntags: [docs]\nsummary: \"Unrelated.\"\n---\n\nWrite short docs.")

	memories := rt.relevantMemories("How should this Go runtime handle global state?", 4)
	if len(memories) != 1 {
		t.Fatalf("expected one relevant memory, got %d", len(memories))
	}
	if memories[0].Title != "Explicit State" {
		t.Fatalf("unexpected memory: %+v", memories[0])
	}
	if memories[0].Type != memoryTypeFeedback {
		t.Fatalf("expected feedback memory type, got %q", memories[0].Type)
	}
}

func TestParseMemoryRecordDefaultsMissingTypeToProject(t *testing.T) {
	record, ok := parseMemoryRecord("legacy.md", "---\nid: legacy\ntitle: \"Legacy\"\nsummary: \"Old memory.\"\n---\n\nStill valid.")
	if !ok {
		t.Fatalf("expected legacy memory to parse")
	}
	if record.Type != memoryTypeProject {
		t.Fatalf("expected missing type to default to project, got %q", record.Type)
	}
}

func TestRebuildMemoryIndexWritesMemoryMarkdownIndex(t *testing.T) {
	workdir := t.TempDir()
	rt := newAgentRuntime(testConfig(workdir), nil, nil)
	if !rt.writeMemory(memoryCandidate{
		Title:   "Runtime State",
		Type:    "feedback",
		Tags:    []string{"go"},
		Summary: "Keep state explicit.",
		Content: "Runtime state belongs on agentRuntime.",
	}) {
		t.Fatalf("expected memory to be written")
	}

	rt.rebuildMemoryIndex()
	index := readFile(t, filepath.Join(workdir, ".agents", ".memory", "MEMORY.md"))
	if !strings.Contains(index, "# Memory Index") || !strings.Contains(index, "Runtime State") {
		t.Fatalf("unexpected index:\n%s", index)
	}
	if !strings.Contains(index, "- feedback [Runtime State]") {
		t.Fatalf("expected typed memory index:\n%s", index)
	}

	path := filepath.Join(workdir, ".agents", ".memory", memoryID("Runtime State", "Runtime state belongs on agentRuntime.")+".md")
	body := readFile(t, path)
	if !strings.Contains(body, "\ntype: feedback\n") {
		t.Fatalf("expected memory frontmatter type, got:\n%s", body)
	}
}

func TestLatestUserTextSkipsInjectedMemoryBlocks(t *testing.T) {
	messages := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock("real request")),
		anthropic.NewUserMessage(anthropic.NewTextBlock("<memory>\ninternal\n</memory>")),
	}

	got := latestUserText(messages)
	if got != "real request" {
		t.Fatalf("expected real request, got %q", got)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
