package nodeeditor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	DefaultWorkflowRunTimeoutMS = 30000
	WorkflowRunStatusCompleted  = "completed"
	WorkflowRunStatusFailed     = "failed"

	WorkflowExecutionModeDryRun          = "dry_run"
	WorkflowExecutionModeExternalCommand = "external_command"
	WorkflowExecutionModeBeeAgent        = "bee_agent"
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

type PlanExecutorFactory func(WorkflowPlanRunRequest) (PlanExecutor, string, error)

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
		mode = WorkflowExecutionModeDryRun
	}
	timeoutMS := request.TimeoutMS
	if timeoutMS <= 0 {
		timeoutMS = DefaultWorkflowRunTimeoutMS
	}
	timeout := time.Duration(timeoutMS) * time.Millisecond
	switch mode {
	case WorkflowExecutionModeDryRun:
		return PlanExecutor{Invoker: DryRunAgentInvoker{}, Timeout: timeout, TimeoutMS: timeoutMS}, mode, nil
	case WorkflowExecutionModeExternalCommand:
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

func WorkflowRunTimeout(request WorkflowPlanRunRequest) (time.Duration, int) {
	timeoutMS := request.TimeoutMS
	if timeoutMS <= 0 {
		timeoutMS = DefaultWorkflowRunTimeoutMS
	}
	return time.Duration(timeoutMS) * time.Millisecond, timeoutMS
}

func (s *Store) RunWorkflowPlan(ctx context.Context, id string, request WorkflowPlanRunRequest) (WorkflowPlanRun, error) {
	plan, err := s.ReadWorkflowPlan(id)
	if err != nil {
		return WorkflowPlanRun{}, err
	}
	executorFactory := s.planExecutorFactory
	if executorFactory == nil {
		executorFactory = NewPlanExecutor
	}
	executor, mode, err := executorFactory(request)
	if err != nil {
		return WorkflowPlanRun{}, err
	}
	run, err := executor.Execute(ctx, plan, request.Input)
	run.ExecutionMode = mode
	if mode == WorkflowExecutionModeExternalCommand {
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
	messages := workflowInitialMessages(plan)
	producedMessages := workflowProducedMessageEndpoints(plan)
	executables := workflowExecutableNodes(plan)
	producerNodeIDs := workflowProducerNodeIDs(executables)
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

	execCtx, cancelExecution := context.WithCancel(ctx)
	defer cancelExecution()
	started := map[string]bool{}
	completed := map[string]bool{}
	stepResults := make([]WorkflowPlanRunStep, len(plan.AgentRuns))
	remaining := len(executables)
	for remaining > 0 {
		if err := execCtx.Err(); err != nil {
			return failWorkflowPlanRun(run, err), err
		}
		var ready []workflowExecutableNode
		for _, executable := range executables {
			if started[executable.NodeID] || !workflowExecutableDependenciesCompleted(executable, producerNodeIDs, completed) {
				continue
			}
			started[executable.NodeID] = true
			ready = append(ready, executable)
		}
		if len(ready) == 0 {
			err := workflowExecutionDeadlockError(executables, started, completed, producerNodeIDs)
			return failWorkflowPlanRun(run, err), err
		}

		results := make(chan workflowExecutableRunResult, len(ready))
		var wg sync.WaitGroup
		for _, readyRun := range ready {
			wg.Add(1)
			go func(readyRun workflowExecutableNode) {
				defer wg.Done()
				results <- executeWorkflowNode(execCtx, invoker, readyRun, messages, input, producedMessages)
			}(readyRun)
		}
		go func() {
			wg.Wait()
			close(results)
		}()

		var batchResults []workflowExecutableRunResult
		var firstFailure *workflowExecutableRunResult
		for result := range results {
			batchResults = append(batchResults, result)
			if result.Err != nil {
				cancelExecution()
			}
			if result.Err != nil && (firstFailure == nil || result.Order < firstFailure.Order) {
				copyResult := result
				firstFailure = &copyResult
			}
		}
		if firstFailure != nil {
			cancelExecution()
			if firstFailure.AgentIndex >= 0 {
				stepResults[firstFailure.AgentIndex] = firstFailure.Step
			}
			run.Steps = appendCompletedWorkflowSteps(stepResults)
			return failWorkflowPlanRun(run, firstFailure.Err), firstFailure.Err
		}
		for _, result := range batchResults {
			if result.AgentIndex >= 0 {
				stepResults[result.AgentIndex] = result.Step
			}
			completed[result.NodeID] = true
			run.Diagnostics = append(run.Diagnostics, result.Diagnostics...)
			for key, content := range result.Messages {
				messages[key] = content
			}
			remaining--
		}
	}
	run.Steps = appendCompletedWorkflowSteps(stepResults)

	for _, output := range plan.Outputs {
		inputs, err := workflowPlanRunInputs(output.Inputs, messages, input, producedMessages)
		if err != nil {
			err = fmt.Errorf("prepare output %q inputs: %w", output.NodeID, err)
			return failWorkflowPlanRun(run, err), err
		}
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

type workflowExecutableKind string

const (
	workflowExecutableAgent     workflowExecutableKind = "agent"
	workflowExecutableTransform workflowExecutableKind = "transform"
)

type workflowExecutableNode struct {
	Order      int
	Kind       workflowExecutableKind
	NodeID     string
	AgentIndex int
	Agent      WorkflowCompiledRun
	Transform  WorkflowCompiledTransform
}

type workflowExecutableRunResult struct {
	Order       int
	NodeID      string
	AgentIndex  int
	Step        WorkflowPlanRunStep
	Messages    map[string]string
	Diagnostics []string
	Err         error
}

func executeWorkflowNode(ctx context.Context, invoker AgentInvoker, readyRun workflowExecutableNode, messages map[string]string, input string, producedMessages map[string]bool) workflowExecutableRunResult {
	if readyRun.Kind == workflowExecutableTransform {
		return executeWorkflowTransform(readyRun, messages, input, producedMessages)
	}
	return executeWorkflowAgent(ctx, invoker, readyRun, messages, input, producedMessages)
}

func executeWorkflowAgent(ctx context.Context, invoker AgentInvoker, readyRun workflowExecutableNode, messages map[string]string, input string, producedMessages map[string]bool) workflowExecutableRunResult {
	agentRun := readyRun.Agent
	stepStart := time.Now()
	stepInputs, err := workflowPlanRunInputs(agentRun.Inputs, messages, input, producedMessages)
	if err != nil {
		err = fmt.Errorf("prepare agent %q inputs: %w", agentRun.NodeID, err)
		stepFinish := time.Now()
		step := newWorkflowPlanRunStep(agentRun, stepInputs, stepStart, stepFinish)
		return workflowExecutableRunResult{Order: readyRun.Order, NodeID: readyRun.NodeID, AgentIndex: readyRun.AgentIndex, Step: failWorkflowPlanRunStep(step, err), Err: err}
	}
	result, err := invoker.InvokeAgent(ctx, AgentInvocation{Run: agentRun, Inputs: stepInputs})
	stepFinish := time.Now()
	step := newWorkflowPlanRunStep(agentRun, stepInputs, stepStart, stepFinish)
	if err != nil {
		err = fmt.Errorf("invoke agent %q: %w", agentRun.NodeID, err)
		return workflowExecutableRunResult{Order: readyRun.Order, NodeID: readyRun.NodeID, AgentIndex: readyRun.AgentIndex, Step: failWorkflowPlanRunStep(step, err), Err: err}
	}
	messagesOut := map[string]string{}
	for _, output := range agentRun.Outputs {
		stepOutput := WorkflowPlanRunStepOutput{
			Port:    output.Port,
			Content: result.Content,
			To:      append([]Endpoint(nil), output.To...),
		}
		step.Outputs = append(step.Outputs, stepOutput)
		messagesOut[endpointKey(Endpoint{Node: agentRun.NodeID, Port: output.Port})] = result.Content
	}
	return workflowExecutableRunResult{
		Order:       readyRun.Order,
		NodeID:      readyRun.NodeID,
		AgentIndex:  readyRun.AgentIndex,
		Step:        step,
		Messages:    messagesOut,
		Diagnostics: result.Diagnostics,
	}
}

func executeWorkflowTransform(readyRun workflowExecutableNode, messages map[string]string, input string, producedMessages map[string]bool) workflowExecutableRunResult {
	transform := readyRun.Transform
	inputs, err := workflowPlanRunInputs(transform.Inputs, messages, input, producedMessages)
	if err != nil {
		err = fmt.Errorf("prepare transform %q inputs: %w", transform.NodeID, err)
		return workflowExecutableRunResult{Order: readyRun.Order, NodeID: readyRun.NodeID, AgentIndex: -1, Err: err}
	}
	content := workflowTransformContent(transform, inputs)
	messagesOut := map[string]string{}
	for _, output := range transform.Outputs {
		messagesOut[endpointKey(Endpoint{Node: transform.NodeID, Port: output.Port})] = content
	}
	return workflowExecutableRunResult{
		Order:      readyRun.Order,
		NodeID:     readyRun.NodeID,
		AgentIndex: -1,
		Messages:   messagesOut,
	}
}

func workflowExecutableDependenciesCompleted(executable workflowExecutableNode, producerNodeIDs map[string]bool, completed map[string]bool) bool {
	for _, input := range workflowExecutableInputs(executable) {
		if producerNodeIDs[input.FromNode] && !completed[input.FromNode] {
			return false
		}
	}
	return true
}

func workflowExecutionDeadlockError(executables []workflowExecutableNode, started map[string]bool, completed map[string]bool, producerNodeIDs map[string]bool) error {
	var blocked []string
	for _, executable := range executables {
		if started[executable.NodeID] {
			continue
		}
		var waiting []string
		for _, input := range workflowExecutableInputs(executable) {
			if producerNodeIDs[input.FromNode] && !completed[input.FromNode] {
				waiting = append(waiting, input.FromNode+"."+input.FromPort)
			}
		}
		if len(waiting) == 0 {
			blocked = append(blocked, executable.NodeID)
			continue
		}
		blocked = append(blocked, fmt.Sprintf("%s waits for %s", executable.NodeID, strings.Join(waiting, ", ")))
	}
	if len(blocked) == 0 {
		return fmt.Errorf("workflow execution deadlock")
	}
	return fmt.Errorf("workflow execution deadlock: %s", strings.Join(blocked, "; "))
}

func appendCompletedWorkflowSteps(steps []WorkflowPlanRunStep) []WorkflowPlanRunStep {
	result := make([]WorkflowPlanRunStep, 0, len(steps))
	for _, step := range steps {
		if step.NodeID != "" {
			result = append(result, step)
		}
	}
	return result
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

func workflowPlanRunInputs(inputs []WorkflowCompiledInput, messages map[string]string, fallback string, producedMessages map[string]bool) ([]WorkflowPlanRunInput, error) {
	runInputs := make([]WorkflowPlanRunInput, 0, len(inputs))
	for _, input := range inputs {
		key := endpointKey(Endpoint{Node: input.FromNode, Port: input.FromPort})
		content, ok := messages[key]
		if !ok {
			if producedMessages[key] {
				return runInputs, fmt.Errorf("missing upstream message %s.%s", input.FromNode, input.FromPort)
			}
			content = fallback
		}
		runInputs = append(runInputs, WorkflowPlanRunInput{
			FromNode:   input.FromNode,
			FromPort:   input.FromPort,
			TargetPort: input.TargetPort,
			Content:    content,
		})
	}
	return runInputs, nil
}

func workflowProducedMessageEndpoints(plan WorkflowCompiledPlan) map[string]bool {
	produced := map[string]bool{}
	for _, run := range plan.AgentRuns {
		for _, output := range run.Outputs {
			produced[endpointKey(Endpoint{Node: run.NodeID, Port: output.Port})] = true
		}
	}
	for _, transform := range plan.Transforms {
		for _, output := range transform.Outputs {
			produced[endpointKey(Endpoint{Node: transform.NodeID, Port: output.Port})] = true
		}
	}
	return produced
}

func workflowExecutableNodes(plan WorkflowCompiledPlan) []workflowExecutableNode {
	agents := map[string]workflowExecutableNode{}
	for index, run := range plan.AgentRuns {
		agents[run.NodeID] = workflowExecutableNode{
			Kind:       workflowExecutableAgent,
			NodeID:     run.NodeID,
			AgentIndex: index,
			Agent:      run,
		}
	}
	transforms := map[string]workflowExecutableNode{}
	for _, transform := range plan.Transforms {
		transforms[transform.NodeID] = workflowExecutableNode{
			Kind:       workflowExecutableTransform,
			NodeID:     transform.NodeID,
			AgentIndex: -1,
			Transform:  transform,
		}
	}
	var executables []workflowExecutableNode
	if len(plan.Order) == 0 {
		for _, executable := range transforms {
			executable.Order = len(executables)
			executables = append(executables, executable)
		}
		sort.SliceStable(executables, func(i, j int) bool {
			return executables[i].NodeID < executables[j].NodeID
		})
		for index := range executables {
			executables[index].Order = index
		}
		for _, run := range plan.AgentRuns {
			executable := agents[run.NodeID]
			executable.Order = len(executables)
			executables = append(executables, executable)
		}
		return executables
	}
	for _, nodeID := range plan.Order {
		if executable, ok := transforms[nodeID]; ok {
			executable.Order = len(executables)
			executables = append(executables, executable)
			continue
		}
		if executable, ok := agents[nodeID]; ok {
			executable.Order = len(executables)
			executables = append(executables, executable)
		}
	}
	return executables
}

func workflowProducerNodeIDs(executables []workflowExecutableNode) map[string]bool {
	ids := map[string]bool{}
	for _, executable := range executables {
		ids[executable.NodeID] = true
	}
	return ids
}

func workflowExecutableInputs(executable workflowExecutableNode) []WorkflowCompiledInput {
	if executable.Kind == workflowExecutableTransform {
		return executable.Transform.Inputs
	}
	return executable.Agent.Inputs
}

func workflowInitialMessages(plan WorkflowCompiledPlan) map[string]string {
	messages := map[string]string{}
	for _, trigger := range plan.Triggers {
		if trigger.Type != WorkflowNodeTypeTimer || strings.TrimSpace(trigger.Content) == "" {
			continue
		}
		messages[endpointKey(Endpoint{Node: trigger.NodeID, Port: trigger.Port})] = trigger.Content
	}
	return messages
}

func workflowTransformContent(transform WorkflowCompiledTransform, inputs []WorkflowPlanRunInput) string {
	simulationInputs := workflowSimulationInputsFromRun(inputs)
	switch transform.Type {
	case WorkflowNodeTypeTemplate:
		template := stringNodeConfig(transform.Config, "template")
		if strings.TrimSpace(template) == "" {
			template = "{{inputs}}"
		}
		return renderWorkflowTemplate(template, simulationInputs)
	case WorkflowNodeTypeSwitch:
		return workflowSwitchContent(WorkflowNode{Type: WorkflowNodeTypeSwitch, Config: transform.Config}, simulationInputs)
	default:
		var parts []string
		for _, input := range inputs {
			if strings.TrimSpace(input.Content) != "" {
				parts = append(parts, input.Content)
			}
		}
		return strings.Join(parts, "\n\n")
	}
}

func workflowSimulationInputsFromRun(inputs []WorkflowPlanRunInput) []WorkflowSimulationInput {
	result := make([]WorkflowSimulationInput, 0, len(inputs))
	for _, input := range inputs {
		result = append(result, WorkflowSimulationInput{
			FromNode:   input.FromNode,
			FromPort:   input.FromPort,
			TargetPort: input.TargetPort,
			Content:    input.Content,
		})
	}
	return result
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
