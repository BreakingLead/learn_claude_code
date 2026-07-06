package nodeeditor

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

type PromptPreviewBlock struct {
	NodeID  string `json:"node_id"`
	Name    string `json:"name"`
	Source  string `json:"source"`
	Preview string `json:"preview"`
}

func PromptPreview(blueprint Blueprint, resolved ResolvedAgentDefinition) []PromptPreviewBlock {
	nodes := nodeMap(blueprint)
	var blocks []PromptPreviewBlock
	for _, nodeID := range resolved.PromptNodes {
		source := ResolvePromptSource(blueprint, nodeID, EvaluationContext{Now: time.Now()})
		if !source.OK {
			continue
		}
		node := source.Node
		if node.ID != nodeID {
			selectedBy := nodeID
			if selectNode, ok := nodes[nodeID]; ok && strings.TrimSpace(selectNode.Label) != "" {
				selectedBy = selectNode.Label
			}
			for _, block := range promptPreviewForNode(node) {
				block.Name = fmt.Sprintf("%s via %s", block.Name, selectedBy)
				blocks = append(blocks, block)
			}
			continue
		}
		if _, ok := nodes[nodeID]; ok {
			blocks = append(blocks, promptPreviewForNode(node)...)
		}
	}
	for _, nodeID := range resolved.MemoryNodes {
		if node, ok := nodes[nodeID]; ok {
			blocks = append(blocks, promptPreviewForNode(node)...)
		}
	}
	return blocks
}

func promptPreviewForNode(node Node) []PromptPreviewBlock {
	source := stringNodeConfig(node.Config, "source")
	if source == "" && node.Type == NodeTypePolicy {
		source = "policy"
	}
	name := strings.TrimSpace(node.Label)
	if name == "" {
		name = node.ID
	}
	block := PromptPreviewBlock{
		NodeID: node.ID,
		Name:   name,
		Source: source,
	}
	switch source {
	case "inline":
		block.Source = "blueprint inline"
		block.Preview = truncatePreview(stringNodeConfig(node.Config, "prompt"), 240)
	case "policy":
		block.Source = "blueprint policy"
		block.Preview = truncatePreview(stringNodeConfig(node.Config, "prompt"), 240)
	case "skill_file", "file":
		block.Source = "skill file"
		block.Preview = stringNodeConfig(node.Config, "path")
	case "active_mode":
		block.Source = "active mode"
		block.Preview = "Uses the active mode prompt at runtime."
	case "project_files":
		block.Source = "project files"
		block.Preview = strings.Join(stringSliceNodeConfig(node.Config, "files"), ", ")
	case "default_memory", "memory_index":
		block.Source = "memory index"
		path := strings.TrimSpace(stringNodeConfig(node.Config, "path"))
		if path == "" {
			path = filepath.ToSlash(filepath.Join(".agents", ".memory", "MEMORY.md"))
		}
		block.Preview = path
	case "module_prompt":
		block.Source = "module prompt"
		moduleID := stringNodeConfig(node.Config, "module")
		if moduleID == "" {
			block.Preview = "Uses all enabled module prompt blocks at runtime."
		} else {
			block.Preview = "Uses module prompt: " + moduleID
		}
	default:
		if source == "" {
			block.Source = node.Type
		}
		block.Preview = truncatePreview(stringNodeConfig(node.Config, "prompt"), 240)
	}
	if strings.TrimSpace(block.Preview) == "" {
		block.Preview = "(empty)"
	}
	return []PromptPreviewBlock{block}
}

func stringNodeConfig(config map[string]any, key string) string {
	if config == nil {
		return ""
	}
	value, ok := config[key]
	if !ok {
		return ""
	}
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func truncatePreview(text string, limit int) string {
	text = strings.TrimSpace(text)
	if limit <= 0 || len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}
