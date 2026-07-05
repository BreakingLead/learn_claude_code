package nodeeditor

import (
	"sort"
	"strings"
)

type CapabilityResolution struct {
	ToolNames []string `json:"tool_names"`
	Resolved  bool     `json:"resolved"`
}

func EffectiveToolNames(blueprint Blueprint, resolved ResolvedAgentDefinition) CapabilityResolution {
	nodes := nodeMap(blueprint)
	toolSet := map[string]bool{}
	resolvedAny := false
	for _, nodeID := range resolved.ToolsetNodes {
		names, ok := toolNamesForNode(blueprint, nodeID, nodes, nil)
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
	return CapabilityResolution{ToolNames: names, Resolved: resolvedAny}
}

func toolNamesForNode(blueprint Blueprint, nodeID string, nodes map[string]Node, stack []string) ([]string, bool) {
	if containsNodeID(stack, nodeID) {
		return nil, false
	}
	node, ok := nodes[nodeID]
	if !ok {
		return nil, false
	}
	switch node.Type {
	case NodeTypeToolset:
		return stringSliceNodeConfig(node.Config, "tools"), true
	case NodeTypePolicy:
		upstream, upstreamOK := upstreamToolNames(blueprint, node.ID, nodes, append(stack, nodeID))
		if !upstreamOK {
			upstream = nil
		}
		return filterPolicyToolNames(upstream, stringSliceNodeConfig(node.Config, "allow_tools"), stringSliceNodeConfig(node.Config, "deny_tools")), true
	default:
		tools := stringSliceNodeConfig(node.Config, "tools")
		return tools, len(tools) > 0
	}
}

func upstreamToolNames(blueprint Blueprint, nodeID string, nodes map[string]Node, stack []string) ([]string, bool) {
	toolSet := map[string]bool{}
	resolvedAny := false
	for _, edge := range blueprint.Edges {
		if edge.Target.Node != nodeID {
			continue
		}
		targetPort := portForEndpoint(nodes, edge.Target, DirectionInput)
		sourcePort := portForEndpoint(nodes, edge.Source, DirectionOutput)
		if targetPort.Type != PortTypeToolset || sourcePort.Type != PortTypeToolset {
			continue
		}
		names, ok := toolNamesForNode(blueprint, edge.Source.Node, nodes, stack)
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
	return names, resolvedAny
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
