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

func BuildCompositeFromSelection(blueprint Blueprint, selectedIDs []string, id string, name string) (CompositeDefinition, error) {
	id = safeID(id)
	if id == "" {
		return CompositeDefinition{}, fmt.Errorf("composite id is required")
	}
	selected := map[string]bool{}
	for _, nodeID := range selectedIDs {
		nodeID = strings.TrimSpace(nodeID)
		if nodeID != "" {
			selected[nodeID] = true
		}
	}
	if len(selected) == 0 {
		return CompositeDefinition{}, fmt.Errorf("select at least one node")
	}

	sourceNodes := map[string]Node{}
	for _, node := range blueprint.Nodes {
		sourceNodes[node.ID] = node
	}
	var nodes []Node
	for _, node := range blueprint.Nodes {
		if selected[node.ID] {
			nodes = append(nodes, cloneNode(node))
		}
	}
	if len(nodes) != len(selected) {
		return CompositeDefinition{}, fmt.Errorf("selection contains unknown nodes")
	}
	normalizeCompositeNodePositions(nodes)

	var edges []Edge
	var inputs []CompositePortMapping
	var outputs []CompositePortMapping
	usedInputPorts := map[string]bool{}
	usedOutputPorts := map[string]bool{}
	for _, edge := range blueprint.Edges {
		sourceSelected := selected[edge.Source.Node]
		targetSelected := selected[edge.Target.Node]
		switch {
		case sourceSelected && targetSelected:
			edges = append(edges, edge)
		case !sourceSelected && targetSelected:
			_, innerPort, err := findPort(sourceNodes, edge.Target, DirectionInput)
			if err != nil {
				return CompositeDefinition{}, err
			}
			portID := uniqueCompositePortID("in_"+edge.Target.Node+"_"+edge.Target.Port, usedInputPorts)
			inputs = append(inputs, CompositePortMapping{
				Port: Port{
					ID:        portID,
					Type:      innerPort.Type,
					Label:     compositePortLabel(sourceNodes[edge.Target.Node], innerPort),
					Direction: DirectionInput,
					Multiple:  innerPort.Multiple,
					Order:     innerPort.Order,
				},
				Endpoint: edge.Target,
			})
		case sourceSelected && !targetSelected:
			_, innerPort, err := findPort(sourceNodes, edge.Source, DirectionOutput)
			if err != nil {
				return CompositeDefinition{}, err
			}
			portID := uniqueCompositePortID("out_"+edge.Source.Node+"_"+edge.Source.Port, usedOutputPorts)
			outputs = append(outputs, CompositePortMapping{
				Port: Port{
					ID:        portID,
					Type:      innerPort.Type,
					Label:     compositePortLabel(sourceNodes[edge.Source.Node], innerPort),
					Direction: DirectionOutput,
					Order:     innerPort.Order,
				},
				Endpoint: edge.Source,
			})
		}
	}
	if strings.TrimSpace(name) == "" {
		name = id
	}
	definition := CompositeDefinition{
		Version: SchemaVersion,
		ID:      id,
		Name:    name,
		Inputs:  inputs,
		Outputs: outputs,
		Nodes:   nodes,
		Edges:   edges,
		Metadata: map[string]any{
			"created_from": blueprint.ID,
		},
	}
	if err := ValidateComposite(definition); err != nil {
		return CompositeDefinition{}, err
	}
	return definition, nil
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

func cloneNode(node Node) Node {
	next := node
	next.Inputs = append([]Port(nil), node.Inputs...)
	next.Outputs = append([]Port(nil), node.Outputs...)
	if node.Config != nil {
		next.Config = map[string]any{}
		for key, value := range node.Config {
			next.Config[key] = value
		}
	}
	return next
}

func normalizeCompositeNodePositions(nodes []Node) {
	if len(nodes) == 0 {
		return
	}
	minX := nodes[0].Position.X
	minY := nodes[0].Position.Y
	for _, node := range nodes[1:] {
		if node.Position.X < minX {
			minX = node.Position.X
		}
		if node.Position.Y < minY {
			minY = node.Position.Y
		}
	}
	for index := range nodes {
		nodes[index].Position.X = nodes[index].Position.X - minX + 80
		nodes[index].Position.Y = nodes[index].Position.Y - minY + 80
	}
}

func uniqueCompositePortID(raw string, used map[string]bool) string {
	base := safeID(raw)
	if base == "" {
		base = "port"
	}
	id := base
	for suffix := 2; used[id]; suffix++ {
		id = fmt.Sprintf("%s_%d", base, suffix)
	}
	used[id] = true
	return id
}

func compositePortLabel(node Node, port Port) string {
	nodeLabel := strings.TrimSpace(node.Label)
	if nodeLabel == "" {
		nodeLabel = node.ID
	}
	portLabel := strings.TrimSpace(port.Label)
	if portLabel == "" {
		portLabel = port.ID
	}
	return nodeLabel + " / " + portLabel
}
