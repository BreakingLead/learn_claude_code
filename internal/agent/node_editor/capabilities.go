package nodeeditor

import (
	"sort"
	"strings"
)

type CapabilityResolution struct {
	ToolNames   []string            `json:"tool_names"`
	Resolved    bool                `json:"resolved"`
	Diagnostics []string            `json:"diagnostics,omitempty"`
	Policies    []PolicyApplication `json:"policies,omitempty"`
}

type PolicyApplication struct {
	NodeID       string   `json:"node_id"`
	InputTools   []string `json:"input_tools"`
	OutputTools  []string `json:"output_tools"`
	DroppedTools []string `json:"dropped_tools,omitempty"`
}

func EffectiveToolNames(blueprint Blueprint, resolved ResolvedAgentDefinition) CapabilityResolution {
	nodes := nodeMap(blueprint)
	toolSet := map[string]bool{}
	resolvedAny := false
	var diagnostics []string
	var policies []PolicyApplication
	for _, nodeID := range resolved.ToolsetNodes {
		names, ok, nodeDiagnostics, nodePolicies := toolNamesForNode(blueprint, nodeID, nodes, nil)
		diagnostics = append(diagnostics, nodeDiagnostics...)
		policies = append(policies, nodePolicies...)
		if !ok {
			continue
		}
		resolvedAny = true
		for _, name := range names {
			name = strings.TrimSpace(name)
			if name != "" {
				toolSet[name] = true
			}
		}
	}
	names := make([]string, 0, len(toolSet))
	for name := range toolSet {
		names = append(names, name)
	}
	sort.Strings(names)
	return CapabilityResolution{ToolNames: names, Resolved: resolvedAny, Diagnostics: diagnostics, Policies: policies}
}

func toolNamesForNode(blueprint Blueprint, nodeID string, nodes map[string]Node, stack []string) ([]string, bool, []string, []PolicyApplication) {
	if containsNodeID(stack, nodeID) {
		return nil, false, []string{"tool capability cycle: " + strings.Join(append(stack, nodeID), " -> ")}, nil
	}
	node, ok := nodes[nodeID]
	if !ok {
		return nil, false, []string{"tool capability node not found: " + nodeID}, nil
	}
	switch node.Type {
	case NodeTypeToolset:
		return stringSliceNodeConfig(node.Config, "tools"), true, nil, nil
	case NodeTypePolicy:
		upstream, upstreamOK, diagnostics, policies := upstreamToolNames(blueprint, node.ID, nodes, append(stack, nodeID))
		if !upstreamOK {
			upstream = nil
		}
		output := filterPolicyToolNames(upstream, stringSliceNodeConfig(node.Config, "allow_tools"), stringSliceNodeConfig(node.Config, "deny_tools"))
		policies = append(policies, PolicyApplication{
			NodeID:       node.ID,
			InputTools:   sortedUniqueStrings(upstream),
			OutputTools:  sortedUniqueStrings(output),
			DroppedTools: droppedToolNames(upstream, output),
		})
		return output, true, diagnostics, policies
	default:
		tools := stringSliceNodeConfig(node.Config, "tools")
		return tools, len(tools) > 0, nil, nil
	}
}

func upstreamToolNames(blueprint Blueprint, nodeID string, nodes map[string]Node, stack []string) ([]string, bool, []string, []PolicyApplication) {
	toolSet := map[string]bool{}
	resolvedAny := false
	var diagnostics []string
	var policies []PolicyApplication
	for _, edge := range blueprint.Edges {
		if edge.Target.Node != nodeID {
			continue
		}
		targetPort := portForEndpoint(nodes, edge.Target, DirectionInput)
		sourcePort := portForEndpoint(nodes, edge.Source, DirectionOutput)
		if targetPort.Type != PortTypeToolset || sourcePort.Type != PortTypeToolset {
			continue
		}
		names, ok, nodeDiagnostics, nodePolicies := toolNamesForNode(blueprint, edge.Source.Node, nodes, stack)
		diagnostics = append(diagnostics, nodeDiagnostics...)
		policies = append(policies, nodePolicies...)
		if !ok {
			continue
		}
		resolvedAny = true
		for _, name := range names {
			name = strings.TrimSpace(name)
			if name != "" {
				toolSet[name] = true
			}
		}
	}
	names := make([]string, 0, len(toolSet))
	for name := range toolSet {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, resolvedAny, diagnostics, policies
}

func filterPolicyToolNames(upstream []string, allow []string, deny []string) []string {
	allowSet := map[string]bool{}
	for _, name := range allow {
		name = strings.TrimSpace(name)
		if name != "" {
			allowSet[name] = true
		}
	}
	denySet := map[string]bool{}
	for _, name := range deny {
		name = strings.TrimSpace(name)
		if name != "" {
			denySet[name] = true
		}
	}
	var names []string
	for _, name := range upstream {
		name = strings.TrimSpace(name)
		if name == "" || denySet[name] {
			continue
		}
		if len(allowSet) > 0 && !allowSet[name] {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func droppedToolNames(input []string, output []string) []string {
	outputSet := map[string]bool{}
	for _, name := range output {
		name = strings.TrimSpace(name)
		if name != "" {
			outputSet[name] = true
		}
	}
	var dropped []string
	for _, name := range input {
		name = strings.TrimSpace(name)
		if name != "" && !outputSet[name] {
			dropped = append(dropped, name)
		}
	}
	return sortedUniqueStrings(dropped)
}

func sortedUniqueStrings(values []string) []string {
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			seen[value] = true
		}
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func nodeMap(blueprint Blueprint) map[string]Node {
	nodes := make(map[string]Node, len(blueprint.Nodes))
	for _, node := range blueprint.Nodes {
		nodes[node.ID] = node
	}
	return nodes
}

func portForEndpoint(nodes map[string]Node, endpoint Endpoint, direction string) Port {
	node, ok := nodes[endpoint.Node]
	if !ok {
		return Port{}
	}
	ports := node.Outputs
	if direction == DirectionInput {
		ports = node.Inputs
	}
	for _, port := range ports {
		if port.ID == endpoint.Port {
			return port
		}
	}
	return Port{}
}

func stringSliceNodeConfig(config map[string]any, key string) []string {
	if config == nil {
		return nil
	}
	value, ok := config[key]
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		var values []string
		for _, item := range typed {
			if text, ok := item.(string); ok {
				values = append(values, text)
			}
		}
		return values
	default:
		return nil
	}
}

func containsNodeID(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
