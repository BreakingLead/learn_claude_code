package agent

import (
	nodeeditor "bee_agent/internal/agent/node_editor"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeLoadsBlueprintSnapshot(t *testing.T) {
	workdir := t.TempDir()
	config := testConfig(workdir)
	if _, err := nodeeditor.EnsureDefaultBlueprint(config.DefaultBlueprintPath); err != nil {
		t.Fatal(err)
	}

	rt := newAgentRuntime(config, nil, nil)
	snapshot, ok := rt.blueprintSnapshot().(map[string]any)
	if !ok {
		t.Fatalf("expected blueprint snapshot, got %#v", rt.blueprintSnapshot())
	}
	if snapshot["graph"] != "default" || snapshot["root"] != "agent-main" {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
}

func TestRuntimeUsesBlueprintToolsetWhenEnabled(t *testing.T) {
	workdir := t.TempDir()
	config := testConfig(workdir)
	config.UseBlueprint = true
	blueprint := nodeeditor.DefaultBlueprint()
	blueprint.Nodes = append(blueprint.Nodes, nodeeditor.Node{
		ID:       "task-tools",
		Type:     nodeeditor.NodeTypeToolset,
		Label:    "Task Tools",
		Position: nodeeditor.Position{X: 80, Y: 500},
		Outputs: []nodeeditor.Port{
			{ID: "toolset_out", Type: nodeeditor.PortTypeToolset, Label: "Toolset", Direction: nodeeditor.DirectionOutput},
		},
		Config: map[string]any{"tools": []string{"todo_write"}},
	})
	blueprint.Edges = append(blueprint.Edges, nodeeditor.Edge{
		ID:     "edge-task-tools",
		Source: nodeeditor.Endpoint{Node: "task-tools", Port: "toolset_out"},
		Target: nodeeditor.Endpoint{Node: "agent-main", Port: "toolset_in"},
	})
	if err := nodeeditor.WriteBlueprint(config.DefaultBlueprintPath, blueprint); err != nil {
		t.Fatal(err)
	}

	rt := newAgentRuntime(config, nil, nil)
	spec := rt.mainAgentSpec()
	got := strings.Join(spec.ToolNames, ",")
	for _, want := range []string{"bash", "edit_file", "glob", "read_file", "todo_write", "write_file"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %s in blueprint tool names: %s", want, got)
		}
	}
	if strings.Contains(got, "task_create") {
		t.Fatalf("unexpected non-blueprint tool in tool names: %s", got)
	}
}

func TestRuntimeExpandsCompositeToolsetWhenEnabled(t *testing.T) {
	workdir := t.TempDir()
	config := testConfig(workdir)
	config.UseBlueprint = true
	store := nodeeditor.NewStore(workdir)
	if err := store.WriteComposite("safe-tools", nodeeditor.CompositeDefinition{
		Version: nodeeditor.SchemaVersion,
		ID:      "safe-tools",
		Name:    "Safe Tools",
		Outputs: []nodeeditor.CompositePortMapping{
			{
				Port:     nodeeditor.Port{ID: "toolset_out", Type: nodeeditor.PortTypeToolset, Label: "Toolset", Direction: nodeeditor.DirectionOutput},
				Endpoint: nodeeditor.Endpoint{Node: "readonly-tools", Port: "toolset_out"},
			},
		},
		Nodes: []nodeeditor.Node{
			{
				ID:      "readonly-tools",
				Type:    nodeeditor.NodeTypeToolset,
				Label:   "Readonly Tools",
				Outputs: []nodeeditor.Port{{ID: "toolset_out", Type: nodeeditor.PortTypeToolset, Label: "Toolset", Direction: nodeeditor.DirectionOutput}},
				Config:  map[string]any{"tools": []string{"read_file", "glob"}},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	blueprint := nodeeditor.DefaultBlueprint()
	blueprint.Nodes = append(blueprint.Nodes, nodeeditor.Node{
		ID:      "safe-tools-node",
		Type:    nodeeditor.NodeTypeComposite,
		Label:   "Safe Tools",
		Outputs: []nodeeditor.Port{{ID: "toolset_out", Type: nodeeditor.PortTypeToolset, Label: "Toolset", Direction: nodeeditor.DirectionOutput}},
		Config:  map[string]any{"definition": "safe-tools"},
	})
	blueprint.Edges = append(blueprint.Edges, nodeeditor.Edge{
		ID:     "edge-safe-tools-node",
		Source: nodeeditor.Endpoint{Node: "safe-tools-node", Port: "toolset_out"},
		Target: nodeeditor.Endpoint{Node: "agent-main", Port: "toolset_in"},
	})
	if err := nodeeditor.WriteBlueprint(config.DefaultBlueprintPath, blueprint); err != nil {
		t.Fatal(err)
	}

	rt := newAgentRuntime(config, nil, nil)
	got := strings.Join(rt.mainAgentSpec().ToolNames, ",")
	if !strings.Contains(got, "read_file") || !strings.Contains(got, "glob") {
		t.Fatalf("expected composite tools in %s", got)
	}
}

func TestRuntimeUsesBlueprintPromptOrderWhenEnabled(t *testing.T) {
	workdir := t.TempDir()
	writeFile(t, filepath.Join(workdir, "AGENTS.md"), "# Guide\nFollow project rules.")
	writeFile(t, filepath.Join(workdir, "README.md"), "# Readme\nProject details.")
	config := testConfig(workdir)
	config.UseBlueprint = true
	if _, err := nodeeditor.EnsureDefaultBlueprint(config.DefaultBlueprintPath); err != nil {
		t.Fatal(err)
	}

	rt := newAgentRuntime(config, nil, nil)
	ctx := rt.promptContext([]string{"bash", "read_file"})
	if len(ctx.PromptBlocks) < 2 {
		t.Fatalf("expected blueprint prompt blocks, got %#v", ctx.PromptBlocks)
	}
	if ctx.PromptBlocks[0].Module != "project-context" {
		t.Fatalf("expected project context first, got %#v", ctx.PromptBlocks[0])
	}
	if ctx.PromptBlocks[len(ctx.PromptBlocks)-1].Module != "build-mode" {
		t.Fatalf("expected active mode block last, got %#v", ctx.PromptBlocks)
	}
}

func TestRuntimeUsesFileBackedSkillNodeWhenEnabled(t *testing.T) {
	workdir := t.TempDir()
	writeFile(t, filepath.Join(workdir, ".agents", "skills", "reviewer", "SKILL.md"), "# Reviewer\nCheck edge cases.")
	config := testConfig(workdir)
	config.UseBlueprint = true
	blueprint := nodeeditor.DefaultBlueprint()
	blueprint.Nodes = append(blueprint.Nodes, nodeeditor.Node{
		ID:       "reviewer-skill",
		Type:     "skill",
		Label:    "Reviewer Skill",
		Position: nodeeditor.Position{X: 120, Y: 520},
		Outputs: []nodeeditor.Port{
			{ID: "prompt_out", Type: nodeeditor.PortTypePrompt, Label: "Prompt", Direction: nodeeditor.DirectionOutput},
		},
		Config: map[string]any{
			"source": "skill_file",
			"path":   ".agents/skills/reviewer/SKILL.md",
		},
	})
	blueprint.Nodes[0].Inputs = append(blueprint.Nodes[0].Inputs, nodeeditor.Port{
		ID:        "prompt_4",
		Type:      nodeeditor.PortTypePrompt,
		Label:     "Prompt 4",
		Direction: nodeeditor.DirectionInput,
		Order:     4,
	})
	blueprint.Edges = append(blueprint.Edges, nodeeditor.Edge{
		ID:     "edge-reviewer-skill",
		Source: nodeeditor.Endpoint{Node: "reviewer-skill", Port: "prompt_out"},
		Target: nodeeditor.Endpoint{Node: "agent-main", Port: "prompt_4"},
	})
	if err := nodeeditor.WriteBlueprint(config.DefaultBlueprintPath, blueprint); err != nil {
		t.Fatal(err)
	}

	rt := newAgentRuntime(config, nil, nil)
	prompt := rt.getSystemPrompt([]string{"read_file"})
	if !strings.Contains(prompt, "Check edge cases.") {
		t.Fatalf("expected skill file content in prompt: %s", prompt)
	}
}

func TestRuntimeFallsBackWhenBlueprintDisabled(t *testing.T) {
	workdir := t.TempDir()
	config := testConfig(workdir)
	config.UseBlueprint = false
	if _, err := nodeeditor.EnsureDefaultBlueprint(config.DefaultBlueprintPath); err != nil {
		t.Fatal(err)
	}

	rt := newAgentRuntime(config, nil, nil)
	spec := rt.mainAgentSpec()
	got := strings.Join(spec.ToolNames, ",")
	if !strings.Contains(got, "task_create") {
		t.Fatalf("expected existing module tool when blueprint is disabled: %s", got)
	}
}
