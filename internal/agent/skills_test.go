package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGetSystemPromptIncludesMemoryAndCachesByContext(t *testing.T) {
	workdir := t.TempDir()
	writeFile(t, filepath.Join(workdir, "AGENTS.md"), "# Repository Guidelines\n\nUse explicit runtime state.")
	writeFile(t, filepath.Join(workdir, "README.md"), "# Project\n\nRun with go run ./cmd/bee-agent.")
	writeFile(t, filepath.Join(workdir, ".agents", ".memory", "MEMORY.md"), "# Memory Index\n\n- Prefer explicit state.")
	writeFile(t, filepath.Join(workdir, ".agents", "skills", "demo", "SKILL.md"), "---\nname: demo\ndescription: Demo skill\n---\nDetails")

	rt := newAgentRuntime(testConfig(workdir), nil, nil)

	prompt := rt.getSystemPrompt([]string{"bash", "read_file"})
	if !strings.Contains(prompt, "Use explicit runtime state.") {
		t.Fatalf("prompt missing AGENTS.md project context: %s", prompt)
	}
	if !strings.Contains(prompt, "Module context:") || !strings.Contains(prompt, "## Memory") || !strings.Contains(prompt, "Prefer explicit state.") {
		t.Fatalf("prompt missing .agents/.memory/MEMORY.md section: %s", prompt)
	}
	if !strings.Contains(prompt, "**demo**: Demo skill") {
		t.Fatalf("prompt missing skill catalog: %s", prompt)
	}

	cachedKey := rt.promptCache.contextKey
	if again := rt.getSystemPrompt([]string{"read_file", "bash"}); again != prompt {
		t.Fatalf("expected stable prompt for same tools in different order")
	}
	if rt.promptCache.contextKey != cachedKey {
		t.Fatalf("expected stable context key for same prompt context")
	}

	writeFile(t, filepath.Join(workdir, ".agents", ".memory", "MEMORY.md"), "# Memory Index\n\nChanged memory.")
	updated := rt.getSystemPrompt([]string{"bash", "read_file"})
	if updated == prompt {
		t.Fatalf("expected prompt to refresh after memory content changes")
	}
	if rt.promptCache.contextKey == cachedKey {
		t.Fatalf("expected context key to change after memory content changes")
	}
}

func TestGetSystemPromptEmitsAssembledThenCacheHit(t *testing.T) {
	workdir := t.TempDir()
	writeFile(t, filepath.Join(workdir, ".agents", ".memory", "MEMORY.md"), "# Memory Index\n\n- Cache this.")
	events := make(chan uiEvent, 4)
	rt := newAgentRuntime(testConfig(workdir), events, nil)

	rt.getSystemPrompt([]string{"bash"})
	first := <-events
	if !strings.Contains(first.Text, "[assembled]") {
		t.Fatalf("expected assembled log, got %q", first.Text)
	}

	rt.getSystemPrompt([]string{"bash"})
	second := <-events
	if !strings.Contains(second.Text, "[cache hit]") {
		t.Fatalf("expected cache hit log, got %q", second.Text)
	}
}

func testConfig(workdir string) agentConfig {
	return agentConfig{
		Model:              "test-model",
		Workdir:            workdir,
		CompactDir:         filepath.Join(workdir, ".agents", "compact"),
		ToolResultsDir:     filepath.Join(workdir, ".agents", "compact", "tool_results"),
		TranscriptDir:      filepath.Join(workdir, ".agents", "compact", "transcripts"),
		MemoryDir:          filepath.Join(workdir, ".agents", ".memory"),
		MemoryIndex:        filepath.Join(workdir, ".agents", ".memory", "MEMORY.md"),
		TaskDir:            filepath.Join(workdir, ".agents", ".tasks"),
		TaskIndex:          filepath.Join(workdir, ".agents", ".tasks", "TASKS.md"),
		ScheduledTasksPath: filepath.Join(workdir, ".scheduled_tasks.json"),
		BackgroundTimeout:  10 * time.Minute,
	}
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
