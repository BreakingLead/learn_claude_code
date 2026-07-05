package nodeeditor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultWorkflowValidatesAndOrders(t *testing.T) {
	workflow := DefaultWorkflow()
	if err := ValidateWorkflow(workflow); err != nil {
		t.Fatal(err)
	}
	order, err := WorkflowExecutionOrder(workflow)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(order, ",")
	for _, want := range []string{"prompt", "developer", "reviewer", "summary", "output"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing node %q in order %s", want, got)
		}
	}
	if indexOf(order, "prompt") > indexOf(order, "developer") ||
		indexOf(order, "prompt") > indexOf(order, "reviewer") ||
		indexOf(order, "developer") > indexOf(order, "summary") ||
		indexOf(order, "reviewer") > indexOf(order, "summary") ||
		indexOf(order, "summary") > indexOf(order, "output") {
		t.Fatalf("unexpected execution order: %v", order)
	}
}

func TestWorkflowExecutionPlanShowsDataFlow(t *testing.T) {
	steps, err := WorkflowExecutionPlan(DefaultWorkflow())
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != len(DefaultWorkflow().Nodes) {
		t.Fatalf("unexpected plan length: %+v", steps)
	}
	summary := workflowStepByNodeID(steps, "summary")
	if summary.NodeID != "summary" {
		t.Fatalf("missing summary step: %+v", steps)
	}
	if !containsNodeID(summary.InputsFrom, "developer") || !containsNodeID(summary.InputsFrom, "reviewer") {
		t.Fatalf("unexpected summary inputs: %+v", summary)
	}
	if !containsNodeID(summary.OutputsTo, "output") {
		t.Fatalf("unexpected summary outputs: %+v", summary)
	}
}

func TestSimulateWorkflowShowsMessageHandoff(t *testing.T) {
	steps, err := SimulateWorkflow(DefaultWorkflow(), "build the API")
	if err != nil {
		t.Fatal(err)
	}
	prompt := workflowSimulationStepByNodeID(steps, "prompt")
	if len(prompt.Outputs) != 1 || prompt.Outputs[0].Content != "build the API" {
		t.Fatalf("unexpected prompt simulation: %+v", prompt)
	}
	developer := workflowSimulationStepByNodeID(steps, "developer")
	if len(developer.Inputs) != 1 || developer.Inputs[0].FromNode != "prompt" {
		t.Fatalf("unexpected developer inputs: %+v", developer)
	}
	if len(developer.Outputs) != 1 || !strings.Contains(developer.Outputs[0].Content, "Developer Agent") {
		t.Fatalf("unexpected developer output: %+v", developer)
	}
	output := workflowSimulationStepByNodeID(steps, "output")
	if len(output.Inputs) != 1 || output.Inputs[0].FromNode != "summary" {
		t.Fatalf("unexpected output inputs: %+v", output)
	}
}

func TestEnsureDefaultWorkflowWritesOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agents", "blueprints", "workflows", "review-pipeline.json")
	created, err := EnsureDefaultWorkflow(path)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("expected default workflow to be created")
	}
	workflow, err := ReadWorkflow(path)
	if err != nil {
		t.Fatal(err)
	}
	if workflow.ID != "review-pipeline" {
		t.Fatalf("unexpected workflow: %+v", workflow)
	}

	custom := []byte(`{"version":1,"id":"custom","name":"Custom","nodes":[{"id":"out","type":"workflow_output","label":"Out","position":{"x":0,"y":0},"inputs":[{"id":"message","type":"message","label":"Message","direction":"input"}]}],"edges":[]}`)
	if err := os.WriteFile(path, custom, 0o644); err != nil {
		t.Fatal(err)
	}
	created, err = EnsureDefaultWorkflow(path)
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("expected existing workflow to be preserved")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(custom) {
		t.Fatalf("expected custom workflow to remain, got %s", string(got))
	}
}

func TestExampleWorkflowsValidateAndOrder(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "..", ".agents", "blueprints", "workflows", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("expected example workflows")
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			workflow, err := ReadWorkflow(path)
			if err != nil {
				t.Fatal(err)
			}
			order, err := WorkflowExecutionOrder(workflow)
			if err != nil {
				t.Fatal(err)
			}
			if len(order) != len(workflow.Nodes) {
				t.Fatalf("unexpected order for %s: %+v", path, order)
			}
		})
	}
}

func TestValidateWorkflowRejectsCycles(t *testing.T) {
	workflow := DefaultWorkflow()
	workflow.Edges = append(workflow.Edges, Edge{
		ID:     "edge-output-summary",
		Source: Endpoint{Node: "output", Port: "loop"},
		Target: Endpoint{Node: "summary", Port: "input"},
	})
	workflow.Nodes[len(workflow.Nodes)-1].Outputs = []Port{{ID: "loop", Type: PortTypeMessage, Label: "Loop", Direction: DirectionOutput}}

	err := ValidateWorkflow(workflow)
	if err == nil {
		t.Fatal("expected cycle error")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateWorkflowRejectsIncompatiblePorts(t *testing.T) {
	workflow := DefaultWorkflow()
	workflow.Nodes[1].Inputs[0].Type = PortTypePrompt

	err := ValidateWorkflow(workflow)
	if err == nil {
		t.Fatal("expected incompatible port error")
	}
	if !strings.Contains(err.Error(), "connects message to prompt") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateWorkflowRejectsDuplicateSingleInput(t *testing.T) {
	workflow := DefaultWorkflow()
	workflow.Edges = append(workflow.Edges, Edge{
		ID:     "edge-reviewer-output",
		Source: Endpoint{Node: "reviewer", Port: "output"},
		Target: Endpoint{Node: "output", Port: "message"},
	})

	err := ValidateWorkflow(workflow)
	if err == nil {
		t.Fatal("expected duplicate input error")
	}
	if !strings.Contains(err.Error(), "allows only one edge") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateWorkflowRejectsAgentWithoutBlueprint(t *testing.T) {
	workflow := DefaultWorkflow()
	workflow.Nodes[1].AgentBlueprint = ""

	err := ValidateWorkflow(workflow)
	if err == nil {
		t.Fatal("expected missing agent blueprint error")
	}
	if !strings.Contains(err.Error(), "requires agent_blueprint") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func indexOf(values []string, target string) int {
	for index, value := range values {
		if value == target {
			return index
		}
	}
	return -1
}

func workflowStepByNodeID(steps []WorkflowExecutionStep, nodeID string) WorkflowExecutionStep {
	for _, step := range steps {
		if step.NodeID == nodeID {
			return step
		}
	}
	return WorkflowExecutionStep{}
}

func workflowSimulationStepByNodeID(steps []WorkflowSimulationStep, nodeID string) WorkflowSimulationStep {
	for _, step := range steps {
		if step.NodeID == nodeID {
			return step
		}
	}
	return WorkflowSimulationStep{}
}
