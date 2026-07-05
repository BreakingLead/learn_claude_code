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
