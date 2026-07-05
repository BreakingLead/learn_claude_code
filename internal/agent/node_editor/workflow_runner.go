package nodeeditor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	DefaultWorkflowRunTimeoutMS = 30000
	WorkflowRunStatusCompleted  = "completed"
	WorkflowRunStatusFailed     = "failed"
)

type WorkflowPlanRunRequest struct {
	Input           string   `json:"input,omitempty"`
	ExecutionMode   string   `json:"execution_mode,omitempty"`
	ExternalCommand []string `json:"external_command,omitempty"`
	TimeoutMS       int      `json:"timeout_ms,omitempty"`
}

type WorkflowPlanRun struct {
	ID              string                  `json:"id,omitempty"`
	WorkflowID      string                  `json:"workflow_id"`
	Name            string                  `json:"name"`
	CreatedAt       string                  `json:"created_at,omitempty"`
	StartedAt       string                  `json:"started_at,omitempty"`
	FinishedAt      string                  `json:"finished_at,omitempty"`
	DurationMS      int64                   `json:"duration_ms,omitempty"`
	ExecutionMode   string                  `json:"execution_mode,omitempty"`
	ExternalCommand []string                `json:"external_command,omitempty"`
	TimeoutMS       int                     `json:"timeout_ms,omitempty"`
	RerunOf         string                  `json:"rerun_of,omitempty"`
	Status          string                  `json:"status,omitempty"`
	Error           string                  `json:"error,omitempty"`
	SourceHash      string                  `json:"source_hash"`
	CurrentHash     string                  `json:"current_hash,omitempty"`
	Stale           bool                    `json:"stale"`
	PlanSnapshot    *WorkflowCompiledPlan   `json:"plan_snapshot,omitempty"`
	Input           string                  `json:"input"`
	Steps           []WorkflowPlanRunStep   `json:"steps,omitempty"`
	Outputs         []WorkflowPlanRunOutput `json:"outputs,omitempty"`
	Diagnostics     []string                `json:"diagnostics,omitempty"`
}

