package nodeeditor

import (
	"path/filepath"
	"strings"
	"testing"
)

type mapCompositeLoader map[string]CompositeDefinition

func (m mapCompositeLoader) LoadComposite(id string) (CompositeDefinition, error) {
	definition, ok := m[id]
	if !ok {
		return CompositeDefinition{}, errNotFound(id)
	}
	return definition, nil
}

type errNotFound string

func (e errNotFound) Error() string { return "not found: " + string(e) }

func TestExpandCompositesReplacesCompositeNodeWithInternalGraph(t *testing.T) {
	blueprint := DefaultBlueprint()
	blueprint.Nodes = append(blueprint.Nodes, Node{
		ID:       "safe-tools",
		Type:     NodeTypeComposite,
		Label:    "Safe Tools",
		Position: Position{X: 300, Y: 400},
		Outputs:  []Port{{ID: "toolset_out", Type: PortTypeToolset, Label: "Toolset", Direction: DirectionOutput}},
		Config:   map[string]any{"definition": "safe-tools"},
	})
	blueprint.Edges = append(blueprint.Edges, Edge{
		ID:     "edge-safe-tools",
		Source: Endpoint{Node: "safe-tools", Port: "toolset_out"},
		Target: Endpoint{Node: "agent-main", Port: "toolset_in"},
	})

	expanded, err := ExpandComposites(blueprint, mapCompositeLoader{
		"safe-tools": safeToolsComposite(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(expanded); err != nil {
		t.Fatal(err)
	}
	resolved, err := Resolve(expanded)
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(resolved.ToolsetNodes, "safe-tools__readonly-tools") {
		t.Fatalf("expected expanded toolset node in %+v", resolved.ToolsetNodes)
	}
}

func TestExpandCompositesRejectsCycles(t *testing.T) {
	blueprint := DefaultBlueprint()
	blueprint.Nodes = append(blueprint.Nodes, Node{
		ID:      "cycle",
		Type:    NodeTypeComposite,
		Label:   "Cycle",
		Outputs: []Port{{ID: "prompt_out", Type: PortTypePrompt, Label: "Prompt", Direction: DirectionOutput}},
		Config:  map[string]any{"definition": "cycle-a"},
	})
	blueprint.Edges = append(blueprint.Edges, Edge{
		ID:     "edge-cycle",
		Source: Endpoint{Node: "cycle", Port: "prompt_out"},
		Target: Endpoint{Node: "agent-main", Port: "prompt_3"},
	})

	_, err := ExpandComposites(blueprint, mapCompositeLoader{
		"cycle-a": cycleComposite("cycle-b"),
		"cycle-b": cycleComposite("cycle-a"),
	})
	if err == nil {
		t.Fatal("expected composite cycle error")
	}
	if !strings.Contains(err.Error(), "composite cycle") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExampleCompositeDefinitionIsValid(t *testing.T) {
	path := filepath.Join("..", "..", "..", ".agents", "blueprints", "composites", "safe-readonly-workspace.json")
	definition, err := ReadComposite(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateComposite(definition); err != nil {
		t.Fatal(err)
	}
	template := CompositeNodeTemplate(definition)
	if template.Type != NodeTypeComposite || len(template.Node.Outputs) != 2 {
		t.Fatalf("unexpected composite template: %+v", template)
	}
}

func TestBuildCompositeFromSelectionExposesBoundaryPorts(t *testing.T) {
	blueprint := DefaultBlueprint()
	definition, err := BuildCompositeFromSelection(
		blueprint,
		[]string{"project-context", "build-mode"},
		"context-pack",
		"Context Pack",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(definition.Nodes) != 2 {
		t.Fatalf("nodes = %d, want 2", len(definition.Nodes))
	}
	if len(definition.Edges) != 0 {
		t.Fatalf("internal edges = %d, want 0", len(definition.Edges))
	}
	if len(definition.Inputs) != 0 {
		t.Fatalf("inputs = %d, want 0", len(definition.Inputs))
	}
	if len(definition.Outputs) != 2 {
		t.Fatalf("outputs = %d, want 2", len(definition.Outputs))
	}
	for _, output := range definition.Outputs {
		if output.Port.Direction != DirectionOutput || output.Port.Type != PortTypePrompt {
			t.Fatalf("unexpected output mapping: %+v", output)
		}
	}
}

func safeToolsComposite() CompositeDefinition {
	return CompositeDefinition{
		Version: SchemaVersion,
		ID:      "safe-tools",
		Name:    "Safe Tools",
		Outputs: []CompositePortMapping{
			{
				Port:     Port{ID: "toolset_out", Type: PortTypeToolset, Label: "Toolset", Direction: DirectionOutput},
				Endpoint: Endpoint{Node: "readonly-tools", Port: "toolset_out"},
			},
		},
		Nodes: []Node{
			{
				ID:      "readonly-tools",
				Type:    NodeTypeToolset,
				Label:   "Readonly Tools",
				Outputs: []Port{{ID: "toolset_out", Type: PortTypeToolset, Label: "Toolset", Direction: DirectionOutput}},
				Config:  map[string]any{"tools": []string{"read_file", "glob"}},
			},
		},
	}
}

func cycleComposite(next string) CompositeDefinition {
	return CompositeDefinition{
		Version: SchemaVersion,
		ID:      next,
		Name:    next,
		Outputs: []CompositePortMapping{
			{
				Port:     Port{ID: "prompt_out", Type: PortTypePrompt, Label: "Prompt", Direction: DirectionOutput},
				Endpoint: Endpoint{Node: "inner", Port: "prompt_out"},
			},
		},
		Nodes: []Node{
			{
				ID:      "inner",
				Type:    NodeTypeComposite,
				Label:   "Inner",
				Outputs: []Port{{ID: "prompt_out", Type: PortTypePrompt, Label: "Prompt", Direction: DirectionOutput}},
				Config:  map[string]any{"definition": next},
			},
		},
	}
}
