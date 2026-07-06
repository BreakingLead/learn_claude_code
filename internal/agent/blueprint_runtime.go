package agent

import (
	nodeeditor "bee_agent/internal/agent/node_editor"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"
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
	capabilities := nodeeditor.EffectiveToolNames(rt.blueprint.Graph, rt.blueprint.Resolved)
	if !capabilities.Resolved {
		return fallback
	}
	return capabilities.ToolNames
}

func (rt *agentRuntime) blueprintPromptBlocks(toolNames []string) ([]PromptBlock, bool) {
	if rt == nil || rt.blueprint == nil || !rt.blueprint.Enabled || rt.blueprint.Error != "" {
		return nil, false
	}
	nodes := blueprintNodeMap(rt.blueprint.Graph)
	var blocks []PromptBlock
	for _, nodeID := range rt.blueprint.Resolved.PromptNodes {
		source := nodeeditor.ResolvePromptSource(rt.blueprint.Graph, nodeID, nodeeditor.EvaluationContext{Now: time.Now()})
		if !source.OK {
			continue
		}
		selectedBlocks := rt.promptBlocksForBlueprintNode(source.Node, toolNames)
		if source.Node.ID != nodeID {
			for index := range selectedBlocks {
				selectedBlocks[index].Name = fmt.Sprintf("%s via %s", selectedBlocks[index].Name, nodeID)
			}
		}
		blocks = append(blocks, selectedBlocks...)
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
