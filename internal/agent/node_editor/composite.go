package nodeeditor

import (
	"fmt"
	"strings"
)

const MaxCompositeDepth = 8

type CompositeDefinition struct {
	Version  int                    `json:"version"`
	ID       string                 `json:"id"`
	Name     string                 `json:"name"`
	Inputs   []CompositePortMapping `json:"inputs,omitempty"`
	Outputs  []CompositePortMapping `json:"outputs,omitempty"`
	Nodes    []Node                 `json:"nodes"`
	Edges    []Edge                 `json:"edges"`
	Metadata map[string]any         `json:"metadata,omitempty"`
}

type CompositePortMapping struct {
	Port     Port     `json:"port"`
	Endpoint Endpoint `json:"endpoint"`
}

type CompositeLoader interface {
	LoadComposite(id string) (CompositeDefinition, error)
}

func ExpandComposites(blueprint Blueprint, loader CompositeLoader) (Blueprint, error) {
	return expandComposites(blueprint, loader, nil, 0)
}

func expandComposites(blueprint Blueprint, loader CompositeLoader, stack []string, depth int) (Blueprint, error) {
	if loader == nil {
		return blueprint, nil
	}
	if depth > MaxCompositeDepth {
		return Blueprint{}, fmt.Errorf("composite expansion exceeded max depth %d", MaxCompositeDepth)
	}
	var expandedNodes []Node
	var expandedEdges []Edge
	replacements := map[string]compositeReplacement{}
	changed := false

	for _, node := range blueprint.Nodes {
		if node.Type != NodeTypeComposite {
			expandedNodes = append(expandedNodes, node)
			continue
		}
		definitionID := strings.TrimSpace(configString(node.Config, "definition"))
		if definitionID == "" {
			return Blueprint{}, fmt.Errorf("composite node %q missing definition", node.ID)
		}
		if containsString(stack, definitionID) {
			return Blueprint{}, fmt.Errorf("composite cycle: %s -> %s", strings.Join(stack, " -> "), definitionID)
		}
		definition, err := loader.LoadComposite(definitionID)
		if err != nil {
			return Blueprint{}, fmt.Errorf("load composite %q: %w", definitionID, err)
		}
		if err := ValidateComposite(definition); err != nil {
			return Blueprint{}, fmt.Errorf("validate composite %q: %w", definitionID, err)
		}
		subgraph := Blueprint{
			Version:   SchemaVersion,
			ID:        definition.ID,
			Name:      definition.Name,
			RootAgent: firstAgentID(definition.Nodes),
			Nodes:     definition.Nodes,
			Edges:     definition.Edges,
		}
		expanded, err := expandComposites(subgraph, loader, append(stack, definitionID), depth+1)
		if err != nil {
			return Blueprint{}, err
		}
		prefix := node.ID + "__"
		for _, inner := range expanded.Nodes {
			expandedNodes = append(expandedNodes, namespaceNode(inner, prefix, node.Position))
		}
		for _, edge := range expanded.Edges {
			expandedEdges = append(expandedEdges, namespaceEdge(edge, prefix))
		}
		replacements[node.ID] = compositeReplacement{
			inputs:  compositeEndpointMap(definition.Inputs, prefix),
			outputs: compositeEndpointMap(definition.Outputs, prefix),
		}
		changed = true
	}

	for _, edge := range blueprint.Edges {
		next := edge
		if replacement, ok := replacements[edge.Source.Node]; ok {
			endpoint, exists := replacement.outputs[edge.Source.Port]
			if !exists {
				return Blueprint{}, fmt.Errorf("composite node %q has no output port %q", edge.Source.Node, edge.Source.Port)
			}
			next.Source = endpoint
		}
		if replacement, ok := replacements[edge.Target.Node]; ok {
			endpoint, exists := replacement.inputs[edge.Target.Port]
			if !exists {
				return Blueprint{}, fmt.Errorf("composite node %q has no input port %q", edge.Target.Node, edge.Target.Port)
			}
			next.Target = endpoint
		}
		if _, sourceWasComposite := replacements[edge.Source.Node]; sourceWasComposite {
			next.ID = edge.ID + "__expanded"
		}
		if _, targetWasComposite := replacements[edge.Target.Node]; targetWasComposite {
			next.ID = edge.ID + "__expanded"
		}
		expandedEdges = append(expandedEdges, next)
	}

	if !changed {
		return blueprint, nil
	}
	blueprint.Nodes = expandedNodes
	blueprint.Edges = expandedEdges
	return expandComposites(blueprint, loader, stack, depth+1)
}

