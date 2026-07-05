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
				Config:  map[string]any{"source": "inline", "prompt": "Write skill instructions here."},
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
				Config:  map[string]any{"source": "default_memory"},
			},
		},
	}
}
