package agent

import (
	"path/filepath"
	"testing"
)

func TestBlueprintPathFromEnvUsesID(t *testing.T) {
	workdir := t.TempDir()
	t.Setenv("BEE_AGENT_BLUEPRINT_ID", "review-agent")
	t.Setenv("BEE_AGENT_BLUEPRINT_PATH", "")

	got := blueprintPathFromEnv(workdir)
	want := filepath.Join(workdir, ".agents", "blueprints", "agents", "review-agent.json")
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestBlueprintPathFromEnvPrefersExplicitPath(t *testing.T) {
	workdir := t.TempDir()
	t.Setenv("BEE_AGENT_BLUEPRINT_ID", "review-agent")
	t.Setenv("BEE_AGENT_BLUEPRINT_PATH", ".agents/blueprints/agents/custom.json")

	got := blueprintPathFromEnv(workdir)
	want := filepath.Join(workdir, ".agents", "blueprints", "agents", "custom.json")
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}
