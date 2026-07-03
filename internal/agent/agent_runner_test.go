package agent

import "testing"

func TestSubagentSpecRestrictsTools(t *testing.T) {
	rt := newAgentRuntime(testConfig(t.TempDir()), nil, nil)
	tools := filterToolsByName(rt.buildTools(), rt.subagentSpec().ToolNames)
	names := toolNames(tools)

	if hasString(names, "task") {
		t.Fatalf("subagent must not expose recursive task tool: %v", names)
	}
	if hasString(names, "background_bash") {
		t.Fatalf("subagent must not expose background tools: %v", names)
	}
	for _, want := range []string{"bash", "read_file", "write_file", "edit_file", "glob"} {
		if !hasString(names, want) {
			t.Fatalf("subagent missing tool %q in %v", want, names)
		}
	}
}

func TestMainAgentToolsIncludeTodoModuleTool(t *testing.T) {
	rt := newAgentRuntime(testConfig(t.TempDir()), nil, nil)
	names := toolNames(rt.buildTools())

	if !hasString(names, "todo_write") {
		t.Fatalf("main agent tools should include todo module tool: %v", names)
	}
}

func TestFilterToolHandlersKeepsOnlyRequestedNames(t *testing.T) {
	rt := newAgentRuntime(testConfig(t.TempDir()), nil, nil)
	handlers := filterToolHandlers(rt.toolHandlers(), []string{"read_file", "glob"})

	if _, ok := handlers["read_file"]; !ok {
		t.Fatal("expected read_file handler")
	}
	if _, ok := handlers["glob"]; !ok {
		t.Fatal("expected glob handler")
	}
	if _, ok := handlers["bash"]; ok {
		t.Fatal("did not expect bash handler")
	}
}
