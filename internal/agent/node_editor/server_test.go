package nodeeditor

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestServerBlueprintAPI(t *testing.T) {
	workdir := t.TempDir()
	path := DefaultBlueprintPath(workdir)
	if _, err := EnsureDefaultBlueprint(path); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewServer(workdir).Handler())
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/blueprints")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d", resp.StatusCode)
	}
	var list struct {
		Blueprints []BlueprintSummary `json:"blueprints"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if len(list.Blueprints) != 1 || list.Blueprints[0].ID != "default" {
		t.Fatalf("unexpected list response: %+v", list)
	}

	resp, err = http.Get(server.URL + "/api/blueprints/default")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var blueprint Blueprint
	if err := json.NewDecoder(resp.Body).Decode(&blueprint); err != nil {
		t.Fatal(err)
	}
	if blueprint.RootAgent != "agent-main" {
		t.Fatalf("unexpected root agent: %q", blueprint.RootAgent)
	}

	raw, err := json.Marshal(blueprint)
	if err != nil {
		t.Fatal(err)
	}
	resp, err = http.Post(server.URL+"/api/blueprints/default/validate", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var validation struct {
		OK bool `json:"ok"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&validation); err != nil {
		t.Fatal(err)
	}
	if !validation.OK {
		t.Fatal("expected valid blueprint")
	}
}

func TestServerNodeTemplatesAPI(t *testing.T) {
	workdir := t.TempDir()
	store := NewStore(workdir)
	if err := store.WriteComposite("safe-tools", safeToolsComposite()); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewServer(workdir).Handler())
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/node-templates")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("templates status = %d", resp.StatusCode)
	}
	var payload struct {
		Templates []NodeTemplate `json:"templates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Templates) < 4 {
		t.Fatalf("expected builtin templates, got %+v", payload.Templates)
	}
	seen := map[string]bool{}
	for _, template := range payload.Templates {
		seen[template.Type] = true
	}
	for _, want := range []string{"prompt", "skill", "toolset", "memory"} {
		if !seen[want] {
			t.Fatalf("missing template %q in %+v", want, payload.Templates)
		}
	}
	if !seen[NodeTypeComposite] {
		t.Fatalf("missing composite template in %+v", payload.Templates)
	}
}

func TestServerCompositeAPI(t *testing.T) {
	workdir := t.TempDir()
	server := httptest.NewServer(NewServer(workdir).Handler())
	defer server.Close()

	raw, err := json.Marshal(safeToolsComposite())
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPut, server.URL+"/api/composites/safe-tools", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put composite status = %d", resp.StatusCode)
	}

	resp, err = http.Get(server.URL + "/api/composites")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var list struct {
		Composites []CompositeSummary `json:"composites"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if len(list.Composites) != 1 || list.Composites[0].ID != "safe-tools" {
		t.Fatalf("unexpected composite list: %+v", list)
	}
}

func TestServerPutBlueprintValidatesRouteID(t *testing.T) {
	workdir := t.TempDir()
	server := httptest.NewServer(NewServer(workdir).Handler())
	defer server.Close()

	blueprint := DefaultBlueprint()
	blueprint.ID = "other"
	raw, err := json.Marshal(blueprint)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPut, server.URL+"/api/blueprints/default", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestServerServesWebShell(t *testing.T) {
	server := httptest.NewServer(NewServer(t.TempDir()).Handler())
	defer server.Close()

	resp, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Bee Agent Builder") {
		t.Fatalf("unexpected shell from %s: %s", filepath.Base(resp.Request.URL.Path), buf.String())
	}
}
