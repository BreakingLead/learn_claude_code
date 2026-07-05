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

func TestExampleAgentBlueprintsValidateAndResolve(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "..", ".agents", "blueprints", "agents", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("expected example agent blueprints")
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			blueprint, err := ReadBlueprint(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := Validate(blueprint); err != nil {
				t.Fatal(err)
			}
			resolved, err := Resolve(blueprint)
			if err != nil {
				t.Fatal(err)
			}
			if resolved.ID == "" {
				t.Fatalf("missing resolved agent for %s", path)
			}
		})
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

func TestEffectiveToolNamesAppliesPolicyNodes(t *testing.T) {
	blueprint := DefaultBlueprint()
	blueprint.Nodes = append(blueprint.Nodes, Node{
		ID:       "readonly-policy",
		Type:     NodeTypePolicy,
		Label:    "Readonly Policy",
		Position: Position{X: 360, Y: 380},
		Inputs: []Port{
			{ID: "toolset_in", Type: PortTypeToolset, Label: "Tools In", Direction: DirectionInput, Multiple: true},
		},
		Outputs: []Port{
			{ID: "toolset_out", Type: PortTypeToolset, Label: "Tools Out", Direction: DirectionOutput},
		},
		Config: map[string]any{
			"allow_tools": []string{"read_file", "glob"},
			"deny_tools":  []string{"glob"},
		},
	})
	blueprint.Edges = []Edge{
		{ID: "edge-tools-policy", Source: Endpoint{Node: "core-tools", Port: "toolset_out"}, Target: Endpoint{Node: "readonly-policy", Port: "toolset_in"}},
		{ID: "edge-policy-agent", Source: Endpoint{Node: "readonly-policy", Port: "toolset_out"}, Target: Endpoint{Node: "agent-main", Port: "toolset_in"}},
		{ID: "edge-project-agent", Source: Endpoint{Node: "project-context", Port: "prompt_out"}, Target: Endpoint{Node: "agent-main", Port: "prompt_1"}},
		{ID: "edge-build-agent", Source: Endpoint{Node: "build-mode", Port: "prompt_out"}, Target: Endpoint{Node: "agent-main", Port: "prompt_2"}},
	}
	resolved, err := Resolve(blueprint)
	if err != nil {
		t.Fatal(err)
	}
	capabilities := EffectiveToolNames(blueprint, resolved)
	if !capabilities.Resolved {
		t.Fatal("expected capabilities to resolve")
	}
	if got := strings.Join(capabilities.ToolNames, ","); got != "read_file" {
		t.Fatalf("unexpected effective tools: %s", got)
	}
	if len(capabilities.Policies) != 1 {
		t.Fatalf("expected one policy application, got %+v", capabilities.Policies)
	}
	policy := capabilities.Policies[0]
	if policy.NodeID != "readonly-policy" {
		t.Fatalf("unexpected policy node: %+v", policy)
	}
	if got := strings.Join(policy.OutputTools, ","); got != "read_file" {
		t.Fatalf("unexpected policy output tools: %+v", policy)
	}
	if !containsNodeID(policy.DroppedTools, "glob") || !containsNodeID(policy.DroppedTools, "write_file") {
		t.Fatalf("expected dropped tools in policy trace: %+v", policy)
	}
}

