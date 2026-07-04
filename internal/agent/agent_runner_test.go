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

func TestMainAgentToolsIncludeFileLocalDefinitions(t *testing.T) {
	rt := newAgentRuntime(testConfig(t.TempDir()), nil, nil)
	names := toolNames(rt.buildTools())
	handlers := rt.toolHandlers()

	for _, want := range []string{
		"task",
		"load_skill",
		"task_create",
		"task_list",
		"task_get",
		"task_claim",
		"task_complete",
		"background_bash",
		"background_status",
		"background_list",
		"schedule_cron",
		"list_crons",
		"cancel_cron",
	} {
		if !hasString(names, want) {
			t.Fatalf("main agent tools missing %q in %v", want, names)
		}
		if handlers[want] == nil {
			t.Fatalf("main agent handlers missing %q", want)
		}
	}
}

func TestRuntimeSnapshotsComeFromModules(t *testing.T) {
	rt := newAgentRuntime(testConfig(t.TempDir()), nil, nil)
	snapshots := rt.modules.runtimeSnapshots()

	for _, want := range []string{"project", "skills", "todo", "memory", "subagent", "task_system", "team", "background", "cron"} {
		if _, ok := snapshots[want]; !ok {
			t.Fatalf("missing module snapshot %q in %#v", want, snapshots)
		}
	}
}

func TestDisabledModulesRemoveToolsSnapshotsAndLifecycle(t *testing.T) {
	config := testConfig(t.TempDir())
	config.DisabledModules = map[string]bool{
		"skills":      true,
		"todo":        true,
		"memory":      true,
		"subagent":    true,
		"task_system": true,
		"team":        true,
		"background":  true,
		"cron":        true,
	}
	rt := newAgentRuntime(config, nil, nil)
	names := toolNames(rt.buildTools())
	snapshots := rt.modules.runtimeSnapshots()
	spec := rt.mainAgentSpec()

	for _, disabledTool := range []string{"load_skill", "todo_write", "task", "task_create", "team_send_message", "background_bash", "schedule_cron"} {
		if hasString(names, disabledTool) {
			t.Fatalf("disabled module tool %q should not be exposed in %v", disabledTool, names)
		}
	}
	for _, disabledModule := range []string{"skills", "todo", "memory", "subagent", "task_system", "team", "background", "cron"} {
		if _, ok := snapshots[disabledModule]; ok {
			t.Fatalf("disabled module %q should not expose snapshot: %#v", disabledModule, snapshots)
		}
	}
	if rt.background != nil || rt.cron != nil {
		t.Fatalf("disabled background/cron should not initialize registries")
	}
	if spec.InjectRelevantMemories || spec.ExtractMemoriesOnStop || spec.InjectBackgroundNotifications {
		t.Fatalf("disabled lifecycle modules should turn off main agent injections: %+v", spec)
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
