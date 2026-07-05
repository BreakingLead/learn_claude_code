package nodeeditor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultBlueprintValidatesAndResolves(t *testing.T) {
	blueprint := DefaultBlueprint()
	if err := Validate(blueprint); err != nil {
		t.Fatal(err)
	}

	resolved, err := Resolve(blueprint)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ID != "agent-main" {
		t.Fatalf("unexpected agent id: %q", resolved.ID)
	}
	if got := strings.Join(resolved.PromptNodes, ","); got != "project-context,build-mode" {
		t.Fatalf("unexpected prompt order: %s", got)
	}
	if got := strings.Join(resolved.ToolsetNodes, ","); got != "core-tools" {
		t.Fatalf("unexpected toolsets: %s", got)
	}
}

func TestEnsureDefaultBlueprintWritesOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agents", "blueprints", "agents", "default.json")
	created, err := EnsureDefaultBlueprint(path)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("expected default blueprint to be created")
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(original), "Default Bee Agent") {
		t.Fatalf("unexpected default blueprint content: %s", string(original))
	}

	custom := []byte(`{"version":1,"id":"custom","name":"Custom","root_agent":"agent-main","nodes":[],"edges":[]}`)
	if err := os.WriteFile(path, custom, 0o644); err != nil {
		t.Fatal(err)
	}
	created, err = EnsureDefaultBlueprint(path)
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("expected existing blueprint to be preserved")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(custom) {
		t.Fatalf("expected custom blueprint to remain, got %s", string(got))
	}
}

func TestValidateRejectsIncompatiblePorts(t *testing.T) {
	blueprint := DefaultBlueprint()
	blueprint.Edges[0].Target.Port = "toolset_in"
	err := Validate(blueprint)
	if err == nil {
		t.Fatal("expected incompatible port error")
	}
	if !strings.Contains(err.Error(), "connects prompt to toolset") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRejectsDuplicateSingleInput(t *testing.T) {
	blueprint := DefaultBlueprint()
	blueprint.Edges = append(blueprint.Edges, Edge{
		ID:     "edge-duplicate-prompt",
		Source: Endpoint{Node: "build-mode", Port: "prompt_out"},
		Target: Endpoint{Node: "agent-main", Port: "prompt_1"},
	})
	err := Validate(blueprint)
	if err == nil {
		t.Fatal("expected duplicate single-input error")
	}
	if !strings.Contains(err.Error(), "allows only one edge") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateAllowsMultipleToolsets(t *testing.T) {
	blueprint := DefaultBlueprint()
	extra := Node{
		ID:       "extra-tools",
		Type:     NodeTypeToolset,
		Label:    "Extra Tools",
		Position: Position{X: 100, Y: 500},
		Outputs: []Port{
			{ID: "toolset_out", Type: PortTypeToolset, Label: "Toolset", Direction: DirectionOutput},
		},
	}
	blueprint.Nodes = append(blueprint.Nodes, extra)
	blueprint.Edges = append(blueprint.Edges, Edge{
		ID:     "edge-extra-tools",
		Source: Endpoint{Node: "extra-tools", Port: "toolset_out"},
		Target: Endpoint{Node: "agent-main", Port: "toolset_in"},
	})

	resolved, err := Resolve(blueprint)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(resolved.ToolsetNodes, ","); got != "core-tools,extra-tools" {
		t.Fatalf("unexpected toolset nodes: %s", got)
	}
}