func TestEffectiveToolNamesReportsPolicyCycles(t *testing.T) {
	blueprint := DefaultBlueprint()
	policy := func(id string, x int) Node {
		return Node{
			ID:       id,
			Type:     NodeTypePolicy,
			Label:    id,
			Position: Position{X: x, Y: 380},
			Inputs: []Port{
				{ID: "toolset_in", Type: PortTypeToolset, Label: "Tools In", Direction: DirectionInput, Multiple: true},
			},
			Outputs: []Port{
				{ID: "toolset_out", Type: PortTypeToolset, Label: "Tools Out", Direction: DirectionOutput},
			},
			Config: map[string]any{"deny_tools": []string{"write_file"}},
		}
	}
	blueprint.Nodes = append(blueprint.Nodes, policy("policy-a", 300), policy("policy-b", 460))
	blueprint.Edges = []Edge{
		{ID: "edge-policy-a-b", Source: Endpoint{Node: "policy-a", Port: "toolset_out"}, Target: Endpoint{Node: "policy-b", Port: "toolset_in"}},
		{ID: "edge-policy-b-a", Source: Endpoint{Node: "policy-b", Port: "toolset_out"}, Target: Endpoint{Node: "policy-a", Port: "toolset_in"}},
		{ID: "edge-policy-agent", Source: Endpoint{Node: "policy-a", Port: "toolset_out"}, Target: Endpoint{Node: "agent-main", Port: "toolset_in"}},
		{ID: "edge-project-agent", Source: Endpoint{Node: "project-context", Port: "prompt_out"}, Target: Endpoint{Node: "agent-main", Port: "prompt_1"}},
		{ID: "edge-build-agent", Source: Endpoint{Node: "build-mode", Port: "prompt_out"}, Target: Endpoint{Node: "agent-main", Port: "prompt_2"}},
	}
	resolved, err := Resolve(blueprint)
	if err != nil {
		t.Fatal(err)
	}
	capabilities := EffectiveToolNames(blueprint, resolved)
	if len(capabilities.Diagnostics) == 0 {
		t.Fatalf("expected cycle diagnostic, got %+v", capabilities)
	}
	if !strings.Contains(capabilities.Diagnostics[0], "tool capability cycle") {
		t.Fatalf("unexpected diagnostics: %+v", capabilities.Diagnostics)
	}
}

func TestPromptPreviewIncludesOrderedPromptAndMemoryNodes(t *testing.T) {
	blueprint := DefaultBlueprint()
	blueprint.Nodes = append(blueprint.Nodes,
		Node{
			ID:       "policy-prompt",
			Type:     NodeTypePolicy,
			Label:    "Plan Policy",
			Position: Position{X: 300, Y: 260},
			Outputs:  []Port{{ID: "prompt_out", Type: PortTypePrompt, Label: "Prompt", Direction: DirectionOutput}},
			Config:   map[string]any{"prompt": "Plan before editing."},
		},
		Node{
			ID:       "memory",
			Type:     NodeTypeMemory,
			Label:    "Memory",
			Position: Position{X: 300, Y: 420},
			Outputs:  []Port{{ID: "memory_out", Type: PortTypeMemory, Label: "Memory", Direction: DirectionOutput}},
			Config:   map[string]any{"source": "default_memory"},
		},
	)
	blueprint.Edges = append(blueprint.Edges,
		Edge{ID: "edge-policy-prompt", Source: Endpoint{Node: "policy-prompt", Port: "prompt_out"}, Target: Endpoint{Node: "agent-main", Port: "prompt_3"}},
		Edge{ID: "edge-memory", Source: Endpoint{Node: "memory", Port: "memory_out"}, Target: Endpoint{Node: "agent-main", Port: "memory_in"}},
	)
	resolved, err := Resolve(blueprint)
	if err != nil {
		t.Fatal(err)
	}
	blocks := PromptPreview(blueprint, resolved)
	if got := len(blocks); got != 4 {
		t.Fatalf("blocks = %d, want 4: %+v", got, blocks)
	}
	if blocks[0].NodeID != "project-context" || blocks[2].NodeID != "policy-prompt" {
		t.Fatalf("unexpected prompt order: %+v", blocks)
	}
	if blocks[2].Source != "blueprint policy" || !strings.Contains(blocks[2].Preview, "Plan before editing") {
		t.Fatalf("unexpected policy preview: %+v", blocks[2])
	}
	if blocks[3].Source != "memory index" {
		t.Fatalf("unexpected memory preview: %+v", blocks[3])
	}
}
