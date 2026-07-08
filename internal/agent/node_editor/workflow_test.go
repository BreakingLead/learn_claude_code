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
		indexOf(order, "developer") > indexOf(order, "reviewer") ||
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
	reviewer := workflowStepByNodeID(steps, "reviewer")
	if !containsNodeID(reviewer.InputsFrom, "developer") {
		t.Fatalf("expected reviewer to depend on developer output: %+v", reviewer)
	}
	if !strings.Contains(summary.Instruction, "Merge upstream") {
		t.Fatalf("expected summary instruction in execution plan, got %+v", summary)
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
	if !strings.Contains(developer.Outputs[0].Content, "Implement the requested change") {
		t.Fatalf("expected developer instruction in simulation output: %+v", developer)
	}
	reviewer := workflowSimulationStepByNodeID(steps, "reviewer")
	if len(reviewer.Inputs) != 1 || reviewer.Inputs[0].FromNode != "developer" {
		t.Fatalf("expected reviewer to consume developer output: %+v", reviewer)
	}
	if !strings.Contains(reviewer.Inputs[0].Content, "Developer Agent") {
		t.Fatalf("expected developer content in reviewer input: %+v", reviewer.Inputs)
	}
	output := workflowSimulationStepByNodeID(steps, "output")
	if len(output.Inputs) != 1 || output.Inputs[0].FromNode != "summary" {
		t.Fatalf("unexpected output inputs: %+v", output)
	}
}

func TestTimerWorkflowSimulatesScheduledPrompt(t *testing.T) {
	workflow := timerWorkflow()
	if err := ValidateWorkflow(workflow); err != nil {
		t.Fatal(err)
	}
	steps, err := SimulateWorkflow(workflow, "manual input should not replace timer")
	if err != nil {
		t.Fatal(err)
	}
	timer := workflowSimulationStepByNodeID(steps, "timer")
	if len(timer.Outputs) != 1 || timer.Outputs[0].Content != "Check CI status." {
		t.Fatalf("unexpected timer simulation: %+v", timer)
	}
	agent := workflowSimulationStepByNodeID(steps, "agent")
	if len(agent.Inputs) != 1 || agent.Inputs[0].FromNode != "timer" || agent.Inputs[0].Content != "Check CI status." {
		t.Fatalf("expected agent to receive timer prompt: %+v", agent)
	}
}

func TestSimulateWorkflowTemplateAndSwitch(t *testing.T) {
	templateSteps, err := SimulateWorkflow(templateWorkflow(), "build the API")
	if err != nil {
		t.Fatal(err)
	}
	template := workflowSimulationStepByNodeID(templateSteps, "template")
	if len(template.Outputs) != 1 || !strings.Contains(template.Outputs[0].Content, "Scheduled request:\nbuild the API") {
		t.Fatalf("unexpected template output: %+v", template)
	}

	switchSteps, err := SimulateWorkflow(switchWorkflow(), "manual input")
	if err != nil {
		t.Fatal(err)
	}
	choice := workflowSimulationStepByNodeID(switchSteps, "switch")
	if len(choice.Outputs) != 1 || choice.Outputs[0].Content != "Run the daytime prompt." {
		t.Fatalf("unexpected switch output: %+v", choice)
	}
}

func TestDefaultTimerWorkflowValidatesAndOrders(t *testing.T) {
	workflow := DefaultTimerWorkflow()
	if err := ValidateWorkflow(workflow); err != nil {
		t.Fatal(err)
	}
	order, err := WorkflowExecutionOrder(workflow)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(order, ",") != "timer,ci-agent,output" {
		t.Fatalf("unexpected timer workflow order: %v", order)
	}
	if diagnostics := WorkflowDiagnostics(workflow); len(diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", diagnostics)
	}
}

func TestWorkflowDiagnosticsWarnAboutDisconnectedNodes(t *testing.T) {
	if diagnostics := WorkflowDiagnostics(DefaultWorkflow()); len(diagnostics) != 0 {
		t.Fatalf("expected default workflow without diagnostics, got %+v", diagnostics)
	}

	workflow := DefaultWorkflow()
	workflow.Edges = workflow.Edges[:1]
	diagnostics := WorkflowDiagnostics(workflow)
	got := strings.Join(diagnostics, "\n")
	for _, want := range []string{
		`agent node "reviewer" has no incoming message`,
		`agent node "developer" has no outgoing message`,
		`output node "output" has no incoming message`,
		`node "output" is not reachable from a workflow input`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing diagnostic %q in %+v", want, diagnostics)
		}
	}
}

