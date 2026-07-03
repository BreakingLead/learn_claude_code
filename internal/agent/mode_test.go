package agent

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanModeRestrictsWriteToolsAndInjectsPrompt(t *testing.T) {
	config := testConfig(t.TempDir())
	rt := newAgentRuntime(config, nil, nil)

	if err := rt.switchMode("plan"); err != nil {
		t.Fatal(err)
	}
	names := rt.mainAgentSpec().ToolNames
	for _, blocked := range []string{"bash", "write_file", "edit_file", "task", "task_create", "background_bash", "schedule_cron"} {
		if hasString(names, blocked) {
			t.Fatalf("plan mode should not expose %q in %v", blocked, names)
		}
	}
	for _, allowed := range []string{"read_file", "glob"} {
		if !hasString(names, allowed) {
			t.Fatalf("plan mode should expose %q in %v", allowed, names)
		}
	}
	prompt := rt.getSystemPrompt(names)
	if !strings.Contains(prompt, "Mode: plan") || !strings.Contains(prompt, "Do not modify files") {
		t.Fatalf("plan mode prompt missing from system prompt: %s", prompt)
	}
}

func TestModeConfigLoadsCustomModeAndSwitches(t *testing.T) {
	workdir := t.TempDir()
	writeFile(t, filepath.Join(workdir, ".agents", "modes.json"), `{
  "default": "review",
  "modes": [
    {
      "name": "review",
      "description": "read-only review mode",
      "prompt": "Review only. Report findings first.",
      "tools": ["read_file", "glob", "bash"]
    }
  ]
}`)
	rt := newAgentRuntime(testConfig(workdir), nil, nil)

	if rt.activeMode().Name != "review" {
		t.Fatalf("expected custom default mode, got %q", rt.activeMode().Name)
	}
	names := rt.mainAgentSpec().ToolNames
	if !hasString(names, "bash") || hasString(names, "write_file") {
		t.Fatalf("custom mode tool filter not applied: %v", names)
	}
	if prompt := rt.getSystemPrompt(names); !strings.Contains(prompt, "Review only. Report findings first.") {
		t.Fatalf("custom mode prompt missing: %s", prompt)
	}

	if err := rt.switchMode("build"); err != nil {
		t.Fatal(err)
	}
	if !hasString(rt.mainAgentSpec().ToolNames, "write_file") {
		t.Fatalf("build mode should restore write tools: %v", rt.mainAgentSpec().ToolNames)
	}
}

func TestModeConfigDisableToolsFiltersBuildMode(t *testing.T) {
	workdir := t.TempDir()
	writeFile(t, filepath.Join(workdir, ".agents", "modes.json"), `{
  "default": "safe-build",
  "modes": [
    {
      "name": "safe-build",
      "description": "build without shell",
      "prompt": "Build without shell commands.",
      "disable_tools": ["bash", "background_bash"]
    }
  ]
}`)
	rt := newAgentRuntime(testConfig(workdir), nil, nil)
	names := rt.mainAgentSpec().ToolNames

	if hasString(names, "bash") || hasString(names, "background_bash") {
		t.Fatalf("disable_tools should remove shell tools: %v", names)
	}
	if !hasString(names, "write_file") {
		t.Fatalf("mode without explicit tools should keep other build tools: %v", names)
	}
}