type WorkflowPlanRunStep struct {
	NodeID      string                      `json:"node_id"`
	Label       string                      `json:"label"`
	BlueprintID string                      `json:"blueprint_id"`
	Instruction string                      `json:"instruction,omitempty"`
	Status      string                      `json:"status,omitempty"`
	Error       string                      `json:"error,omitempty"`
	StartedAt   string                      `json:"started_at,omitempty"`
	FinishedAt  string                      `json:"finished_at,omitempty"`
	DurationMS  int64                       `json:"duration_ms,omitempty"`
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
	Invoker   AgentInvoker
	Timeout   time.Duration
	TimeoutMS int
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

type ExternalCommandAgentInvoker struct {
	Command []string
}

func NewDryRunPlanExecutor() PlanExecutor {
	return PlanExecutor{
		Invoker:   DryRunAgentInvoker{},
		Timeout:   time.Duration(DefaultWorkflowRunTimeoutMS) * time.Millisecond,
		TimeoutMS: DefaultWorkflowRunTimeoutMS,
	}
}

func NewPlanExecutor(request WorkflowPlanRunRequest) (PlanExecutor, string, error) {
	mode := strings.TrimSpace(request.ExecutionMode)
	if mode == "" {
		mode = "dry_run"
	}
	timeoutMS := request.TimeoutMS
	if timeoutMS <= 0 {
		timeoutMS = DefaultWorkflowRunTimeoutMS
	}
	timeout := time.Duration(timeoutMS) * time.Millisecond
	switch mode {
	case "dry_run":
		return PlanExecutor{Invoker: DryRunAgentInvoker{}, Timeout: timeout, TimeoutMS: timeoutMS}, mode, nil
	case "external_command":
		if len(request.ExternalCommand) == 0 || strings.TrimSpace(request.ExternalCommand[0]) == "" {
			return PlanExecutor{}, "", fmt.Errorf("external_command execution requires external_command")
		}
		return PlanExecutor{
			Invoker:   ExternalCommandAgentInvoker{Command: append([]string(nil), request.ExternalCommand...)},
			Timeout:   timeout,
			TimeoutMS: timeoutMS,
		}, mode, nil
	default:
		return PlanExecutor{}, "", fmt.Errorf("unknown execution mode %q", mode)
	}
}

func (s *Store) RunWorkflowPlan(ctx context.Context, id string, request WorkflowPlanRunRequest) (WorkflowPlanRun, error) {
	plan, err := s.ReadWorkflowPlan(id)
	if err != nil {
		return WorkflowPlanRun{}, err
	}
	executor, mode, err := NewPlanExecutor(request)
	if err != nil {
		return WorkflowPlanRun{}, err
	}
	run, err := executor.Execute(ctx, plan, request.Input)
	run.ExecutionMode = mode
	if mode == "external_command" {
		run.ExternalCommand = append([]string(nil), request.ExternalCommand...)
	}
	currentHash, stale := s.currentWorkflowHash(plan.WorkflowID, plan.SourceHash)
	run.CurrentHash = currentHash
	run.Stale = stale
	if stale {
		run.Diagnostics = append(run.Diagnostics, "saved workflow plan is stale; refresh it before using this dry-run as evidence")
	}
	return run, err
}

func RunCompiledWorkflowPlan(plan WorkflowCompiledPlan, input string) WorkflowPlanRun {
	run, err := NewDryRunPlanExecutor().Execute(context.Background(), plan, input)
	if err != nil {
		return WorkflowPlanRun{
			WorkflowID:    plan.WorkflowID,
			Name:          plan.Name,
			ExecutionMode: "dry_run",
			Status:        WorkflowRunStatusFailed,
			Error:         err.Error(),
			SourceHash:    plan.SourceHash,
			Input:         input,
			Diagnostics:   []string{err.Error()},
		}
	}
	run.ExecutionMode = "dry_run"
	return run
}

func (e PlanExecutor) Execute(ctx context.Context, plan WorkflowCompiledPlan, input string) (WorkflowPlanRun, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if e.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.Timeout)
		defer cancel()
	}
	invoker := e.Invoker
	if invoker == nil {
		invoker = DryRunAgentInvoker{}
	}
	if strings.TrimSpace(input) == "" {
		input = "Sample workflow input"
	}
	runStart := time.Now()
	messages := map[string]string{}
	planSnapshot := plan
	run := WorkflowPlanRun{
		WorkflowID:   plan.WorkflowID,
		Name:         plan.Name,
		StartedAt:    runStart.UTC().Format(time.RFC3339Nano),
		TimeoutMS:    e.TimeoutMS,
		Status:       WorkflowRunStatusCompleted,
		SourceHash:   plan.SourceHash,
		PlanSnapshot: &planSnapshot,
		Input:        input,
		Diagnostics:  append([]string(nil), plan.Diagnostics...),
	}

	for _, agentRun := range plan.AgentRuns {
		if err := ctx.Err(); err != nil {
			return failWorkflowPlanRun(run, err), err
		}
		stepInputs := workflowPlanRunInputs(agentRun.Inputs, messages, input)
		stepStart := time.Now()
		result, err := invoker.InvokeAgent(ctx, AgentInvocation{Run: agentRun, Inputs: stepInputs})
		stepFinish := time.Now()
		step := newWorkflowPlanRunStep(agentRun, stepInputs, stepStart, stepFinish)
		if err != nil {
			err = fmt.Errorf("invoke agent %q: %w", agentRun.NodeID, err)
			run.Steps = append(run.Steps, failWorkflowPlanRunStep(step, err))
			return failWorkflowPlanRun(run, err), err
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
	finishWorkflowPlanRun(&run, runStart, time.Now())
	return run, nil
}

func newWorkflowPlanRunStep(agentRun WorkflowCompiledRun, inputs []WorkflowPlanRunInput, started time.Time, finished time.Time) WorkflowPlanRunStep {
	return WorkflowPlanRunStep{
		NodeID:      agentRun.NodeID,
		Label:       agentRun.Label,
		BlueprintID: agentRun.BlueprintID,
		Instruction: agentRun.Instruction,
		Status:      WorkflowRunStatusCompleted,
		StartedAt:   started.UTC().Format(time.RFC3339Nano),
		FinishedAt:  finished.UTC().Format(time.RFC3339Nano),
		DurationMS:  durationMillis(finished.Sub(started)),
		Inputs:      inputs,
	}
}

func failWorkflowPlanRunStep(step WorkflowPlanRunStep, err error) WorkflowPlanRunStep {
	step.Status = WorkflowRunStatusFailed
	if err != nil {
		step.Error = err.Error()
	}
	return step
}

func failWorkflowPlanRun(run WorkflowPlanRun, err error) WorkflowPlanRun {
	run.Status = WorkflowRunStatusFailed
	if run.StartedAt != "" {
		finishWorkflowPlanRun(&run, parseWorkflowRunTime(run.StartedAt), time.Now())
	}
	if err != nil {
		run.Error = err.Error()
		run.Diagnostics = append(run.Diagnostics, err.Error())
	}
	return run
}

func finishWorkflowPlanRun(run *WorkflowPlanRun, started time.Time, finished time.Time) {
	if run == nil {
		return
	}
	run.FinishedAt = finished.UTC().Format(time.RFC3339Nano)
	if !started.IsZero() {
		run.DurationMS = durationMillis(finished.Sub(started))
	}
}

func parseWorkflowRunTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func durationMillis(duration time.Duration) int64 {
	if duration < 0 {
		return 0
	}
	return duration.Milliseconds()
}

func (DryRunAgentInvoker) InvokeAgent(ctx context.Context, invocation AgentInvocation) (AgentInvocationResult, error) {
	if err := ctx.Err(); err != nil {
		return AgentInvocationResult{}, err
	}
	return AgentInvocationResult{
		Content: workflowPlanDryRunContent(invocation.Run, invocation.Inputs),
	}, nil
}

func (i ExternalCommandAgentInvoker) InvokeAgent(ctx context.Context, invocation AgentInvocation) (AgentInvocationResult, error) {
	if len(i.Command) == 0 || strings.TrimSpace(i.Command[0]) == "" {
		return AgentInvocationResult{}, fmt.Errorf("external command is required")
	}
	raw, err := json.Marshal(invocation)
	if err != nil {
		return AgentInvocationResult{}, err
	}
	cmd := exec.CommandContext(ctx, i.Command[0], i.Command[1:]...)
	cmd.Stdin = bytes.NewReader(raw)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return AgentInvocationResult{}, ctxErr
		}
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return AgentInvocationResult{}, fmt.Errorf("external command failed: %s", message)
	}
	output := strings.TrimSpace(stdout.String())
	if output == "" {
		return AgentInvocationResult{}, fmt.Errorf("external command returned empty output")
	}
	var result AgentInvocationResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		return AgentInvocationResult{Content: output}, nil
	}
	if strings.TrimSpace(result.Content) == "" {
		return AgentInvocationResult{}, fmt.Errorf("external command result content is required")
	}
	return result, nil
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
