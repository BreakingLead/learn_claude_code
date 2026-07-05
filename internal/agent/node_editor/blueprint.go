package nodeeditor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	SchemaVersion = 1

	NodeTypeAgent     = "agent"
	NodeTypePrompt    = "prompt"
	NodeTypeToolset   = "toolset"
	NodeTypeMemory    = "memory"
	NodeTypeComposite = "composite"

	PortTypePrompt  = "prompt"
	PortTypeToolset = "toolset"
	PortTypeMemory  = "memory"
	PortTypeOutput  = "output"

	DirectionInput  = "input"
	DirectionOutput = "output"
)

type Blueprint struct {
	Version   int             `json:"version"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	RootAgent string          `json:"root_agent"`
	Nodes     []Node          `json:"nodes"`
	Edges     []Edge          `json:"edges"`
	Metadata  map[string]any  `json:"metadata,omitempty"`
	Config    json.RawMessage `json:"config,omitempty"`
}

type Node struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Label    string         `json:"label"`
	Position Position       `json:"position"`
	Inputs   []Port         `json:"inputs,omitempty"`
	Outputs  []Port         `json:"outputs,omitempty"`
	Config   map[string]any `json:"config,omitempty"`
}

type Position struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type Port struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Label     string `json:"label"`
	Direction string `json:"direction"`
	Multiple  bool   `json:"multiple,omitempty"`
	Order     int    `json:"order,omitempty"`
}

type Edge struct {
	ID     string   `json:"id"`
	Source Endpoint `json:"source"`
	Target Endpoint `json:"target"`
}

type Endpoint struct {
	Node string `json:"node"`
	Port string `json:"port"`
}

type ResolvedAgentDefinition struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	PromptNodes   []string `json:"prompt_nodes"`
	ToolsetNodes  []string `json:"toolset_nodes"`
	MemoryNodes   []string `json:"memory_nodes"`
	OriginalGraph string   `json:"original_graph"`
}

func DefaultBlueprint() Blueprint {
	return Blueprint{
		Version:   SchemaVersion,
		ID:        "default",
		Name:      "Default Bee Agent",
		RootAgent: "agent-main",
		Nodes: []Node{
			{
				ID:       "agent-main",
				Type:     NodeTypeAgent,
				Label:    "Bee Agent",
				Position: Position{X: 620, Y: 160},
				Inputs: []Port{
					{ID: "prompt_1", Type: PortTypePrompt, Label: "Prompt 1", Direction: DirectionInput, Order: 1},
					{ID: "prompt_2", Type: PortTypePrompt, Label: "Prompt 2", Direction: DirectionInput, Order: 2},
					{ID: "prompt_3", Type: PortTypePrompt, Label: "Prompt 3", Direction: DirectionInput, Order: 3},
					{ID: "toolset_in", Type: PortTypeToolset, Label: "Tools", Direction: DirectionInput, Multiple: true},
					{ID: "memory_in", Type: PortTypeMemory, Label: "Memory", Direction: DirectionInput, Multiple: true},
				},
				Config: map[string]any{
					"display_name": "Agent",
				},
			},
			{
				ID:       "project-context",
				Type:     NodeTypePrompt,
				Label:    "Project Context",
				Position: Position{X: 120, Y: 80},
				Outputs: []Port{
					{ID: "prompt_out", Type: PortTypePrompt, Label: "Prompt", Direction: DirectionOutput},
				},
				Config: map[string]any{
					"source": "project_files",
					"files":  []string{"AGENTS.md", "README.md"},
				},
			},
			{
				ID:       "build-mode",
				Type:     NodeTypePrompt,
				Label:    "Active Mode",
				Position: Position{X: 120, Y: 220},
				Outputs: []Port{
					{ID: "prompt_out", Type: PortTypePrompt, Label: "Prompt", Direction: DirectionOutput},
				},
				Config: map[string]any{
					"source": "active_mode",
				},
			},
			{
				ID:       "core-tools",
				Type:     NodeTypeToolset,
				Label:    "Core Tools",
				Position: Position{X: 120, Y: 380},
				Outputs: []Port{
					{ID: "toolset_out", Type: PortTypeToolset, Label: "Toolset", Direction: DirectionOutput},
				},
				Config: map[string]any{
					"tools": []string{"bash", "read_file", "write_file", "edit_file", "glob"},
				},
			},
		},
		Edges: []Edge{
			{ID: "edge-project-agent", Source: Endpoint{Node: "project-context", Port: "prompt_out"}, Target: Endpoint{Node: "agent-main", Port: "prompt_1"}},
			{ID: "edge-build-agent", Source: Endpoint{Node: "build-mode", Port: "prompt_out"}, Target: Endpoint{Node: "agent-main", Port: "prompt_2"}},
			{ID: "edge-tools-agent", Source: Endpoint{Node: "core-tools", Port: "toolset_out"}, Target: Endpoint{Node: "agent-main", Port: "toolset_in"}},
		},
		Metadata: map[string]any{
			"generated_by": "bee-agent",
			"purpose":      "default agent blueprint",
		},
	}
}

func DefaultBlueprintPath(workdir string) string {
	return filepath.Join(workdir, ".agents", "blueprints", "agents", "default.json")
}

func EnsureDefaultBlueprint(path string) (bool, error) {
	if strings.TrimSpace(path) == "" {
		return false, fmt.Errorf("default blueprint path is required")
	}
	if _, err := os.Stat(path); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, err
	}
	blueprint := DefaultBlueprint()
	if err := Validate(blueprint); err != nil {
		return false, err
	}
	return true, WriteBlueprint(path, blueprint)
}

func ReadBlueprint(path string) (Blueprint, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Blueprint{}, err
	}
	var blueprint Blueprint
	if err := json.Unmarshal(raw, &blueprint); err != nil {
		return Blueprint{}, err
	}
	return blueprint, nil
}

func WriteBlueprint(path string, blueprint Blueprint) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(blueprint, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(path, raw, 0o644)
}

func Validate(blueprint Blueprint) error {
	if blueprint.Version != SchemaVersion {
		return fmt.Errorf("unsupported blueprint version %d", blueprint.Version)
	}
	if strings.TrimSpace(blueprint.ID) == "" {
		return fmt.Errorf("blueprint id is required")
	}
	if strings.TrimSpace(blueprint.RootAgent) == "" {
		return fmt.Errorf("root_agent is required")
	}
	nodes := map[string]Node{}
	for _, node := range blueprint.Nodes {
		if strings.TrimSpace(node.ID) == "" {
			return fmt.Errorf("node id is required")
		}
		if _, exists := nodes[node.ID]; exists {
			return fmt.Errorf("duplicate node id %q", node.ID)
		}
		if strings.TrimSpace(node.Type) == "" {
			return fmt.Errorf("node %q type is required", node.ID)
		}
		if err := validatePorts(node); err != nil {
			return err
		}
		nodes[node.ID] = node
	}
	root, ok := nodes[blueprint.RootAgent]
	if !ok {
		return fmt.Errorf("root_agent %q not found", blueprint.RootAgent)
	}
	if root.Type != NodeTypeAgent {
		return fmt.Errorf("root_agent %q must be an agent node", blueprint.RootAgent)
	}

	incoming := map[string]int{}
	edgeIDs := map[string]bool{}
	for _, edge := range blueprint.Edges {
		if strings.TrimSpace(edge.ID) == "" {
			return fmt.Errorf("edge id is required")
		}
		if edgeIDs[edge.ID] {
			return fmt.Errorf("duplicate edge id %q", edge.ID)
		}
		edgeIDs[edge.ID] = true
		_, sourcePort, err := findPort(nodes, edge.Source, DirectionOutput)
		if err != nil {
			return fmt.Errorf("edge %q source: %w", edge.ID, err)
		}
		_, targetPort, err := findPort(nodes, edge.Target, DirectionInput)
		if err != nil {
			return fmt.Errorf("edge %q target: %w", edge.ID, err)
		}
		if sourcePort.Type != targetPort.Type {
			return fmt.Errorf("edge %q connects %s to %s", edge.ID, sourcePort.Type, targetPort.Type)
		}
		key := edge.Target.Node + ":" + edge.Target.Port
		incoming[key]++
		if incoming[key] > 1 && !targetPort.Multiple {
			return fmt.Errorf("input port %s on node %s allows only one edge", edge.Target.Port, edge.Target.Node)
		}
	}
	return nil
}

func Resolve(blueprint Blueprint) (ResolvedAgentDefinition, error) {
	if err := Validate(blueprint); err != nil {
		return ResolvedAgentDefinition{}, err
	}
	nodes := map[string]Node{}
	for _, node := range blueprint.Nodes {
		nodes[node.ID] = node
	}
	root := nodes[blueprint.RootAgent]
	incoming := incomingEdges(blueprint, root.ID)

	promptEdges := incomingByPortType(root, incoming, PortTypePrompt)
	sort.Slice(promptEdges, func(i, j int) bool {
		left := inputPortOrder(root, promptEdges[i].Target.Port)
		right := inputPortOrder(root, promptEdges[j].Target.Port)
		if left == right {
			return promptEdges[i].ID < promptEdges[j].ID
		}
		return left < right
	})

	resolved := ResolvedAgentDefinition{
		ID:            root.ID,
		Name:          root.Label,
		OriginalGraph: blueprint.ID,
	}
	for _, edge := range promptEdges {
		resolved.PromptNodes = append(resolved.PromptNodes, edge.Source.Node)
	}
	for _, edge := range incomingByPortType(root, incoming, PortTypeToolset) {
		resolved.ToolsetNodes = append(resolved.ToolsetNodes, edge.Source.Node)
	}
	for _, edge := range incomingByPortType(root, incoming, PortTypeMemory) {
		resolved.MemoryNodes = append(resolved.MemoryNodes, edge.Source.Node)
	}
	sort.Strings(resolved.ToolsetNodes)
	sort.Strings(resolved.MemoryNodes)
	return resolved, nil
}

func validatePorts(node Node) error {
	seen := map[string]bool{}
	for _, port := range append(append([]Port(nil), node.Inputs...), node.Outputs...) {
		if strings.TrimSpace(port.ID) == "" {
			return fmt.Errorf("node %q has port with empty id", node.ID)
		}
		if seen[port.ID] {
			return fmt.Errorf("node %q has duplicate port id %q", node.ID, port.ID)
		}
		seen[port.ID] = true
		if strings.TrimSpace(port.Type) == "" {
			return fmt.Errorf("node %q port %q type is required", node.ID, port.ID)
		}
		switch port.Direction {
		case DirectionInput, DirectionOutput:
		default:
			return fmt.Errorf("node %q port %q has invalid direction %q", node.ID, port.ID, port.Direction)
		}
	}
	for _, port := range node.Inputs {
		if port.Direction != DirectionInput {
			return fmt.Errorf("node %q input port %q has direction %q", node.ID, port.ID, port.Direction)
		}
	}
	for _, port := range node.Outputs {
		if port.Direction != DirectionOutput {
			return fmt.Errorf("node %q output port %q has direction %q", node.ID, port.ID, port.Direction)
		}
	}
	return nil
}

func findPort(nodes map[string]Node, endpoint Endpoint, direction string) (Node, Port, error) {
	node, ok := nodes[endpoint.Node]
	if !ok {
		return Node{}, Port{}, fmt.Errorf("node %q not found", endpoint.Node)
	}
	ports := node.Outputs
	if direction == DirectionInput {
		ports = node.Inputs
	}
	for _, port := range ports {
		if port.ID == endpoint.Port {
			return node, port, nil
		}
	}
	return Node{}, Port{}, fmt.Errorf("%s port %q not found on node %q", direction, endpoint.Port, endpoint.Node)
}

func incomingEdges(blueprint Blueprint, nodeID string) []Edge {
	var edges []Edge
	for _, edge := range blueprint.Edges {
		if edge.Target.Node == nodeID {
			edges = append(edges, edge)
		}
	}
	return edges
}

func incomingByPortType(node Node, edges []Edge, portType string) []Edge {
	var matched []Edge
	for _, edge := range edges {
		for _, port := range node.Inputs {
			if port.ID == edge.Target.Port && port.Type == portType {
				matched = append(matched, edge)
				break
			}
		}
	}
	return matched
}

func inputPortOrder(node Node, portID string) int {
	for _, port := range node.Inputs {
		if port.ID == portID {
			if port.Order != 0 {
				return port.Order
			}
			return len(node.Inputs) + 1
		}
	}
	return len(node.Inputs) + 1
}
