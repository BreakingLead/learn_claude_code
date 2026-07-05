package nodeeditor

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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
	if !containsNodeID(validation.Agents[0].ToolNames, "bash") {
		t.Fatalf("expected workflow agent tool summary, got %+v", validation.Agents[0])
	}
	if len(validation.Agents[0].PromptBlocks) == 0 {
		t.Fatalf("expected workflow agent prompt summary, got %+v", validation.Agents[0])
	}
	if len(validation.Diagnostics) != 0 {
		t.Fatalf("unexpected workflow diagnostics: %+v", validation.Diagnostics)
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

func TestServerWorkflowCompileAPI(t *testing.T) {
	workdir := t.TempDir()
	if _, err := EnsureDefaultBlueprint(DefaultBlueprintPath(workdir)); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewServer(workdir).Handler())
	defer server.Close()

	raw, err := json.Marshal(DefaultWorkflow())
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(server.URL+"/api/workflows/review-pipeline/compile", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("compile status = %d", resp.StatusCode)
	}
	var payload WorkflowCompileResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if !payload.OK || payload.Plan.WorkflowID != "review-pipeline" {
		t.Fatalf("unexpected compile payload: %+v", payload)
	}
	if payload.Plan.SourceHash == "" {
		t.Fatalf("expected source hash in compiled plan: %+v", payload.Plan)
	}
	if len(payload.Plan.AgentRuns) != 3 {
		t.Fatalf("expected three compiled agent runs, got %+v", payload.Plan.AgentRuns)
	}
	developer := payload.Plan.AgentRuns[0]
	if developer.NodeID != "developer" || !containsNodeID(developer.ToolNames, "bash") {
		t.Fatalf("unexpected developer run: %+v", developer)
	}
	if len(developer.PromptBlocks) == 0 || !strings.Contains(developer.Instruction, "Implement the requested change") {
		t.Fatalf("expected compiled prompt and instruction, got %+v", developer)
	}
	if len(payload.Plan.Outputs) != 1 || payload.Plan.Outputs[0].NodeID != "output" {
		t.Fatalf("unexpected compiled outputs: %+v", payload.Plan.Outputs)
	}
}

func TestServerSaveCompiledWorkflowPlanAPI(t *testing.T) {
	workdir := t.TempDir()
	if _, err := EnsureDefaultBlueprint(DefaultBlueprintPath(workdir)); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewServer(workdir).Handler())
	defer server.Close()

	raw, err := json.Marshal(DefaultWorkflow())
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(server.URL+"/api/workflows/review-pipeline/compiled-plan", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("save compiled plan status = %d", resp.StatusCode)
	}
	var payload WorkflowCompiledPlanSaveResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if !payload.OK || payload.Path == "" || payload.Plan.WorkflowID != "review-pipeline" {
		t.Fatalf("unexpected save payload: %+v", payload)
	}
	if payload.Plan.SourceHash == "" {
		t.Fatalf("expected source hash in save payload: %+v", payload.Plan)
	}
	rawPlan, err := os.ReadFile(payload.Path)
	if err != nil {
		t.Fatal(err)
	}
	var stored WorkflowCompiledPlan
	if err := json.Unmarshal(rawPlan, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.WorkflowID != "review-pipeline" || len(stored.AgentRuns) != 3 {
		t.Fatalf("unexpected stored compiled plan: %+v", stored)
	}
	if stored.SourceHash != payload.Plan.SourceHash {
		t.Fatalf("stored source hash = %q, want %q", stored.SourceHash, payload.Plan.SourceHash)
	}
}

