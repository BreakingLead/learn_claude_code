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
	state := &runtimeBlueprint{
		Path:    rt.config.DefaultBlueprintPath,
		Enabled: rt.config.UseBlueprint,
	}
	blueprint, err := nodeeditor.ReadBlueprint(rt.config.DefaultBlueprintPath)
	if err != nil {
		state.Error = err.Error()
		return state
	}
	resolved, err := nodeeditor.Resolve(blueprint)
	if err != nil {
		state.Graph = blueprint
		state.Error = err.Error()
		return state
	}
	state.Graph = blueprint
	state.Resolved = resolved
	return state
}

func (rt *agentRuntime) blueprintToolNames(fallback []string) []string {
	if rt == nil || rt.blueprint == nil || !rt.blueprint.Enabled || rt.blueprint.Error != "" {
		return fallback
	}
	nodes := blueprintNodeMap(rt.blueprint.Graph)
	toolSet := map[string]bool{}
	for _, nodeID := range rt.blueprint.Resolved.ToolsetNodes {
		node, ok := nodes[nodeID]
		if !ok {
			continue
		}
		for _, name := range stringSliceConfig(node.Config, "tools") {
			if strings.TrimSpace(name) != "" {
				toolSet[name] = true
			}
		}
	}
	if len(toolSet) == 0 {
		return fallback
	}
	names := make([]string, 0, len(toolSet))
	for name := range toolSet {
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
	if len(blocks) == 0 {
		return nil, false
	}
	return blocks, true
}

func (rt *agentRuntime) promptBlocksForBlueprintNode(node nodeeditor.Node, toolNames []string) []PromptBlock {
	source := stringConfig(node.Config, "source")
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
