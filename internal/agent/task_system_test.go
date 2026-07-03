package agent

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// TestTaskCreateUpdateAndList 验证 .agents/.tasks/ 任务文件和 TASKS.md 索引会同步更新。
func TestTaskCreateUpdateAndList(t *testing.T) {
	workdir := t.TempDir()
	rt := newAgentRuntime(testConfig(workdir), nil, nil)

	raw, _ := json.Marshal(map[string]string{
		"title":   "Implement recovery",
		"summary": "Add retry state machine",
		"content": "Recovery should stay on agentRuntime.",
	})
	created := rt.runTaskCreate(raw)
	if !strings.Contains(created, "Created task") {
		t.Fatalf("unexpected create result: %s", created)
	}

	tasks := rt.loadTaskRecords()
	if len(tasks) != 1 {
		t.Fatalf("expected one task, got %d", len(tasks))
	}
	updateRaw, _ := json.Marshal(map[string]string{
		"id":     tasks[0].ID,
		"status": "completed",
	})
	updated := rt.runTaskUpdate(updateRaw)
	if !strings.Contains(updated, "Updated task") {
		t.Fatalf("unexpected update result: %s", updated)
	}

	index := readFile(t, filepath.Join(workdir, ".agents", ".tasks", "TASKS.md"))
	if !strings.Contains(index, "[completed] Implement recovery") {
		t.Fatalf("unexpected task index:\n%s", index)
	}
}
