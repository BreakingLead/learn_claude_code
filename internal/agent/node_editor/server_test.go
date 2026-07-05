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
	var validation BlueprintValidationResponse
	if err := json.NewDecoder(resp.Body).Decode(&validation); err != nil {
		t.Fatal(err)
	}
	if !validation.OK {
		t.Fatal("expected valid blueprint")
	}
	if !validation.Capabilities.Resolved || len(validation.Capabilities.ToolNames) == 0 {
		t.Fatalf("expected validation capabilities, got %+v", validation.Capabilities)
	}
	if validation.Expanded.ID != "default" || validation.Resolved.ID != "agent-main" {
		t.Fatalf("expected expanded/resolved validation payload, got %+v", validation)
	}
	if len(validation.PromptBlocks) == 0 || validation.PromptBlocks[0].NodeID != "project-context" {
		t.Fatalf("expected prompt preview blocks, got %+v", validation.PromptBlocks)
	}
	if len(validation.Diagnostics) != 0 {
		t.Fatalf("expected no default config diagnostics, got %+v", validation.Diagnostics)
	}
	if validation.Runtime.ID != "default" || !strings.Contains(validation.Runtime.Command, "BEE_AGENT_BLUEPRINT_ID=default") {
		t.Fatalf("expected runtime selector, got %+v", validation.Runtime)
	}
}

func TestServerCreateBlueprintAPI(t *testing.T) {
	workdir := t.TempDir()
	path := DefaultBlueprintPath(workdir)
	if _, err := EnsureDefaultBlueprint(path); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewServer(workdir).Handler())
	defer server.Close()

	request := CreateBlueprintRequest{
		ID:       "review-agent",
		Name:     "Review Agent",
		SourceID: "default",
	}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(server.URL+"/api/blueprints", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	var payload struct {
		OK        bool      `json:"ok"`
		Blueprint Blueprint `json:"blueprint"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if !payload.OK || payload.Blueprint.ID != "review-agent" || payload.Blueprint.Name != "Review Agent" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	stored, err := NewStore(workdir).ReadAgent("review-agent")
	if err != nil {
		t.Fatal(err)
	}
	if stored.RootAgent != "agent-main" {
		t.Fatalf("unexpected stored blueprint: %+v", stored)
	}
}

func TestStoreValidateBlueprintForRuntime(t *testing.T) {
	store := NewStore(t.TempDir())
	response := store.ValidateBlueprintForRuntime(DefaultBlueprint())
	if !response.OK {
		t.Fatalf("expected valid blueprint, got %s", response.Error)
	}
	if response.Runtime.ID != "default" {
		t.Fatalf("unexpected runtime selector: %+v", response.Runtime)
	}
	if !response.Capabilities.Resolved || !containsNodeID(response.Capabilities.ToolNames, "read_file") {
		t.Fatalf("unexpected capabilities: %+v", response.Capabilities)
	}
	if len(response.PromptBlocks) == 0 {
		t.Fatalf("expected prompt preview blocks, got %+v", response.PromptBlocks)
	}
	if len(response.Diagnostics) != 0 {
		t.Fatalf("expected no default config diagnostics, got %+v", response.Diagnostics)
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
	for _, want := range []string{"agent", "prompt", "skill", "toolset", "memory", "policy"} {
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

func TestServerCompositeFromSelectionAPI(t *testing.T) {
	workdir := t.TempDir()
	server := httptest.NewServer(NewServer(workdir).Handler())
	defer server.Close()

	request := CompositeFromSelectionRequest{
		Blueprint: DefaultBlueprint(),
		NodeIDs:   []string{"core-tools"},
		ID:        "readonly-pack",
		Name:      "Readonly Pack",
	}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(server.URL+"/api/composites/from-selection", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var payload struct {
		OK        bool                `json:"ok"`
		Composite CompositeDefinition `json:"composite"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if !payload.OK || payload.Composite.ID != "readonly-pack" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	stored, err := NewStore(workdir).LoadComposite("readonly-pack")
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Outputs) != 1 || stored.Outputs[0].Port.Type != PortTypeToolset {
		t.Fatalf("unexpected stored composite: %+v", stored)
	}
}