func TestServerWorkflowPlanListAndGetAPI(t *testing.T) {
	workdir := t.TempDir()
	if _, err := EnsureDefaultBlueprint(DefaultBlueprintPath(workdir)); err != nil {
		t.Fatal(err)
	}
	store := NewStore(workdir)
	workflow := DefaultWorkflow()
	if err := store.WriteWorkflow("review-pipeline", workflow); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.SaveCompiledWorkflowPlan(workflow); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewServer(workdir).Handler())
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/workflow-plans")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list workflow plans status = %d", resp.StatusCode)
	}
	var list struct {
		Plans []WorkflowPlanSummary `json:"plans"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if len(list.Plans) != 1 || list.Plans[0].ID != "review-pipeline" {
		t.Fatalf("unexpected workflow plan list: %+v", list)
	}
	if list.Plans[0].SourceHash == "" || list.Plans[0].CurrentHash == "" || list.Plans[0].Stale {
		t.Fatalf("expected fresh workflow plan summary: %+v", list.Plans[0])
	}

	resp, err = http.Get(server.URL + "/api/workflow-plans/review-pipeline")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var plan WorkflowCompiledPlan
	if err := json.NewDecoder(resp.Body).Decode(&plan); err != nil {
		t.Fatal(err)
	}
	if plan.WorkflowID != "review-pipeline" || len(plan.AgentRuns) != 3 {
		t.Fatalf("unexpected workflow plan: %+v", plan)
	}
	if plan.SourceHash == "" {
		t.Fatalf("expected source hash in workflow plan: %+v", plan)
	}

	workflow.Name = "Changed Review Pipeline"
	if err := store.WriteWorkflow("review-pipeline", workflow); err != nil {
		t.Fatal(err)
	}
	resp, err = http.Get(server.URL + "/api/workflow-plans")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var staleList struct {
		Plans []WorkflowPlanSummary `json:"plans"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&staleList); err != nil {
		t.Fatal(err)
	}
	if len(staleList.Plans) != 1 || !staleList.Plans[0].Stale || staleList.Plans[0].CurrentHash == staleList.Plans[0].SourceHash {
		t.Fatalf("expected stale workflow plan summary: %+v", staleList)
	}
}

func TestServerDeleteWorkflowPlanAPI(t *testing.T) {
	workdir := t.TempDir()
	if _, err := EnsureDefaultBlueprint(DefaultBlueprintPath(workdir)); err != nil {
		t.Fatal(err)
	}
	store := NewStore(workdir)
	if _, _, err := store.SaveCompiledWorkflowPlan(DefaultWorkflow()); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewServer(workdir).Handler())
	defer server.Close()

	req, err := http.NewRequest(http.MethodDelete, server.URL+"/api/workflow-plans/review-pipeline", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete workflow plan status = %d", resp.StatusCode)
	}
	if _, err := store.ReadWorkflowPlan("review-pipeline"); err == nil {
		t.Fatal("expected workflow plan to be deleted")
	}
}

func TestServerRefreshWorkflowPlanAPI(t *testing.T) {
	workdir := t.TempDir()
	if _, err := EnsureDefaultBlueprint(DefaultBlueprintPath(workdir)); err != nil {
		t.Fatal(err)
	}
	store := NewStore(workdir)
	workflow := DefaultWorkflow()
	if err := store.WriteWorkflow("review-pipeline", workflow); err != nil {
		t.Fatal(err)
	}
	original, _, err := store.SaveCompiledWorkflowPlan(workflow)
	if err != nil {
		t.Fatal(err)
	}
	workflow.Name = "Changed Review Pipeline"
	if err := store.WriteWorkflow("review-pipeline", workflow); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewServer(workdir).Handler())
	defer server.Close()

	resp, err := http.Post(server.URL+"/api/workflow-plans/review-pipeline/refresh", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("refresh workflow plan status = %d", resp.StatusCode)
	}
	var payload WorkflowCompiledPlanSaveResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if !payload.OK || payload.Plan.SourceHash == "" || payload.Plan.SourceHash == original.SourceHash {
		t.Fatalf("unexpected refresh payload: %+v", payload)
	}

	summaries, err := store.ListWorkflowPlans()
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 || summaries[0].Stale {
		t.Fatalf("expected refreshed plan to be fresh: %+v", summaries)
	}
}

