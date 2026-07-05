package nodeeditor

import (
	"fmt"
	"strings"
)

type WorkflowPlanRunRequest struct {
	Input string `json:"input,omitempty"`
}

type WorkflowPlanRunResponse struct {
	OK    bool            `json:"ok"`
	Error string          `json:"error,omitempty"`
	Run   WorkflowPlanRun `json:"run,omitempty"`
}

type WorkflowPlanRun struct {
	WorkflowID  string                  `json:"workflow_id"`
	Name        string                  `json:"name"`
	SourceHash  string                  `json:"source_hash"`
	CurrentHash string                  `json:"current_hash,omitempty"`
	Stale       bool                    `json:"stale"`
	Input       string                  `json:"input"`
	Steps       []WorkflowPlanRunStep   `json:"steps,omitempty"`
	Outputs     []WorkflowPlanRunOutput `json:"outputs,omitempty"`
	Diagnostics []string                `json:"diagnostics,omitempty"`
}

type WorkflowPlanRunStep struct {
	NodeID      string                      `json:"node_id"`
	Label       string                      `json:"label"`
	BlueprintID string                      `json:"blueprint_id"`
	Instruction string                      `json:"instruction,omitempty"`
	Inputs      []WorkflowPlanRunInput      `json:"inputs,omitempty"`
	Outputs     []WorkflowPlanRunStepOutput `json:"outputs,omitempty"`
}

type WorkflowPlanRunInput struct {
	FromNode   string `json:"from_node"`
	FromPort   string `json:"from_port"`
	TargetPort string `json:"target_port"`
	Content    string `json:"content"`
}

type WorkflowPlanRunStepOutput struct {
	Port    string     `json:"port"`
	Content string     `json:"content"`
	To      []Endpoint `json:"to,omitempty"`
}

type WorkflowPlanRunOutput struct {
	NodeID  string                 `json:"node_id"`
	Label   string                 `json:"label"`
	Content string                 `json:"content"`
	Inputs  []WorkflowPlanRunInput `json:"inputs,omitempty"`
}

func (s *Store) RunWorkflowPlan(id string, input string) (WorkflowPlanRun, error) {
	plan, err := s.ReadWorkflowPlan(id)
	if err != nil {
		return WorkflowPlanRun{}, err
	}
	run := RunCompiledWorkflowPlan(plan, input)
	currentHash, stale := s.currentWorkflowHash(plan.WorkflowID, plan.SourceHash)
	run.CurrentHash = currentHash
	run.Stale = stale
	if stale {
		run.Diagnostics = append(run.Diagnostics, "saved workflow plan is stale; refresh it before using this dry-run as evidence")
	}
	return run, nil
}

func RunCompiledWorkflowPlan(plan WorkflowCompiledPlan, input string) WorkflowPlanRun {
	if strings.TrimSpace(input) == "" {
		input = "Sample workflow input"
	}
	messages := map[string]string{}
	run := WorkflowPlanRun{
		WorkflowID:  plan.WorkflowID,
		Name:        plan.Name,
		SourceHash:  plan.SourceHash,
		Input:       input,
		Diagnostics: append([]string(nil), plan.Diagnostics...),
	}

	for _, agentRun := range plan.AgentRuns {
		stepInputs := workflowPlanRunInputs(agentRun.Inputs, messages, input)
		content := workflowPlanDryRunContent(agentRun, stepInputs)
		step := WorkflowPlanRunStep{
			NodeID:      agentRun.NodeID,
			Label:       agentRun.Label,
			BlueprintID: agentRun.BlueprintID,
			Instruction: agentRun.Instruction,
			Inputs:      stepInputs,
		}
		for _, output := range agentRun.Outputs {
			stepOutput := WorkflowPlanRunStepOutput{
				Port:    output.Port,
				Content: content,
				To:      append([]Endpoint(nil), output.To...),
			}
			step.Outputs = append(step.Outputs, stepOutput)
			messages[endpointKey(Endpoint{Node: agentRun.NodeID, Port: output.Port})] = content
		}
		run.Steps = append(run.Steps, step)
	}

	for _, output := range plan.Outputs {
		inputs := workflowPlanRunInputs(output.Inputs, messages, input)
		run.Outputs = append(run.Outputs, WorkflowPlanRunOutput{
			NodeID:  output.NodeID,
			Label:   output.Label,
			Content: workflowPlanOutputContent(inputs),
			Inputs:  inputs,
		})
	}
	return run
}

func workflowPlanRunInputs(inputs []WorkflowCompiledInput, messages map[string]string, fallback string) []WorkflowPlanRunInput {
	runInputs := make([]WorkflowPlanRunInput, 0, len(inputs))
	for _, input := range inputs {
		key := endpointKey(Endpoint{Node: input.FromNode, Port: input.FromPort})
		content, ok := messages[key]
		if !ok {
			content = fallback
			messages[key] = content
		}
		runInputs = append(runInputs, WorkflowPlanRunInput{
			FromNode:   input.FromNode,
			FromPort:   input.FromPort,
			TargetPort: input.TargetPort,
			Content:    content,
		})
	}
	return runInputs
}

func workflowPlanDryRunContent(run WorkflowCompiledRun, inputs []WorkflowPlanRunInput) string {
	label := run.Label
	if strings.TrimSpace(label) == "" {
		label = run.NodeID
	}
	blueprint := run.BlueprintID
	if strings.TrimSpace(blueprint) == "" {
		blueprint = "unknown"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[Dry run %s via %s]", label, blueprint)
	if strings.TrimSpace(run.Instruction) != "" {
		fmt.Fprintf(&b, "\nInstruction: %s", run.Instruction)
	}
	if len(run.ToolNames) > 0 {
		fmt.Fprintf(&b, "\nTools: %s", strings.Join(run.ToolNames, ", "))
	}
	if len(inputs) == 0 {
		b.WriteString("\nInputs: (none)")
		return b.String()
	}
	b.WriteString("\nInputs:")
	for _, input := range inputs {
		fmt.Fprintf(&b, "\n- %s.%s -> %s: %s", input.FromNode, input.FromPort, input.TargetPort, input.Content)
	}
	return b.String()
}

func workflowPlanOutputContent(inputs []WorkflowPlanRunInput) string {
	var parts []string
	for _, input := range inputs {
		if strings.TrimSpace(input.Content) != "" {
			parts = append(parts, input.Content)
		}
	}
	return strings.Join(parts, "\n\n")
}