func timerWorkflow() WorkflowDefinition {
	return WorkflowDefinition{
		Version: SchemaVersion,
		ID:      "timer-workflow",
		Name:    "Timer Workflow",
		Nodes: []WorkflowNode{
			{
				ID:       "timer",
				Type:     WorkflowNodeTypeTimer,
				Label:    "Every Morning",
				Position: Position{X: 80, Y: 180},
				Outputs:  []Port{{ID: "message", Type: PortTypeMessage, Label: "Message", Direction: DirectionOutput}},
				Config: map[string]any{
					"cron":   "0 9 * * *",
					"prompt": "Check CI status.",
				},
			},
			{
				ID:             "agent",
				Type:           WorkflowNodeTypeAgent,
				Label:          "CI Agent",
				AgentBlueprint: "default",
				Position:       Position{X: 340, Y: 180},
				Inputs:         []Port{{ID: "input", Type: PortTypeMessage, Label: "Input", Direction: DirectionInput}},
				Outputs:        []Port{{ID: "output", Type: PortTypeMessage, Label: "Output", Direction: DirectionOutput}},
				Config: map[string]any{
					"instruction": "Check the scheduled CI request.",
				},
			},
			{
				ID:       "output",
				Type:     WorkflowNodeTypeOutput,
				Label:    "Output",
				Position: Position{X: 620, Y: 180},
				Inputs:   []Port{{ID: "message", Type: PortTypeMessage, Label: "Message", Direction: DirectionInput}},
			},
		},
		Edges: []Edge{
			{ID: "edge-timer-agent", Source: Endpoint{Node: "timer", Port: "message"}, Target: Endpoint{Node: "agent", Port: "input"}},
			{ID: "edge-agent-output", Source: Endpoint{Node: "agent", Port: "output"}, Target: Endpoint{Node: "output", Port: "message"}},
		},
	}
}

func templateWorkflow() WorkflowDefinition {
	return WorkflowDefinition{
		Version: SchemaVersion,
		ID:      "template-workflow",
		Name:    "Template Workflow",
		Nodes: []WorkflowNode{
			{
				ID:       "prompt",
				Type:     WorkflowNodeTypeInput,
				Label:    "Prompt",
				Position: Position{X: 80, Y: 180},
				Outputs:  []Port{{ID: "message", Type: PortTypeMessage, Label: "Message", Direction: DirectionOutput}},
			},
			{
				ID:       "template",
				Type:     WorkflowNodeTypeTemplate,
				Label:    "Template",
				Position: Position{X: 300, Y: 180},
				Inputs:   []Port{{ID: "input", Type: PortTypeMessage, Label: "Input", Direction: DirectionInput, Multiple: true}},
				Outputs:  []Port{{ID: "message", Type: PortTypeMessage, Label: "Message", Direction: DirectionOutput}},
				Config: map[string]any{
					"template": "Scheduled request:\n{{input}}",
				},
			},
			{
				ID:             "agent",
				Type:           WorkflowNodeTypeAgent,
				Label:          "Agent",
				AgentBlueprint: "default",
				Position:       Position{X: 520, Y: 180},
				Inputs:         []Port{{ID: "input", Type: PortTypeMessage, Label: "Input", Direction: DirectionInput}},
				Outputs:        []Port{{ID: "output", Type: PortTypeMessage, Label: "Output", Direction: DirectionOutput}},
				Config: map[string]any{
					"instruction": "Handle the templated input.",
				},
			},
			{
				ID:       "output",
				Type:     WorkflowNodeTypeOutput,
				Label:    "Output",
				Position: Position{X: 760, Y: 180},
				Inputs:   []Port{{ID: "message", Type: PortTypeMessage, Label: "Message", Direction: DirectionInput}},
			},
		},
		Edges: []Edge{
			{ID: "edge-prompt-template", Source: Endpoint{Node: "prompt", Port: "message"}, Target: Endpoint{Node: "template", Port: "input"}},
			{ID: "edge-template-agent", Source: Endpoint{Node: "template", Port: "message"}, Target: Endpoint{Node: "agent", Port: "input"}},
			{ID: "edge-agent-output", Source: Endpoint{Node: "agent", Port: "output"}, Target: Endpoint{Node: "output", Port: "message"}},
		},
	}
}

