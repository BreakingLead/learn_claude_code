package nodeeditor

import (
	"strings"
	"testing"
	"time"
)

func TestResolvePromptSourceChoosesTrueBranch(t *testing.T) {
	blueprint := selectPromptBlueprint(">=", 9, 9)
	source := ResolvePromptSource(blueprint, "time-select", EvaluationContext{Now: time.Date(2026, 7, 6, 9, 30, 0, 0, time.UTC)})
	if !source.OK {
		t.Fatal("expected prompt source to resolve")
	}
	if source.Node.ID != "work-hours" {
		t.Fatalf("expected true prompt branch, got %+v", source.Node)
	}
}

func TestResolvePromptSourceChoosesFalseBranch(t *testing.T) {
	blueprint := selectPromptBlueprint(">", 9, 18)
	source := ResolvePromptSource(blueprint, "time-select", EvaluationContext{Now: time.Date(2026, 7, 6, 9, 30, 0, 0, time.UTC)})
	if !source.OK {
		t.Fatal("expected prompt source to resolve")
	}
	if source.Node.ID != "after-hours" {
		t.Fatalf("expected false prompt branch, got %+v", source.Node)
	}
}

func TestCurrentTimeNodeCanDriveComparison(t *testing.T) {
	blueprint := selectPromptBlueprint(">=", 0, 9)
	blueprint.Nodes = append(blueprint.Nodes, Node{
		ID:       "current-time",
		Type:     NodeTypeTime,
		Label:    "Current Time",
		Position: Position{X: 80, Y: 760},
		Outputs:  []Port{{ID: "value", Type: PortTypeValue, Label: "Time", Direction: DirectionOutput}},
		Config:   map[string]any{"unit": "hour"},
	})
	blueprint.Edges = append(blueprint.Edges, Edge{
		ID:     "edge-time-compare",
		Source: Endpoint{Node: "current-time", Port: "value"},
		Target: Endpoint{Node: "time-compare", Port: "a"},
	})

	source := ResolvePromptSource(blueprint, "time-select", EvaluationContext{Now: time.Date(2026, 7, 6, 10, 30, 0, 0, time.UTC)})
	if !source.OK {
		t.Fatal("expected prompt source to resolve")
	}
	if source.Node.ID != "work-hours" {
		t.Fatalf("expected current time to select true branch, got %+v", source.Node)
	}
}

func TestPromptPreviewUsesSelectedPromptBranch(t *testing.T) {
	blueprint := selectPromptBlueprint(">=", 9, 9)
	resolved, err := Resolve(blueprint)
	if err != nil {
		t.Fatal(err)
	}
	blocks := PromptPreview(blueprint, resolved)
	var joined []string
	for _, block := range blocks {
		joined = append(joined, block.Preview)
	}
	text := strings.Join(joined, "\n")
	if !strings.Contains(text, "Use daytime workflow.") {
		t.Fatalf("expected selected true branch in preview, got %+v", blocks)
	}
	if strings.Contains(text, "Use night workflow.") {
		t.Fatalf("unexpected false branch in preview, got %+v", blocks)
	}
}

func selectPromptBlueprint(operator string, a float64, b float64) Blueprint {
	blueprint := DefaultBlueprint()
	blueprint.Nodes = append(blueprint.Nodes,
		Node{
			ID:       "time-compare",
			Type:     NodeTypeCompare,
			Label:    "Time Compare",
			Position: Position{X: 260, Y: 560},
			Inputs: []Port{
				{ID: "a", Type: PortTypeValue, Label: "A", Direction: DirectionInput},
				{ID: "b", Type: PortTypeValue, Label: "B", Direction: DirectionInput},
			},
			Outputs: []Port{{ID: "result", Type: PortTypeBoolean, Label: "Result", Direction: DirectionOutput}},
			Config:  map[string]any{"operator": operator, "a": a, "b": b},
		},
		Node{
			ID:       "work-hours",
			Type:     NodeTypePrompt,
			Label:    "Work Hours Prompt",
			Position: Position{X: 80, Y: 520},
			Outputs:  []Port{{ID: "prompt_out", Type: PortTypePrompt, Label: "Prompt", Direction: DirectionOutput}},
			Config:   map[string]any{"source": "inline", "prompt": "Use daytime workflow."},
		},
		Node{
			ID:       "after-hours",
			Type:     NodeTypePrompt,
			Label:    "After Hours Prompt",
			Position: Position{X: 80, Y: 640},
			Outputs:  []Port{{ID: "prompt_out", Type: PortTypePrompt, Label: "Prompt", Direction: DirectionOutput}},
			Config:   map[string]any{"source": "inline", "prompt": "Use night workflow."},
		},
		Node{
			ID:       "time-select",
			Type:     NodeTypeSelect,
			Label:    "Time Select",
			Position: Position{X: 480, Y: 580},
			Inputs: []Port{
				{ID: "condition", Type: PortTypeBoolean, Label: "Condition", Direction: DirectionInput},
				{ID: "true", Type: PortTypePrompt, Label: "True", Direction: DirectionInput},
				{ID: "false", Type: PortTypePrompt, Label: "False", Direction: DirectionInput},
			},
			Outputs: []Port{{ID: "prompt_out", Type: PortTypePrompt, Label: "Prompt", Direction: DirectionOutput}},
			Config:  map[string]any{"default": false},
		},
	)
	blueprint.Edges = append(blueprint.Edges,
		Edge{ID: "edge-compare-select", Source: Endpoint{Node: "time-compare", Port: "result"}, Target: Endpoint{Node: "time-select", Port: "condition"}},
		Edge{ID: "edge-work-select", Source: Endpoint{Node: "work-hours", Port: "prompt_out"}, Target: Endpoint{Node: "time-select", Port: "true"}},
		Edge{ID: "edge-after-select", Source: Endpoint{Node: "after-hours", Port: "prompt_out"}, Target: Endpoint{Node: "time-select", Port: "false"}},
		Edge{ID: "edge-select-agent", Source: Endpoint{Node: "time-select", Port: "prompt_out"}, Target: Endpoint{Node: "agent-main", Port: "prompt_3"}},
	)
	return blueprint
}
