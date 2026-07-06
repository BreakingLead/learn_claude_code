package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStartupConfigNeededRequiresAPIKeyAndModels(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("MODEL", "")
	t.Setenv("FALLBACK_MODEL", "")
	if !startupConfigNeeded() {
		t.Fatal("expected startup config when required env is missing")
	}

	t.Setenv("ANTHROPIC_API_KEY", "key")
	t.Setenv("MODEL", "model")
	t.Setenv("FALLBACK_MODEL", "fallback")
	if startupConfigNeeded() {
		t.Fatal("did not expect startup config when required env is present")
	}
}

func TestWriteDotenvValuesUpdatesAndAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	initial := strings.Join([]string{
		"# keep comments",
		"ANTHROPIC_API_KEY=old",
		"ANTHROPIC_BASE_URL=https://old.example",
		"MODEL=old-model",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(initial), 0600); err != nil {
		t.Fatal(err)
	}

	values := map[string]string{
		"ANTHROPIC_API_KEY":  "new key",
		"MODEL":              "new-model",
		"FALLBACK_MODEL":     "new-model",
		"ANTHROPIC_BASE_URL": "",
	}
	if err := writeDotenvValues(path, values); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	for _, want := range []string{
		"# keep comments",
		`ANTHROPIC_API_KEY="new key"`,
		"MODEL=new-model",
		"FALLBACK_MODEL=new-model",
		`ANTHROPIC_BASE_URL=""`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected .env to contain %q, got:\n%s", want, got)
		}
	}
}

func TestStartupConfigValuesUsesModelAsFallback(t *testing.T) {
	m := newStartupConfigModel()
	m.apiKey.SetValue("key")
	m.model.SetValue("model")
	values := m.values()
	if values["MODEL"] != "model" || values["FALLBACK_MODEL"] != "model" {
		t.Fatalf("unexpected model values: %#v", values)
	}
	if values["ANTHROPIC_API_KEY"] != "key" {
		t.Fatalf("expected api key to be captured: %#v", values)
	}
}