func switchWorkflow() WorkflowDefinition {
	return WorkflowDefinition{
		Version: SchemaVersion,
		ID:      "switch-workflow",
		Name:    "Switch Workflow",
		Nodes: []WorkflowNode{
			{
				ID:       "condition",
				Type:     WorkflowNodeTypeTimer,
				Label:    "Condition",
				Position: Position{X: 80, Y: 80},
				Outputs:  []Port{{ID: "message", Type: PortTypeMessage, Label: "Message", Direction: DirectionOutput}},
				Config:   map[string]any{"cron": "0 9 * * *", "prompt": "daytime"},
			},
			{
				ID:       "true-message",
				Type:     WorkflowNodeTypeTimer,
				Label:    "True Message",
				Position: Position{X: 80, Y: 180},
				Outputs:  []Port{{ID: "message", Type: PortTypeMessage, Label: "Message", Direction: DirectionOutput}},
				Config:   map[string]any{"cron": "0 9 * * *", "prompt": "Run the daytime prompt."},
			},
			{
				ID:       "false-message",
				Type:     WorkflowNodeTypeTimer,
				Label:    "False Message",
				Position: Position{X: 80, Y: 280},
				Outputs:  []Port{{ID: "message", Type: PortTypeMessage, Label: "Message", Direction: DirectionOutput}},
				Config:   map[string]any{"cron": "0 9 * * *", "prompt": "Run the after-hours prompt."},
			},
			{
				ID:       "switch",
				Type:     WorkflowNodeTypeSwitch,
				Label:    "Switch",
				Position: Position{X: 340, Y: 180},
				Inputs: []Port{
					{ID: "condition", Type: PortTypeMessage, Label: "Condition", Direction: DirectionInput},
					{ID: "true", Type: PortTypeMessage, Label: "True", Direction: DirectionInput},
					{ID: "false", Type: PortTypeMessage, Label: "False", Direction: DirectionInput},
				},
				Outputs: []Port{{ID: "message", Type: PortTypeMessage, Label: "Message", Direction: DirectionOutput}},
				Config:  map[string]any{"match": "daytime"},
			},
			{
				ID:       "output",
				Type:     WorkflowNodeTypeOutput,
				Label:    "Output",
				Position: Position{X: 600, Y: 180},
				Inputs:   []Port{{ID: "message", Type: PortTypeMessage, Label: "Message", Direction: DirectionInput}},
			},
		},
		Edges: []Edge{
			{ID: "edge-condition-switch", Source: Endpoint{Node: "condition", Port: "message"}, Target: Endpoint{Node: "switch", Port: "condition"}},
			{ID: "edge-true-switch", Source: Endpoint{Node: "true-message", Port: "message"}, Target: Endpoint{Node: "switch", Port: "true"}},
			{ID: "edge-false-switch", Source: Endpoint{Node: "false-message", Port: "message"}, Target: Endpoint{Node: "switch", Port: "false"}},
			{ID: "edge-switch-output", Source: Endpoint{Node: "switch", Port: "message"}, Target: Endpoint{Node: "output", Port: "message"}},
		},
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

func TestEnsureDefaultTimerWorkflowWritesOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agents", "blueprints", "workflows", "timer-ci-check.json")
	created, err := EnsureDefaultTimerWorkflow(path)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("expected default timer workflow to be created")
	}
	workflow, err := ReadWorkflow(path)
	if err != nil {
		t.Fatal(err)
	}
	if workflow.ID != "timer-ci-check" {
		t.Fatalf("unexpected workflow: %+v", workflow)
	}

	created, err = EnsureDefaultTimerWorkflow(path)
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("expected existing timer workflow to be preserved")
	}
}

func TestExampleWorkflowsValidateAndOrder(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "..", "docs", "examples", "workflows", "*.json"))
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
			if diagnostics := WorkflowDiagnostics(workflow); len(diagnostics) != 0 {
				t.Fatalf("unexpected diagnostics for %s: %+v", path, diagnostics)
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
