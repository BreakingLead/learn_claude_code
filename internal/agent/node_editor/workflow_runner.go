package nodeeditor

import (
	"context"
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
	ID          string                  `json:"id,omitempty"`
	WorkflowID  string                  `json:"workflow_id"`
	Name        string                  `json:"name"`
	CreatedAt   string                  `json:"created_at,omitempty"`
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

type PlanExecutor struct {
	Invoker AgentInvoker
}

type AgentInvoker interface {
	InvokeAgent(context.Context, AgentInvocation) (AgentInvocationResult, error)
}

type AgentInvocation struct {
	Run    WorkflowCompiledRun    `json:"run"`
	Inputs []WorkflowPlanRunInput `json:"inputs,omitempty"`
}

type AgentInvocationResult struct {
	Content     string   `json:"content"`
	Diagnostics []string `json:"diagnostics,omitempty"`
}

type DryRunAgentInvoker struct{}

func NewDryRunPlanExecutor() PlanExecutor {
	return PlanExecutor{Invoker: DryRunAgentInvoker{}}
}

func (s *Store) RunWorkflowPlan(ctx context.Context, id string, input string) (WorkflowPlanRun, error) {
	plan, err := s.ReadWorkflowPlan(id)
	if err != nil {
		return WorkflowPlanRun{}, err
	}
	run, err := NewDryRunPlanExecutor().Execute(ctx, plan, input)
	if err != nil {
		return WorkflowPlanRun{}, err
	}
	currentHash, stale := s.currentWorkflowHash(plan.WorkflowID, plan.SourceHash)
	run.CurrentHash = currentHash
	run.Stale = stale
	if stale {
		run.Diagnostics = append(run.Diagnostics, "saved workflow plan is stale; refresh it before using this dry-run as evidence")
	}
	return run, nil
}

func RunCompiledWorkflowPlan(plan WorkflowCompiledPlan, input string) WorkflowPlanRun {
	run, err := NewDryRunPlanExecutor().Execute(context.Background(), plan, input)
	if err != nil {
		return WorkflowPlanRun{
			WorkflowID:  plan.WorkflowID,
			Name:        plan.Name,
			SourceHash:  plan.SourceHash,
			Input:       input,
			Diagnostics: []string{err.Error()},
		}
	}
	return run
}

func (e PlanExecutor) Execute(ctx context.Context, plan WorkflowCompiledPlan, input string) (WorkflowPlanRun, error) {
	invoker := e.Invoker
	if invoker == nil {
		invoker = DryRunAgentInvoker{}
	}
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
		if err := ctx.Err(); err != nil {
			return WorkflowPlanRun{}, err
		}
		stepInputs := workflowPlanRunInputs(agentRun.Inputs, messages, input)
		result, err := invoker.InvokeAgent(ctx, AgentInvocation{Run: agentRun, Inputs: stepInputs})
		if err != nil {
			return WorkflowPlanRun{}, fmt.Errorf("invoke agent %q: %w", agentRun.NodeID, err)
		}
		step := WorkflowPlanRunStep{
			NodeID:      agentRun.NodeID,
			Label:       agentRun.Label,
			BlueprintID: agentRun.BlueprintID,
			Instruction: agentRun.Instruction,
			Inputs:      stepInputs,
		}
		run.Diagnostics = append(run.Diagnostics, result.Diagnostics...)
		for _, output := range agentRun.Outputs {
			stepOutput := WorkflowPlanRunStepOutput{
				Port:    output.Port,
				Content: result.Content,
				To:      append([]Endpoint(nil), output.To...),
			}
			step.Outputs = append(step.Outputs, stepOutput)
			messages[endpointKey(Endpoint{Node: agentRun.NodeID, Port: output.Port})] = result.Content
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
	return run, nil
}

func (DryRunAgentInvoker) InvokeAgent(ctx context.Context, invocation AgentInvocation) (AgentInvocationResult, error) {
	if err := ctx.Err(); err != nil {
		return AgentInvocationResult{}, err
	}
	return AgentInvocationResult{
		Content: workflowPlanDryRunContent(invocation.Run, invocation.Inputs),
	}, nil
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
