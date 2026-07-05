package agent

import (
	nodeeditor "bee_agent/internal/agent/node_editor"
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

type runtimeBlueprint struct {
	Path     string
	Enabled  bool
	Graph    nodeeditor.Blueprint
	Resolved nodeeditor.ResolvedAgentDefinition
	Error    string
}

func (rt *agentRuntime) loadRuntimeBlueprint() *runtimeBlueprint {
	path := rt.config.BlueprintPath
	if strings.TrimSpace(path) == "" {
		path = rt.config.DefaultBlueprintPath
	}
	state := &runtimeBlueprint{
		Path:    path,
		Enabled: rt.config.UseBlueprint,
	}
	blueprint, err := nodeeditor.ReadBlueprint(path)
	if err != nil {
		state.Error = err.Error()
		return state
	}
	expanded, err := nodeeditor.ExpandComposites(blueprint, nodeeditor.NewStore(rt.config.Workdir))
	if err != nil {
		state.Graph = blueprint
		state.Error = err.Error()
		return state
	}
	resolved, err := nodeeditor.Resolve(expanded)
	if err != nil {
		state.Graph = expanded
		state.Error = err.Error()
		return state
	}
	state.Graph = expanded
	state.Resolved = resolved
	return state
}

func (rt *agentRuntime) blueprintToolNames(fallback []string) []string {
	if rt == nil || rt.blueprint == nil || !rt.blueprint.Enabled || rt.blueprint.Error != "" {
		return fallback
	}
	nodes := blueprintNodeMap(rt.blueprint.Graph)
	toolSet := map[string]bool{}
	resolvedAny := false
	for _, nodeID := range rt.blueprint.Resolved.ToolsetNodes {
		names, ok := rt.blueprintToolNamesForNode(nodeID, nodes, nil)
		if !ok {
			continue
		}
		resolvedAny = true
		for _, name := range names {
			if strings.TrimSpace(name) != "" {
				toolSet[name] = true
			}
		}
	}
	if !resolvedAny {
		return fallback
	}
	names := make([]string, 0, len(toolSet))
	for name := range toolSet {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (rt *agentRuntime) blueprintToolNamesForNode(nodeID string, nodes map[string]nodeeditor.Node, stack []string) ([]string, bool) {
	if containsBlueprintStack(stack, nodeID) {
		return nil, false
	}
	node, ok := nodes[nodeID]
	if !ok {
		return nil, false
	}
	switch node.Type {
	case nodeeditor.NodeTypeToolset:
		return stringSliceConfig(node.Config, "tools"), true
	case nodeeditor.NodeTypePolicy:
		upstream, upstreamOK := rt.blueprintUpstreamToolNames(node.ID, nodes, append(stack, nodeID))
		if !upstreamOK {
			upstream = nil
		}
		return filterPolicyToolNames(upstream, stringSliceConfig(node.Config, "allow_tools"), stringSliceConfig(node.Config, "deny_tools")), true
	default:
		return stringSliceConfig(node.Config, "tools"), len(stringSliceConfig(node.Config, "tools")) > 0
	}
}

func (rt *agentRuntime) blueprintUpstreamToolNames(nodeID string, nodes map[string]nodeeditor.Node, stack []string) ([]string, bool) {
	toolSet := map[string]bool{}
	resolvedAny := false
	for _, edge := range rt.blueprint.Graph.Edges {
		if edge.Target.Node != nodeID {
			continue
		}
		targetPort := portForBlueprintEndpoint(nodes, edge.Target, nodeeditor.DirectionInput)
		sourcePort := portForBlueprintEndpoint(nodes, edge.Source, nodeeditor.DirectionOutput)
		if targetPort.Type != nodeeditor.PortTypeToolset || sourcePort.Type != nodeeditor.PortTypeToolset {
			continue
		}
		names, ok := rt.blueprintToolNamesForNode(edge.Source.Node, nodes, stack)
		if !ok {
			continue
		}
		resolvedAny = true
		for _, name := range names {
			if strings.TrimSpace(name) != "" {
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

func (rt *agentRuntime) blueprintPromptBlocks(toolNames []string) ([]PromptBlock, bool) {
	if rt == nil || rt.blueprint == nil || !rt.blueprint.Enabled || rt.blueprint.Error != "" {
		return nil, false
	}
	nodes := blueprintNodeMap(rt.blueprint.Graph)
	var blocks []PromptBlock
	for _, nodeID := range rt.blueprint.Resolved.PromptNodes {
		node, ok := nodes[nodeID]
		if !ok {
			continue
		}
		blocks = append(blocks, rt.promptBlocksForBlueprintNode(node, toolNames)...)
	}
	for _, nodeID := range rt.blueprint.Resolved.MemoryNodes {
		node, ok := nodes[nodeID]
		if !ok {
			continue
		}
		blocks = append(blocks, rt.promptBlocksForBlueprintNode(node, toolNames)...)
	}
	if len(blocks) == 0 {
		return nil, false
	}
	return blocks, true
}

func (rt *agentRuntime) promptBlocksForBlueprintNode(node nodeeditor.Node, toolNames []string) []PromptBlock {
	source := stringConfig(node.Config, "source")
	if source == "" && node.Type == nodeeditor.NodeTypePolicy {
		source = "policy"
	}
	name := node.Label
	if strings.TrimSpace(name) == "" {
		name = node.ID
	}
	switch source {
	case "inline":
		content := strings.TrimSpace(stringConfig(node.Config, "prompt"))
		if content == "" {
			return nil
		}
		return []PromptBlock{{
			Module:  node.ID,
			Name:    name,
			Source:  "blueprint inline",
			Content: content,
		}}
	case "policy":
		content := strings.TrimSpace(stringConfig(node.Config, "prompt"))
		if content == "" {
			return nil
		}
		return []PromptBlock{{
			Module:  node.ID,
			Name:    name,
			Source:  "blueprint policy",
			Content: content,
		}}
	case "skill_file", "file":
		path := strings.TrimSpace(stringConfig(node.Config, "path"))
		if path == "" {
			return nil
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(rt.config.Workdir, path)
		}
		return readPromptFiles([]promptFileCandidate{{
			module: node.ID,
			name:   name,
			path:   path,
		}}, 12000)
	case "active_mode":
		mode := rt.activeMode()
		content := strings.TrimSpace(mode.Prompt)
		if content == "" {
			return nil
		}
		return []PromptBlock{{
			Module:  node.ID,
			Name:    fmt.Sprintf("%s: %s", name, mode.Name),
			Source:  "active mode",
			Content: content,
		}}
	case "project_files":
		var candidates []promptFileCandidate
		for _, file := range stringSliceConfig(node.Config, "files") {
			if strings.TrimSpace(file) == "" {
				continue
			}
			candidates = append(candidates, promptFileCandidate{
				module: node.ID,
				name:   name,
				path:   filepath.Join(rt.config.Workdir, file),
			})
		}
		return readPromptFiles(candidates, 6000)
	case "default_memory", "memory_index":
		path := strings.TrimSpace(stringConfig(node.Config, "path"))
		if path == "" {
			path = rt.config.MemoryIndex
		} else if !filepath.IsAbs(path) {
			path = filepath.Join(rt.config.Workdir, path)
		}
		return readPromptFiles([]promptFileCandidate{{
			module: node.ID,
			name:   name,
			path:   path,
		}}, 6000)
	case "module_prompt":
		blocks := rt.modules.promptBlocks(context.Background(), PromptRequest{ToolNames: toolNames})
		moduleID := stringConfig(node.Config, "module")
		if moduleID == "" {
			return blocks
		}
		var filtered []PromptBlock
		for _, block := range blocks {
			if block.Module == moduleID {
				filtered = append(filtered, block)
			}
		}
		return filtered
	default:
		return nil
	}
}

func portForBlueprintEndpoint(nodes map[string]nodeeditor.Node, endpoint nodeeditor.Endpoint, direction string) nodeeditor.Port {
	node, ok := nodes[endpoint.Node]
	if !ok {
		return nodeeditor.Port{}
	}
	ports := node.Outputs
	if direction == nodeeditor.DirectionInput {
		ports = node.Inputs
	}
	for _, port := range ports {
		if port.ID == endpoint.Port {
			return port
		}
	}
	return nodeeditor.Port{}
}

func (rt *agentRuntime) blueprintSnapshot() any {
	if rt == nil || rt.blueprint == nil {
		return nil
	}
	return map[string]any{
		"path":     rt.blueprint.Path,
		"enabled":  rt.blueprint.Enabled,
		"error":    rt.blueprint.Error,
		"graph":    rt.blueprint.Graph.ID,
		"root":     rt.blueprint.Graph.RootAgent,
		"resolved": rt.blueprint.Resolved,
	}
}

func blueprintNodeMap(blueprint nodeeditor.Blueprint) map[string]nodeeditor.Node {
	nodes := make(map[string]nodeeditor.Node, len(blueprint.Nodes))
	for _, node := range blueprint.Nodes {
		nodes[node.ID] = node
	}
	return nodes
}

func containsBlueprintStack(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func stringConfig(config map[string]any, key string) string {
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

func stringSliceConfig(config map[string]any, key string) []string {
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
