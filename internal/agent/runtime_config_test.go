package agent

import (
	"path/filepath"
	"testing"
)

func TestBlueprintPathFromOptionsUsesID(t *testing.T) {
	workdir := t.TempDir()

	got := blueprintPathFromOptions(workdir, RunOptions{BlueprintID: "review-agent"})
	want := filepath.Join(workdir, ".agents", "blueprints", "agents", "review-agent.json")
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestBlueprintPathFromOptionsPrefersExplicitPath(t *testing.T) {
	workdir := t.TempDir()

	got := blueprintPathFromOptions(workdir, RunOptions{
		BlueprintID:   "review-agent",
		BlueprintPath: ".agents/blueprints/agents/custom.json",
	})
	want := filepath.Join(workdir, ".agents", "blueprints", "agents", "custom.json")
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}
