package nodeeditor

import "strings"

// ConfigDiagnostics 返回不会阻止运行的编辑器提示。
// 这些提示用于发现“节点能连上，但运行时可能没有效果”的配置问题。
func ConfigDiagnostics(blueprint Blueprint, resolved ResolvedAgentDefinition) []string {
	var diagnostics []string
	for _, node := range blueprint.Nodes {
		switch node.Type {
		case NodeTypePrompt, "skill":
			diagnostics = append(diagnostics, promptNodeDiagnostics(node)...)
		case NodeTypeMemory:
			diagnostics = append(diagnostics, memoryNodeDiagnostics(node)...)
		case NodeTypeToolset:
			if len(nonEmptyStrings(stringSliceNodeConfig(node.Config, "tools"))) == 0 {
				diagnostics = append(diagnostics, nodeDiagnostic(node, "toolset has no tools"))
			}
		case NodeTypePolicy:
			diagnostics = append(diagnostics, policyNodeDiagnostics(node)...)
		}
	}
	if len(resolved.PromptNodes) == 0 {
		diagnostics = append(diagnostics, "agent has no connected prompt nodes")
	}
	if len(resolved.ToolsetNodes) == 0 {
		diagnostics = append(diagnostics, "agent has no connected toolset nodes")
	}
	return diagnostics
}

func promptNodeDiagnostics(node Node) []string {
	source := stringNodeConfig(node.Config, "source")
	switch source {
	case "inline":
		if stringNodeConfig(node.Config, "prompt") == "" {
			return []string{nodeDiagnostic(node, "inline prompt is empty")}
		}
	case "skill_file", "file":
		if stringNodeConfig(node.Config, "path") == "" {
			return []string{nodeDiagnostic(node, source+" source requires path")}
		}
	case "project_files":
		if len(nonEmptyStrings(stringSliceNodeConfig(node.Config, "files"))) == 0 {
			return []string{nodeDiagnostic(node, "project_files source requires files")}
		}
	case "active_mode", "module_prompt":
	case "":
		return []string{nodeDiagnostic(node, "prompt source is empty")}
	default:
		return []string{nodeDiagnostic(node, "unknown prompt source "+source)}
	}
	return nil
}

func memoryNodeDiagnostics(node Node) []string {
	source := stringNodeConfig(node.Config, "source")
	switch source {
	case "default_memory", "memory_index", "":
		return nil
	default:
		return []string{nodeDiagnostic(node, "unknown memory source "+source)}
	}
}

func policyNodeDiagnostics(node Node) []string {
	var diagnostics []string
	allow := nonEmptyStrings(stringSliceNodeConfig(node.Config, "allow_tools"))
	deny := nonEmptyStrings(stringSliceNodeConfig(node.Config, "deny_tools"))
	if len(allow) == 0 && len(deny) == 0 && stringNodeConfig(node.Config, "prompt") == "" {
		diagnostics = append(diagnostics, nodeDiagnostic(node, "policy has no tool filter or prompt"))
	}
	for _, name := range allow {
		if containsDiagnosticString(deny, name) {
			diagnostics = append(diagnostics, nodeDiagnostic(node, "tool "+name+" appears in both allow_tools and deny_tools"))
		}
	}
	return diagnostics
}

func nodeDiagnostic(node Node, message string) string {
	name := strings.TrimSpace(node.Label)
	if name == "" {
		name = node.ID
	}
	return name + " (" + node.ID + "): " + message
}

func nonEmptyStrings(values []string) []string {
	var result []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

func containsDiagnosticString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