func TestServerRunWorkflowPlanAPI(t *testing.T) {
	workdir := t.TempDir()
	if _, err := EnsureDefaultBlueprint(DefaultBlueprintPath(workdir)); err != nil {
		t.Fatal(err)
	}
	store := NewStore(workdir)
	workflow := DefaultWorkflow()
	if err := store.WriteWorkflow("review-pipeline", workflow); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.SaveCompiledWorkflowPlan(workflow); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewServer(workdir).Handler())
	defer server.Close()

	raw, err := json.Marshal(WorkflowPlanRunRequest{Input: "build the API"})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(server.URL+"/api/workflow-plans/review-pipeline/run", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("run workflow plan status = %d", resp.StatusCode)
	}
	var payload WorkflowRunSaveResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if !payload.OK {
		t.Fatalf("expected run to succeed: %+v", payload)
	}
	if len(payload.Run.Steps) != 3 {
		t.Fatalf("expected three agent dry-run steps: %+v", payload.Run.Steps)
	}
	if len(payload.Run.Outputs) != 1 || !strings.Contains(payload.Run.Outputs[0].Content, "Summary Agent") {
		t.Fatalf("unexpected run output: %+v", payload.Run.Outputs)
	}
	if payload.Run.ID == "" || payload.Run.CreatedAt == "" || payload.Run.StartedAt == "" || payload.Run.FinishedAt == "" || payload.Run.ExecutionMode != "dry_run" || payload.Run.TimeoutMS != DefaultWorkflowRunTimeoutMS {
		t.Fatalf("expected saved run metadata: %+v", payload.Run)
	}
	if payload.Run.PlanSnapshot == nil || payload.Run.PlanSnapshot.WorkflowID != "review-pipeline" || len(payload.Run.PlanSnapshot.AgentRuns) != 3 {
		t.Fatalf("expected run to include compiled plan snapshot: %+v", payload.Run.PlanSnapshot)
	}
	if len(payload.Run.Steps) == 0 || payload.Run.Steps[0].StartedAt == "" || payload.Run.Steps[0].FinishedAt == "" {
		t.Fatalf("expected run step timing: %+v", payload.Run.Steps)
	}

	resp, err = http.Get(server.URL + "/api/workflow-runs/review-pipeline")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var listPayload struct {
		Runs []WorkflowRunSummary `json:"runs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listPayload); err != nil {
		t.Fatal(err)
	}
	if len(listPayload.Runs) != 1 || listPayload.Runs[0].ID != payload.Run.ID || listPayload.Runs[0].StartedAt == "" || listPayload.Runs[0].FinishedAt == "" || listPayload.Runs[0].ExecutionMode != "dry_run" || listPayload.Runs[0].TimeoutMS != DefaultWorkflowRunTimeoutMS {
		t.Fatalf("unexpected run list: %+v", listPayload.Runs)
	}
	if listPayload.Runs[0].StepCount != 3 || listPayload.Runs[0].FailedStepID != "" {
		t.Fatalf("expected successful run summary steps: %+v", listPayload.Runs[0])
	}

	resp, err = http.Get(server.URL + "/api/workflow-runs/review-pipeline/" + payload.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var stored WorkflowPlanRun
	if err := json.NewDecoder(resp.Body).Decode(&stored); err != nil {
		t.Fatal(err)
	}
	if stored.ID != payload.Run.ID || stored.Input != "build the API" {
		t.Fatalf("unexpected stored run: %+v", stored)
	}
	if stored.PlanSnapshot == nil || len(stored.PlanSnapshot.AgentRuns) != 3 {
		t.Fatalf("expected stored run plan snapshot: %+v", stored.PlanSnapshot)
	}

	resp, err = http.Get(server.URL + "/api/workflow-runs/review-pipeline/" + payload.Run.ID + "/report")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	reportRaw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	report := string(reportRaw)
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/markdown") {
		t.Fatalf("unexpected report response: status=%d content-type=%q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if !strings.Contains(report, "# Workflow Run Report") || !strings.Contains(report, "## Plan Snapshot") || !strings.Contains(report, payload.Run.ID) || !strings.Contains(report, "build the API") || !strings.Contains(report, "Summary Agent") {
		t.Fatalf("unexpected workflow run report:\n%s", report)
	}

	resp, err = http.Post(server.URL+"/api/workflow-runs/review-pipeline/"+payload.Run.ID+"/rerun", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var rerunPayload WorkflowRunSaveResponse
	if err := json.NewDecoder(resp.Body).Decode(&rerunPayload); err != nil {
		t.Fatal(err)
	}
	if !rerunPayload.OK || rerunPayload.Run.ID == "" || rerunPayload.Run.ID == payload.Run.ID {
		t.Fatalf("expected rerun to create a new saved run: %+v", rerunPayload)
	}
	if rerunPayload.Run.Input != payload.Run.Input || rerunPayload.Run.ExecutionMode != payload.Run.ExecutionMode {
		t.Fatalf("expected rerun to reuse prior config: old=%+v new=%+v", payload.Run, rerunPayload.Run)
	}
	if rerunPayload.Run.RerunOf != payload.Run.ID {
		t.Fatalf("expected rerun lineage: old=%+v new=%+v", payload.Run, rerunPayload.Run)
	}

	resp, err = http.Get(server.URL + "/api/workflow-runs/review-pipeline/" + rerunPayload.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var storedRerun WorkflowPlanRun
	if err := json.NewDecoder(resp.Body).Decode(&storedRerun); err != nil {
		t.Fatal(err)
	}
	if storedRerun.RerunOf != payload.Run.ID {
		t.Fatalf("expected stored rerun lineage: %+v", storedRerun)
	}

	resp, err = http.Get(server.URL + "/api/workflow-runs/review-pipeline")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	listPayload.Runs = nil
	if err := json.NewDecoder(resp.Body).Decode(&listPayload); err != nil {
		t.Fatal(err)
	}
	var rerunSummary WorkflowRunSummary
	for _, run := range listPayload.Runs {
		if run.ID == rerunPayload.Run.ID {
			rerunSummary = run
		}
	}
	if rerunSummary.RerunOf != payload.Run.ID {
		t.Fatalf("expected rerun summary lineage: %+v", listPayload.Runs)
	}

	req, err := http.NewRequest(http.MethodDelete, server.URL+"/api/workflow-runs/review-pipeline/"+payload.Run.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete workflow run status = %d", resp.StatusCode)
	}
	req, err = http.NewRequest(http.MethodDelete, server.URL+"/api/workflow-runs/review-pipeline/"+rerunPayload.Run.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete rerun workflow run status = %d", resp.StatusCode)
	}

	resp, err = http.Get(server.URL + "/api/workflow-runs/review-pipeline")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	listPayload.Runs = nil
	if err := json.NewDecoder(resp.Body).Decode(&listPayload); err != nil {
		t.Fatal(err)
	}
	if len(listPayload.Runs) != 0 {
		t.Fatalf("expected workflow run to be deleted: %+v", listPayload.Runs)
	}
}

func TestServerRunWorkflowPlanPersistsFailures(t *testing.T) {
	workdir := t.TempDir()
	if _, err := EnsureDefaultBlueprint(DefaultBlueprintPath(workdir)); err != nil {
		t.Fatal(err)
	}
	store := NewStore(workdir)
	workflow := DefaultWorkflow()
	if err := store.WriteWorkflow("review-pipeline", workflow); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.SaveCompiledWorkflowPlan(workflow); err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(workdir, "slow-invoker.sh")
	script := "#!/bin/sh\nsleep 1\nprintf '{\"content\":\"late\"}\\n'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewServer(workdir).Handler())
	defer server.Close()

	raw, err := json.Marshal(WorkflowPlanRunRequest{
		Input:           "build the API",
		ExecutionMode:   "external_command",
		ExternalCommand: []string{scriptPath},
		TimeoutMS:       20,
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(server.URL+"/api/workflow-plans/review-pipeline/run", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var payload WorkflowRunSaveResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.OK || payload.Run.ID == "" || payload.Run.Status != WorkflowRunStatusFailed || strings.Join(payload.Run.ExternalCommand, " ") != scriptPath {
		t.Fatalf("expected failed saved run response: %+v", payload)
	}

	runs, err := store.ListWorkflowRuns("review-pipeline")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].ID != payload.Run.ID || runs[0].Status != WorkflowRunStatusFailed || strings.Join(runs[0].ExternalCommand, " ") != scriptPath {
		t.Fatalf("expected failed run in history: %+v", runs)
	}
	if runs[0].StepCount != 1 || runs[0].FailedStepID == "" || !strings.Contains(runs[0].FailedStepError, context.DeadlineExceeded.Error()) {
		t.Fatalf("expected failed step summary in history: %+v", runs[0])
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
