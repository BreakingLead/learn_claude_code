package nodeeditor

type NodeTemplate struct {
	Type        string `json:"type"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Node        Node   `json:"node"`
}

func BuiltinNodeTemplates() []NodeTemplate {
	return []NodeTemplate{
		{
			Type:        NodeTypePrompt,
			Label:       "Prompt",
			Description: "Inline prompt or context block.",
			Node: Node{
				Type:    NodeTypePrompt,
				Label:   "Prompt",
				Outputs: []Port{{ID: "prompt_out", Type: PortTypePrompt, Label: "Prompt", Direction: DirectionOutput}},
				Config:  map[string]any{"source": "inline", "prompt": "Write prompt text here."},
			},
		},
		{
			Type:        "skill",
			Label:       "Skill",
			Description: "Skill prompt from inline text or a local skill file.",
			Node: Node{
				Type:    "skill",
				Label:   "Skill",
				Outputs: []Port{{ID: "prompt_out", Type: PortTypePrompt, Label: "Prompt", Direction: DirectionOutput}},
				Config: map[string]any{
					"source": "inline",
					"prompt": "Write skill instructions here.",
					"path":   ".agents/skills/example/SKILL.md",
				},
			},
		},
		{
			Type:        NodeTypeToolset,
			Label:       "Toolset",
			Description: "A set of tool names exposed to the agent.",
			Node: Node{
				Type:    NodeTypeToolset,
				Label:   "Toolset",
				Outputs: []Port{{ID: "toolset_out", Type: PortTypeToolset, Label: "Toolset", Direction: DirectionOutput}},
				Config:  map[string]any{"tools": []string{"read_file", "glob"}},
			},
		},
		{
			Type:        NodeTypeMemory,
			Label:       "Memory",
			Description: "Runtime memory capability.",
			Node: Node{
				Type:    NodeTypeMemory,
				Label:   "Memory",
				Outputs: []Port{{ID: "memory_out", Type: PortTypeMemory, Label: "Memory", Direction: DirectionOutput}},
				Config: map[string]any{
					"source": "default_memory",
					"path":   ".agents/.memory/MEMORY.md",
				},
			},
		},
		{
			Type:        NodeTypePolicy,
			Label:       "Policy",
			Description: "Filter upstream tools and inject operating constraints.",
			Node: Node{
				Type:   NodeTypePolicy,
				Label:  "Policy",
				Inputs: []Port{{ID: "toolset_in", Type: PortTypeToolset, Label: "Tools In", Direction: DirectionInput, Multiple: true}},
				Outputs: []Port{
					{ID: "toolset_out", Type: PortTypeToolset, Label: "Tools Out", Direction: DirectionOutput},
					{ID: "prompt_out", Type: PortTypePrompt, Label: "Prompt", Direction: DirectionOutput},
				},
				Config: map[string]any{
					"allow_tools": []string{},
					"deny_tools":  []string{"write_file", "edit_file"},
					"prompt":      "Follow this policy before using tools.",
				},
			},
		},
	}
}

func CompositeNodeTemplate(definition CompositeDefinition) NodeTemplate {
	label := definition.Name
	if label == "" {
		label = definition.ID
	}
	inputs := make([]Port, 0, len(definition.Inputs))
	for _, mapping := range definition.Inputs {
		inputs = append(inputs, mapping.Port)
	}
	outputs := make([]Port, 0, len(definition.Outputs))
	for _, mapping := range definition.Outputs {
		outputs = append(outputs, mapping.Port)
	}
	return NodeTemplate{
		Type:        NodeTypeComposite,
		Label:       label,
		Description: "Reusable composite node group.",
		Node: Node{
			Type:    NodeTypeComposite,
			Label:   label,
			Inputs:  inputs,
			Outputs: outputs,
			Config:  map[string]any{"definition": definition.ID},
		},
	}
}