func ValidateComposite(definition CompositeDefinition) error {
	if definition.Version != SchemaVersion {
		return fmt.Errorf("unsupported composite version %d", definition.Version)
	}
	if strings.TrimSpace(definition.ID) == "" {
		return fmt.Errorf("composite id is required")
	}
	nodes := map[string]Node{}
	for _, node := range definition.Nodes {
		if strings.TrimSpace(node.ID) == "" {
			return fmt.Errorf("node id is required")
		}
		if _, exists := nodes[node.ID]; exists {
			return fmt.Errorf("duplicate node id %q", node.ID)
		}
		if err := validatePorts(node); err != nil {
			return err
		}
		nodes[node.ID] = node
	}
	for _, mapping := range append(definition.Inputs, definition.Outputs...) {
		if strings.TrimSpace(mapping.Port.ID) == "" {
			return fmt.Errorf("composite port id is required")
		}
		direction := DirectionInput
		if mapping.Port.Direction == DirectionOutput {
			direction = DirectionOutput
		}
		_, innerPort, err := findPort(nodes, mapping.Endpoint, direction)
		if err != nil {
			return err
		}
		if innerPort.Type != mapping.Port.Type {
			return fmt.Errorf("composite port %q maps %s to %s", mapping.Port.ID, mapping.Port.Type, innerPort.Type)
		}
	}
	for _, edge := range definition.Edges {
		sourceNode, sourcePort, err := findPort(nodes, edge.Source, DirectionOutput)
		if err != nil {
			return fmt.Errorf("edge %q source: %w", edge.ID, err)
		}
		_ = sourceNode
		_, targetPort, err := findPort(nodes, edge.Target, DirectionInput)
		if err != nil {
			return fmt.Errorf("edge %q target: %w", edge.ID, err)
		}
		if sourcePort.Type != targetPort.Type {
			return fmt.Errorf("edge %q connects %s to %s", edge.ID, sourcePort.Type, targetPort.Type)
		}
	}
	return nil
}

type compositeReplacement struct {
	inputs  map[string]Endpoint
	outputs map[string]Endpoint
}

func compositeEndpointMap(mappings []CompositePortMapping, prefix string) map[string]Endpoint {
	result := map[string]Endpoint{}
	for _, mapping := range mappings {
		result[mapping.Port.ID] = Endpoint{
			Node: prefix + mapping.Endpoint.Node,
			Port: mapping.Endpoint.Port,
		}
	}
	return result
}

func namespaceNode(node Node, prefix string, offset Position) Node {
	node.ID = prefix + node.ID
	node.Position.X += offset.X
	node.Position.Y += offset.Y
	return node
}

func namespaceEdge(edge Edge, prefix string) Edge {
	edge.ID = prefix + edge.ID
	edge.Source.Node = prefix + edge.Source.Node
	edge.Target.Node = prefix + edge.Target.Node
	return edge
}

func firstAgentID(nodes []Node) string {
	for _, node := range nodes {
		if node.Type == NodeTypeAgent {
			return node.ID
		}
	}
	if len(nodes) > 0 {
		return nodes[0].ID
	}
	return ""
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func configString(config map[string]any, key string) string {
	if config == nil {
		return ""
	}
	value, ok := config[key]
	if !ok {
		return ""
	}
	text, _ := value.(string)
	return text
}
