package agent

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// TestTaskSystemPersistsJSONAndHonorsBlockedBy 验证 JSON 任务、blockedBy 依赖和解锁提示。
func TestTaskSystemPersistsJSONAndHonorsBlockedBy(t *testing.T) {
	workdir := t.TempDir()
	rt := newAgentRuntime(testConfig(workdir), nil, nil)

	schemaID := createTaskForTest(t, rt, "setup database schema")
	endpointsID := createTaskForTest(t, rt, "create API endpoints", schemaID)
	testsID := createTaskForTest(t, rt, "write tests", endpointsID)
	docsID := createTaskForTest(t, rt, "write docs", schemaID)

	blocked := rt.runTaskClaim(mustJSON(t, map[string]string{"id": endpointsID, "owner": "api-agent"}))
	if !strings.Contains(blocked, "Blocked by: "+schemaID) {
		t.Fatalf("expected endpoints to be blocked by schema, got: %s", blocked)
	}

	claimedSchema := rt.runTaskClaim(mustJSON(t, map[string]string{"id": schemaID, "owner": "db-agent"}))
	if !strings.Contains(claimedSchema, "Claimed "+schemaID) {
		t.Fatalf("unexpected schema claim: %s", claimedSchema)
	}

	completedSchema := rt.runTaskComplete(mustJSON(t, map[string]string{"id": schemaID}))
	if !strings.Contains(completedSchema, "Completed "+schemaID) {
		t.Fatalf("unexpected schema complete: %s", completedSchema)
	}
	if !strings.Contains(completedSchema, "create API endpoints") || !strings.Contains(completedSchema, "write docs") {
		t.Fatalf("expected schema completion to unblock endpoints and docs, got: %s", completedSchema)
	}

	claimedEndpoints := rt.runTaskClaim(mustJSON(t, map[string]string{"id": endpointsID}))
	if !strings.Contains(claimedEndpoints, "Claimed "+endpointsID) {
		t.Fatalf("unexpected endpoints claim: %s", claimedEndpoints)
	}

	completedEndpoints := rt.runTaskComplete(mustJSON(t, map[string]string{"id": endpointsID}))
	if !strings.Contains(completedEndpoints, "write tests") {
		t.Fatalf("expected endpoints completion to unblock tests, got: %s", completedEndpoints)
	}

	getTask := rt.runTaskGet(mustJSON(t, map[string]string{"id": testsID}))
	if !strings.Contains(getTask, `"blockedBy":`) || !strings.Contains(getTask, endpointsID) {
		t.Fatalf("expected get_task JSON with dependency, got: %s", getTask)
	}

	index := readFile(t, filepath.Join(workdir, ".agents", ".tasks", "TASKS.md"))
	if !strings.Contains(index, "[completed] setup database schema") {
		t.Fatalf("unexpected task index:\n%s", index)
	}
	if !strings.Contains(index, "blockedBy="+schemaID) || !strings.Contains(index, docsID) {
		t.Fatalf("expected dependency details in task index:\n%s", index)
	}

	taskJSON := readFile(t, filepath.Join(workdir, ".agents", ".tasks", schemaID+".json"))
	if !strings.Contains(taskJSON, `"status": "completed"`) || !strings.Contains(taskJSON, `"owner": "db-agent"`) {
		t.Fatalf("unexpected task JSON:\n%s", taskJSON)
	}
}

func createTaskForTest(t *testing.T, rt *agentRuntime, subject string, blockedBy ...string) string {
	t.Helper()
	raw := mustJSON(t, map[string]any{
		"subject":     subject,
		"description": subject + " description",
		"blockedBy":   blockedBy,
	})
	created := rt.runTaskCreate(raw)
	if !strings.Contains(created, "Created task_") {
		t.Fatalf("unexpected create result: %s", created)
	}
	tasks := rt.loadTaskRecords()
	for _, task := range tasks {
		if task.Subject == subject {
			return task.ID
		}
	}
	t.Fatalf("created task not found: %s", subject)
	return ""
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
