package nodeeditor

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed web/*
var webFS embed.FS

type Server struct {
	store *Store
}

type Store struct {
	root string
}

type BlueprintSummary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

type CompositeSummary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

type WorkflowSummary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

type CreateBlueprintRequest struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	SourceID string `json:"source_id,omitempty"`
}

type CompositeFromSelectionRequest struct {
	Blueprint Blueprint `json:"blueprint"`
	NodeIDs   []string  `json:"node_ids"`
	ID        string    `json:"id"`
	Name      string    `json:"name"`
}

type BlueprintRuntimeSelector struct {
	ID      string `json:"id"`
	Path    string `json:"path"`
	Command string `json:"command"`
}

type BlueprintValidationResponse struct {
	OK           bool                     `json:"ok"`
	Error        string                   `json:"error,omitempty"`
	Diagnostics  []string                 `json:"diagnostics,omitempty"`
	Resolved     ResolvedAgentDefinition  `json:"resolved,omitempty"`
	Expanded     Blueprint                `json:"expanded,omitempty"`
	Capabilities CapabilityResolution     `json:"capabilities,omitempty"`
	PromptBlocks []PromptPreviewBlock     `json:"prompt_blocks,omitempty"`
	Runtime      BlueprintRuntimeSelector `json:"runtime,omitempty"`
}

type WorkflowValidationResponse struct {
	OK    bool     `json:"ok"`
	Error string   `json:"error,omitempty"`
	Order []string `json:"order,omitempty"`
}

func NewStore(workdir string) *Store {
	return &Store{root: filepath.Join(workdir, ".agents", "blueprints")}
}

func (s *Store) AgentDir() string {
	return filepath.Join(s.root, "agents")
}

func (s *Store) CompositeDir() string {
	return filepath.Join(s.root, "composites")
}

func (s *Store) WorkflowDir() string {
	return filepath.Join(s.root, "workflows")
}

func (s *Store) AgentPath(id string) (string, error) {
	id = safeID(id)
	if id == "" {
		return "", fmt.Errorf("blueprint id is required")
	}
	return filepath.Join(s.AgentDir(), id+".json"), nil
}

func (s *Store) CompositePath(id string) (string, error) {
	id = safeID(id)
	if id == "" {
		return "", fmt.Errorf("composite id is required")
	}
	return filepath.Join(s.CompositeDir(), id+".json"), nil
}

func (s *Store) WorkflowPath(id string) (string, error) {
	id = safeID(id)
	if id == "" {
		return "", fmt.Errorf("workflow id is required")
	}
	return filepath.Join(s.WorkflowDir(), id+".json"), nil
}

func (s *Store) ListAgents() ([]BlueprintSummary, error) {
	entries, err := os.ReadDir(s.AgentDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var summaries []BlueprintSummary
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(s.AgentDir(), entry.Name())
		blueprint, err := ReadBlueprint(path)
		if err != nil {
			continue
		}
		summaries = append(summaries, BlueprintSummary{
			ID:   blueprint.ID,
			Name: blueprint.Name,
			Path: path,
		})
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].ID < summaries[j].ID
	})
	return summaries, nil
}

func (s *Store) ListComposites() ([]CompositeSummary, error) {
	entries, err := os.ReadDir(s.CompositeDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var summaries []CompositeSummary
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(s.CompositeDir(), entry.Name())
		definition, err := ReadComposite(path)
		if err != nil {
			continue
		}
		summaries = append(summaries, CompositeSummary{
			ID:   definition.ID,
			Name: definition.Name,
			Path: path,
		})
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].ID < summaries[j].ID
	})
	return summaries, nil
}

func (s *Store) ListWorkflows() ([]WorkflowSummary, error) {
	entries, err := os.ReadDir(s.WorkflowDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var summaries []WorkflowSummary
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(s.WorkflowDir(), entry.Name())
		workflow, err := ReadWorkflow(path)
		if err != nil {
			continue
		}
		summaries = append(summaries, WorkflowSummary{
			ID:   workflow.ID,
			Name: workflow.Name,
			Path: path,
		})
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].ID < summaries[j].ID
	})
	return summaries, nil
}

func (s *Store) ReadAgent(id string) (Blueprint, error) {
	path, err := s.AgentPath(id)
	if err != nil {
		return Blueprint{}, err
	}
	return ReadBlueprint(path)
}

func (s *Store) WriteAgent(id string, blueprint Blueprint) error {
	path, err := s.AgentPath(id)
	if err != nil {
		return err
	}
	if safeID(blueprint.ID) != safeID(id) {
		return fmt.Errorf("blueprint id %q does not match route id %q", blueprint.ID, id)
	}
	if err := Validate(blueprint); err != nil {
		return err
	}
	return WriteBlueprint(path, blueprint)
}

func (s *Store) RuntimeSelector(id string) BlueprintRuntimeSelector {
	id = safeID(id)
	if id == "" {
		id = "default"
	}
	path := filepath.ToSlash(filepath.Join(".agents", "blueprints", "agents", id+".json"))
	return BlueprintRuntimeSelector{
		ID:      id,
		Path:    path,
		Command: "BEE_AGENT_USE_BLUEPRINT=1 BEE_AGENT_BLUEPRINT_ID=" + id + " go run ./cmd/bee-agent",
	}
}

func (s *Store) ValidateBlueprintForRuntime(blueprint Blueprint) BlueprintValidationResponse {
	expanded, err := ExpandComposites(blueprint, s)
	if err != nil {
		return BlueprintValidationResponse{OK: false, Error: err.Error()}
	}
	if err := Validate(expanded); err != nil {
		return BlueprintValidationResponse{OK: false, Error: err.Error()}
	}
	resolved, err := Resolve(expanded)
	if err != nil {
		return BlueprintValidationResponse{OK: false, Error: err.Error()}
	}
	return BlueprintValidationResponse{
		OK:           true,
		Diagnostics:  ConfigDiagnostics(expanded, resolved),
		Resolved:     resolved,
		Expanded:     expanded,
		Capabilities: EffectiveToolNames(expanded, resolved),
		PromptBlocks: PromptPreview(expanded, resolved),
		Runtime:      s.RuntimeSelector(blueprint.ID),
	}
}

func (s *Store) CreateAgent(request CreateBlueprintRequest) (Blueprint, error) {
	id := safeID(request.ID)
	if id == "" {
		return Blueprint{}, fmt.Errorf("blueprint id is required")
	}
	path, err := s.AgentPath(id)
	if err != nil {
		return Blueprint{}, err
	}
	if _, err := os.Stat(path); err == nil {
		return Blueprint{}, fmt.Errorf("blueprint %q already exists", id)
	} else if !os.IsNotExist(err) {
		return Blueprint{}, err
	}

	var blueprint Blueprint
	if strings.TrimSpace(request.SourceID) != "" {
		blueprint, err = s.ReadAgent(request.SourceID)
		if err != nil {
			return Blueprint{}, fmt.Errorf("read source blueprint %q: %w", request.SourceID, err)
		}
	} else {
		blueprint = DefaultBlueprint()
	}
	blueprint.ID = id
	if strings.TrimSpace(request.Name) != "" {
		blueprint.Name = strings.TrimSpace(request.Name)
	} else if strings.TrimSpace(blueprint.Name) == "" || strings.TrimSpace(request.SourceID) == "" {
		blueprint.Name = id
	}
	if blueprint.Metadata == nil {
		blueprint.Metadata = map[string]any{}
	}
	blueprint.Metadata["created_by"] = "node_editor"
	if request.SourceID != "" {
		blueprint.Metadata["copied_from"] = request.SourceID
	}
	if err := s.WriteAgent(id, blueprint); err != nil {
		return Blueprint{}, err
	}
	return blueprint, nil
}

func (s *Store) LoadComposite(id string) (CompositeDefinition, error) {
	path, err := s.CompositePath(id)
	if err != nil {
		return CompositeDefinition{}, err
	}
	return ReadComposite(path)
}

func (s *Store) WriteComposite(id string, definition CompositeDefinition) error {
	path, err := s.CompositePath(id)
	if err != nil {
		return err
	}
	if safeID(definition.ID) != safeID(id) {
		return fmt.Errorf("composite id %q does not match route id %q", definition.ID, id)
	}
	if err := ValidateComposite(definition); err != nil {
		return err
	}
	return WriteComposite(path, definition)
}

func (s *Store) ReadWorkflow(id string) (WorkflowDefinition, error) {
	path, err := s.WorkflowPath(id)
	if err != nil {
		return WorkflowDefinition{}, err
	}
	return ReadWorkflow(path)
}

func (s *Store) WriteWorkflow(id string, workflow WorkflowDefinition) error {
	path, err := s.WorkflowPath(id)
	if err != nil {
		return err
	}
	if safeID(workflow.ID) != safeID(id) {
		return fmt.Errorf("workflow id %q does not match route id %q", workflow.ID, id)
	}
	if err := ValidateWorkflow(workflow); err != nil {
		return err
	}
	if err := s.ValidateWorkflowAgentReferences(workflow); err != nil {
		return err
	}
	return WriteWorkflow(path, workflow)
}

func (s *Store) ValidateWorkflow(workflow WorkflowDefinition) WorkflowValidationResponse {
	order, err := WorkflowExecutionOrder(workflow)
	if err != nil {
		return WorkflowValidationResponse{OK: false, Error: err.Error()}
	}
	if err := s.ValidateWorkflowAgentReferences(workflow); err != nil {
		return WorkflowValidationResponse{OK: false, Error: err.Error()}
	}
	return WorkflowValidationResponse{OK: true, Order: order}
}

func (s *Store) ValidateWorkflowAgentReferences(workflow WorkflowDefinition) error {
	for _, node := range workflow.Nodes {
		if node.Type != WorkflowNodeTypeAgent {
			continue
		}
		id := safeID(node.AgentBlueprint)
		if id == "" {
			return fmt.Errorf("workflow agent node %q requires agent_blueprint", node.ID)
		}
		path, err := s.AgentPath(id)
		if err != nil {
			return err
		}
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("workflow agent node %q references missing blueprint %q", node.ID, node.AgentBlueprint)
			}
			return err
		}
	}
	return nil
}

func NewServer(workdir string) *Server {
	return &Server{store: NewStore(workdir)}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/node-templates", s.handleNodeTemplates)
	mux.HandleFunc("GET /api/blueprints", s.handleListBlueprints)
	mux.HandleFunc("POST /api/blueprints", s.handleCreateBlueprint)
	mux.HandleFunc("GET /api/blueprints/{id}", s.handleGetBlueprint)
	mux.HandleFunc("PUT /api/blueprints/{id}", s.handlePutBlueprint)
	mux.HandleFunc("POST /api/blueprints/{id}/validate", s.handleValidateBlueprint)
	mux.HandleFunc("GET /api/composites", s.handleListComposites)
	mux.HandleFunc("GET /api/composites/{id}", s.handleGetComposite)
	mux.HandleFunc("PUT /api/composites/{id}", s.handlePutComposite)
	mux.HandleFunc("POST /api/composites/from-selection", s.handleCompositeFromSelection)
	mux.HandleFunc("GET /api/workflows", s.handleListWorkflows)
	mux.HandleFunc("GET /api/workflows/{id}", s.handleGetWorkflow)
	mux.HandleFunc("PUT /api/workflows/{id}", s.handlePutWorkflow)
	mux.HandleFunc("POST /api/workflows/{id}/validate", s.handleValidateWorkflow)
	static, _ := fs.Sub(webFS, "web")
	mux.Handle("GET /", http.FileServer(http.FS(static)))
	return mux
}

func (s *Server) handleNodeTemplates(w http.ResponseWriter, r *http.Request) {
	templates := BuiltinNodeTemplates()
	composites, err := s.store.ListComposites()
	if err == nil {
		for _, composite := range composites {
			definition, err := s.store.LoadComposite(composite.ID)
			if err != nil {
				continue
			}
			templates = append(templates, CompositeNodeTemplate(definition))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"templates": templates})
}

func (s *Server) handleListBlueprints(w http.ResponseWriter, r *http.Request) {
	summaries, err := s.store.ListAgents()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"blueprints": summaries})
}

func (s *Server) handleCreateBlueprint(w http.ResponseWriter, r *http.Request) {
	request, err := decodeCreateBlueprint(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	blueprint, err := s.store.CreateAgent(request)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "blueprint": blueprint})
}

func (s *Server) handleGetBlueprint(w http.ResponseWriter, r *http.Request) {
	blueprint, err := s.store.ReadAgent(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, blueprint)
}

func (s *Server) handlePutBlueprint(w http.ResponseWriter, r *http.Request) {
	blueprint, err := decodeBlueprint(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.store.WriteAgent(r.PathValue("id"), blueprint); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleValidateBlueprint(w http.ResponseWriter, r *http.Request) {
	blueprint, err := decodeBlueprint(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, BlueprintValidationResponse{OK: false, Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, s.store.ValidateBlueprintForRuntime(blueprint))
}

func (s *Server) handleListComposites(w http.ResponseWriter, r *http.Request) {
	summaries, err := s.store.ListComposites()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"composites": summaries})
}

func (s *Server) handleGetComposite(w http.ResponseWriter, r *http.Request) {
	definition, err := s.store.LoadComposite(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, definition)
}

func (s *Server) handlePutComposite(w http.ResponseWriter, r *http.Request) {
	definition, err := decodeComposite(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.store.WriteComposite(r.PathValue("id"), definition); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleCompositeFromSelection(w http.ResponseWriter, r *http.Request) {
	request, err := decodeCompositeFromSelection(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	definition, err := BuildCompositeFromSelection(request.Blueprint, request.NodeIDs, request.ID, request.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.store.WriteComposite(definition.ID, definition); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "composite": definition})
}

func (s *Server) handleListWorkflows(w http.ResponseWriter, r *http.Request) {
	summaries, err := s.store.ListWorkflows()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"workflows": summaries})
}

func (s *Server) handleGetWorkflow(w http.ResponseWriter, r *http.Request) {
	workflow, err := s.store.ReadWorkflow(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, workflow)
}

func (s *Server) handlePutWorkflow(w http.ResponseWriter, r *http.Request) {
	workflow, err := decodeWorkflow(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.store.WriteWorkflow(r.PathValue("id"), workflow); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleValidateWorkflow(w http.ResponseWriter, r *http.Request) {
	workflow, err := decodeWorkflow(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, WorkflowValidationResponse{OK: false, Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, s.store.ValidateWorkflow(workflow))
}

func decodeBlueprint(body io.Reader) (Blueprint, error) {
	defer io.Copy(io.Discard, body)
	var blueprint Blueprint
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&blueprint); err != nil {
		return Blueprint{}, err
	}
	return blueprint, nil
}

func decodeCreateBlueprint(body io.Reader) (CreateBlueprintRequest, error) {
	defer io.Copy(io.Discard, body)
	var request CreateBlueprintRequest
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return CreateBlueprintRequest{}, err
	}
	return request, nil
}

func decodeComposite(body io.Reader) (CompositeDefinition, error) {
	defer io.Copy(io.Discard, body)
	var definition CompositeDefinition
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&definition); err != nil {
		return CompositeDefinition{}, err
	}
	return definition, nil
}

func decodeWorkflow(body io.Reader) (WorkflowDefinition, error) {
	defer io.Copy(io.Discard, body)
	var workflow WorkflowDefinition
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&workflow); err != nil {
		return WorkflowDefinition{}, err
	}
	return workflow, nil
}

func decodeCompositeFromSelection(body io.Reader) (CompositeFromSelectionRequest, error) {
	defer io.Copy(io.Discard, body)
	var request CompositeFromSelectionRequest
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return CompositeFromSelectionRequest{}, err
	}
	return request, nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]any{"error": err.Error()})
}

func safeID(id string) string {
	id = strings.TrimSpace(id)
	var b strings.Builder
	for _, r := range id {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
