package nodeeditor

import (
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