func TestServerWorkflowAPI(t *testing.T) {
	workdir := t.TempDir()
	if _, err := EnsureDefaultBlueprint(DefaultBlueprintPath(workdir)); err != nil {
		t.Fatal(err)
	}
	store := NewStore(workdir)
	if err := store.WriteWorkflow("review-pipeline", DefaultWorkflow()); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewServer(workdir).Handler())
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/workflows")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list workflow status = %d", resp.StatusCode)
	}
	var list struct {
		Workflows []WorkflowSummary `json:"workflows"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if len(list.Workflows) != 1 || list.Workflows[0].ID != "review-pipeline" {
		t.Fatalf("unexpected workflow list: %+v", list)
	}

	resp, err = http.Get(server.URL + "/api/workflows/review-pipeline")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var workflow WorkflowDefinition
	if err := json.NewDecoder(resp.Body).Decode(&workflow); err != nil {
		t.Fatal(err)
	}
	if workflow.ID != "review-pipeline" {
		t.Fatalf("unexpected workflow: %+v", workflow)
	}

	raw, err := json.Marshal(workflow)
	if err != nil {
		t.Fatal(err)
	}
	resp, err = http.Post(server.URL+"/api/workflows/review-pipeline/validate", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var validation WorkflowValidationResponse
	if err := json.NewDecoder(resp.Body).Decode(&validation); err != nil {
		t.Fatal(err)
	}
	if !validation.OK || len(validation.Order) != len(workflow.Nodes) {
		t.Fatalf("unexpected workflow validation: %+v", validation)
	}
	if len(validation.Steps) != len(workflow.Nodes) || validation.Steps[1].NodeID == "" {
		t.Fatalf("unexpected workflow steps: %+v", validation.Steps)
	}
	if len(validation.Agents) != 3 || validation.Agents[0].BlueprintID != "default" {
		t.Fatalf("unexpected workflow agent resolutions: %+v", validation.Agents)
	}

	workflow.Name = "Updated Review Pipeline"
	raw, err = json.Marshal(workflow)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPut, server.URL+"/api/workflows/review-pipeline", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put workflow status = %d", resp.StatusCode)
	}
	stored, err := store.ReadWorkflow("review-pipeline")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Name != "Updated Review Pipeline" {
		t.Fatalf("unexpected stored workflow: %+v", stored)
	}
}

func TestServerWorkflowValidationRejectsMissingAgentBlueprint(t *testing.T) {
	workdir := t.TempDir()
	if _, err := EnsureDefaultBlueprint(DefaultBlueprintPath(workdir)); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewServer(workdir).Handler())
	defer server.Close()

	workflow := DefaultWorkflow()
	workflow.Nodes[1].AgentBlueprint = "missing-agent"
	raw, err := json.Marshal(workflow)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(server.URL+"/api/workflows/review-pipeline/validate", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var validation WorkflowValidationResponse
	if err := json.NewDecoder(resp.Body).Decode(&validation); err != nil {
		t.Fatal(err)
	}
	if validation.OK {
		t.Fatalf("expected missing blueprint validation error, got %+v", validation)
	}
	if !strings.Contains(validation.Error, "missing blueprint") {
		t.Fatalf("unexpected validation error: %+v", validation)
	}
}

func TestServerWorkflowSimulationAPI(t *testing.T) {
	workdir := t.TempDir()
	if _, err := EnsureDefaultBlueprint(DefaultBlueprintPath(workdir)); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewServer(workdir).Handler())
	defer server.Close()

	request := WorkflowSimulationRequest{
		Workflow: DefaultWorkflow(),
		Input:    "implement auth",
	}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(server.URL+"/api/workflows/review-pipeline/simulate", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("simulate status = %d", resp.StatusCode)
	}
	var payload WorkflowSimulationResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if !payload.OK || len(payload.Steps) != len(DefaultWorkflow().Nodes) {
		t.Fatalf("unexpected simulation payload: %+v", payload)
	}
	developer := workflowSimulationStepByNodeID(payload.Steps, "developer")
	if len(developer.Inputs) != 1 || developer.Inputs[0].Content != "implement auth" {
		t.Fatalf("unexpected developer simulation: %+v", developer)
	}
}

func TestServerCreateWorkflowAPI(t *testing.T) {
	workdir := t.TempDir()
	if _, err := EnsureDefaultBlueprint(DefaultBlueprintPath(workdir)); err != nil {
		t.Fatal(err)
	}
	store := NewStore(workdir)
	if err := store.WriteWorkflow("review-pipeline", DefaultWorkflow()); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewServer(workdir).Handler())
	defer server.Close()

	request := CreateBlueprintRequest{
		ID:       "qa-pipeline",
		Name:     "QA Pipeline",
		SourceID: "review-pipeline",
	}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(server.URL+"/api/workflows", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	var payload struct {
		OK       bool               `json:"ok"`
		Workflow WorkflowDefinition `json:"workflow"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if !payload.OK || payload.Workflow.ID != "qa-pipeline" || payload.Workflow.Name != "QA Pipeline" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	stored, err := store.ReadWorkflow("qa-pipeline")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Metadata["copied_from"] != "review-pipeline" {
		t.Fatalf("unexpected workflow metadata: %+v", stored.Metadata)
	}
}

func TestServerDeleteWorkflowAPI(t *testing.T) {
	workdir := t.TempDir()
	if _, err := EnsureDefaultBlueprint(DefaultBlueprintPath(workdir)); err != nil {
		t.Fatal(err)
	}
	store := NewStore(workdir)
	if err := store.WriteWorkflow("review-pipeline", DefaultWorkflow()); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewServer(workdir).Handler())
	defer server.Close()

	req, err := http.NewRequest(http.MethodDelete, server.URL+"/api/workflows/review-pipeline", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete workflow status = %d", resp.StatusCode)
	}
	if _, err := store.ReadWorkflow("review-pipeline"); err == nil {
		t.Fatal("expected workflow to be deleted")
	}
}

func TestServerPutWorkflowValidatesRouteID(t *testing.T) {
	workdir := t.TempDir()
	server := httptest.NewServer(NewServer(workdir).Handler())
	defer server.Close()

	workflow := DefaultWorkflow()
	workflow.ID = "other"
	raw, err := json.Marshal(workflow)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPut, server.URL+"/api/workflows/review-pipeline", bytes.NewReader(raw))
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
