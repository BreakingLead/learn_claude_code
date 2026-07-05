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

type CompositeFromSelectionRequest struct {
	Blueprint Blueprint `json:"blueprint"`
	NodeIDs   []string  `json:"node_ids"`
	ID        string    `json:"id"`
	Name      string    `json:"name"`
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

func NewServer(workdir string) *Server {
	return &Server{store: NewStore(workdir)}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/node-templates", s.handleNodeTemplates)
	mux.HandleFunc("GET /api/blueprints", s.handleListBlueprints)
	mux.HandleFunc("GET /api/blueprints/{id}", s.handleGetBlueprint)
	mux.HandleFunc("PUT /api/blueprints/{id}", s.handlePutBlueprint)
	mux.HandleFunc("POST /api/blueprints/{id}/validate", s.handleValidateBlueprint)
	mux.HandleFunc("GET /api/composites", s.handleListComposites)
	mux.HandleFunc("GET /api/composites/{id}", s.handleGetComposite)
	mux.HandleFunc("PUT /api/composites/{id}", s.handlePutComposite)
	mux.HandleFunc("POST /api/composites/from-selection", s.handleCompositeFromSelection)
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
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	expanded, err := ExpandComposites(blueprint, s.store)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if err := Validate(expanded); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	resolved, err := Resolve(expanded)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "resolved": resolved, "expanded": expanded})
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
