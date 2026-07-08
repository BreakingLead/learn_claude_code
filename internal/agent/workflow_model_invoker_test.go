package agent

import (
	nodeeditor "bee_agent/internal/agent/node_editor"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBeeAgentWorkflowInvokerRunsDefaultTimerWorkflowAgainstModelAPI(t *testing.T) {
	workdir := t.TempDir()
	t.Chdir(workdir)

	var captured map[string]any
	modelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("unexpected model path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode model request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_test",
			"type": "message",
			"role": "assistant",
			"model": "test-model",
			"content": [{"type": "text", "text": "CI is green. No action needed."}],
			"stop_reason": "end_turn",
			"stop_sequence": null,
			"usage": {"input_tokens": 12, "output_tokens": 8}
		}`))
	}))
	defer modelServer.Close()

	config, err := newAgentConfig(RunOptions{
		APIKey:  "test-key",
		BaseURL: modelServer.URL,
		Model:   "test-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := nodeeditor.EnsureDefaultBlueprint(config.DefaultBlueprintPath); err != nil {
		t.Fatal(err)
	}

	store := nodeeditor.NewStore(workdir)
	workflow := nodeeditor.DefaultTimerWorkflow()
	if err := store.WriteWorkflow(workflow.ID, workflow); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.SaveCompiledWorkflowPlan(workflow); err != nil {
		t.Fatal(err)
	}
	store.SetPlanExecutorFactory(newBeeAgentPlanExecutorFactory(config, newAnthropicClient(config)))

	run, err := store.RunWorkflowPlan(context.Background(), workflow.ID, nodeeditor.WorkflowPlanRunRequest{
		ExecutionMode: nodeeditor.WorkflowExecutionModeBeeAgent,
		TimeoutMS:     5000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != nodeeditor.WorkflowRunStatusCompleted || len(run.Outputs) != 1 {
		t.Fatalf("unexpected model workflow run: %+v", run)
	}
	if !strings.Contains(run.Outputs[0].Content, "CI is green") {
		t.Fatalf("expected model output to reach workflow output: %+v", run.Outputs)
	}

	raw, err := json.Marshal(captured)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	for _, want := range []string{
		"Check CI status",
		"Workflow instruction",
		"CI Monitor Agent",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("model request missing %q: %s", want, body)
		}
	}
}
