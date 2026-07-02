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

	writeFile(t, filepath.Join(workdir, ".memory", "explicit-state.md"), "---\nid: explicit-state\ntitle: \"Explicit State\"\ntags: [go, runtime]\nsummary: \"Prefer explicit runtime state.\"\n---\n\nDo not use global variables for agent state.")
	writeFile(t, filepath.Join(workdir, ".memory", "unrelated.md"), "---\nid: unrelated\ntitle: \"Other\"\ntags: [docs]\nsummary: \"Unrelated.\"\n---\n\nWrite short docs.")

	memories := rt.relevantMemories("How should this Go runtime handle global state?", 4)
	if len(memories) != 1 {
		t.Fatalf("expected one relevant memory, got %d", len(memories))
	}
	if memories[0].Title != "Explicit State" {
		t.Fatalf("unexpected memory: %+v", memories[0])
	}
}

func TestRebuildMemoryIndexWritesMemoryMarkdownIndex(t *testing.T) {
	workdir := t.TempDir()
	rt := newAgentRuntime(testConfig(workdir), nil, nil)
	if !rt.writeMemory(memoryCandidate{
		Title:   "Runtime State",
		Tags:    []string{"go"},
		Summary: "Keep state explicit.",
		Content: "Runtime state belongs on agentRuntime.",
	}) {
		t.Fatalf("expected memory to be written")
	}

	rt.rebuildMemoryIndex()
	index := readFile(t, filepath.Join(workdir, ".memory", "MEMORY.md"))
	if !strings.Contains(index, "# Memory Index") || !strings.Contains(index, "Runtime State") {
		t.Fatalf("unexpected index:\n%s", index)
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
