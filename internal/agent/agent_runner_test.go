package agent

import "testing"

func TestSubagentSpecRestrictsTools(t *testing.T) {
	rt := newAgentRuntime(testConfig(t.TempDir()), nil, nil)
	tools := filterToolsByName(buildTools(), rt.subagentSpec().ToolNames)
	names := toolNames(tools)

	if containsString(names, "task") {
		t.Fatalf("subagent must not expose recursive task tool: %v", names)
	}
	if containsString(names, "background_bash") {
		t.Fatalf("subagent must not expose background tools: %v", names)
	}
	for _, want := range []string{"bash", "read_file", "write_file", "edit_file", "glob"} {
		if !containsString(names, want) {
			t.Fatalf("subagent missing tool %q in %v", want, names)
		}
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

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
