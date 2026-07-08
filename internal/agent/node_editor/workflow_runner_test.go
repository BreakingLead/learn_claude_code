package nodeeditor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type recordingInvoker struct {
	mu          sync.Mutex
	calls       []string
	invocations []AgentInvocation
}

func (i *recordingInvoker) InvokeAgent(ctx context.Context, invocation AgentInvocation) (AgentInvocationResult, error) {
	if err := ctx.Err(); err != nil {
		return AgentInvocationResult{}, err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	i.calls = append(i.calls, invocation.Run.NodeID)
	i.invocations = append(i.invocations, invocation)
	return AgentInvocationResult{
		Content: fmt.Sprintf("agent=%s inputs=%d", invocation.Run.NodeID, len(invocation.Inputs)),
	}, nil
}

func TestRunCompiledWorkflowPlanPropagatesMessages(t *testing.T) {
	plan := WorkflowCompiledPlan{
		WorkflowID: "review-pipeline",
		Name:       "Review Pipeline",
		AgentRuns: []WorkflowCompiledRun{
			{
				NodeID:      "developer",
				Label:       "Developer",
				BlueprintID: "default",
				Instruction: "Implement the change.",
				Inputs:      []WorkflowCompiledInput{{FromNode: "prompt", FromPort: "message", TargetPort: "input"}},
				Outputs:     []WorkflowCompiledRunOutput{{Port: "output", To: []Endpoint{{Node: "summary", Port: "input"}}}},
				ToolNames:   []string{"read_file", "write_file"},
			},
			{
				NodeID:      "reviewer",
				Label:       "Reviewer",
				BlueprintID: "default",
				Instruction: "Review the change.",
				Inputs:      []WorkflowCompiledInput{{FromNode: "developer", FromPort: "output", TargetPort: "input"}},
				Outputs:     []WorkflowCompiledRunOutput{{Port: "output", To: []Endpoint{{Node: "summary", Port: "input"}}}},
			},
			{
				NodeID:      "summary",
				Label:       "Summary",
				BlueprintID: "default",
				Instruction: "Summarize both branches.",
				Inputs: []WorkflowCompiledInput{
					{FromNode: "developer", FromPort: "output", TargetPort: "input"},
					{FromNode: "reviewer", FromPort: "output", TargetPort: "input"},
				},
				Outputs: []WorkflowCompiledRunOutput{{Port: "output", To: []Endpoint{{Node: "output", Port: "message"}}}},
			},
		},
		Outputs: []WorkflowCompiledOutput{{
			NodeID: "output",
			Label:  "Output",
			Inputs: []WorkflowCompiledInput{{FromNode: "summary", FromPort: "output", TargetPort: "message"}},
		}},
	}

	run := RunCompiledWorkflowPlan(plan, "build the API")
	if len(run.Steps) != 3 {
		t.Fatalf("unexpected run steps: %+v", run.Steps)
	}
	if !strings.Contains(run.Steps[0].Outputs[0].Content, "build the API") {
		t.Fatalf("expected initial input in first agent output: %+v", run.Steps[0])
	}
	if len(run.Steps[1].Inputs) != 1 || !strings.Contains(run.Steps[1].Inputs[0].Content, "Developer") {
		t.Fatalf("expected reviewer to receive developer output: %+v", run.Steps[1])
	}
	summary := run.Steps[2]
	if len(summary.Inputs) != 2 {
		t.Fatalf("expected summary to receive both upstream outputs: %+v", summary)
	}
	if !strings.Contains(summary.Inputs[0].Content, "Developer") || !strings.Contains(summary.Inputs[1].Content, "Reviewer") {
		t.Fatalf("expected upstream dry-run content in summary inputs: %+v", summary.Inputs)
	}
	if len(run.Outputs) != 1 || !strings.Contains(run.Outputs[0].Content, "Summary") {
		t.Fatalf("unexpected final output: %+v", run.Outputs)
	}
}

func TestRunCompiledWorkflowPlanUsesTimerTriggerContent(t *testing.T) {
	workdir := t.TempDir()
	if _, err := EnsureDefaultBlueprint(DefaultBlueprintPath(workdir)); err != nil {
		t.Fatal(err)
	}
	store := NewStore(workdir)
	plan, err := store.CompileWorkflow(timerWorkflow())
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Triggers) != 1 || plan.Triggers[0].NodeID != "timer" || plan.Triggers[0].Cron != "0 9 * * *" {
		t.Fatalf("expected timer trigger in compiled plan: %+v", plan.Triggers)
	}

	run, err := NewDryRunPlanExecutor().Execute(context.Background(), plan, "manual input")
	if err != nil {
		t.Fatal(err)
	}
	if len(run.Steps) != 1 || len(run.Steps[0].Inputs) != 1 {
		t.Fatalf("unexpected timer run steps: %+v", run.Steps)
	}
	if run.Steps[0].Inputs[0].Content != "Check CI status." {
		t.Fatalf("expected timer content to feed agent, got %+v", run.Steps[0].Inputs)
	}
}

func TestRunCompiledWorkflowPlanExecutesTemplateTransform(t *testing.T) {
	workdir := t.TempDir()
	if _, err := EnsureDefaultBlueprint(DefaultBlueprintPath(workdir)); err != nil {
		t.Fatal(err)
	}
	store := NewStore(workdir)
	plan, err := store.CompileWorkflow(templateWorkflow())
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Transforms) != 1 || plan.Transforms[0].NodeID != "template" {
		t.Fatalf("expected template transform in compiled plan: %+v", plan.Transforms)
	}
	invoker := &recordingInvoker{}
	run, err := (PlanExecutor{Invoker: invoker}).Execute(context.Background(), plan, "build the API")
	if err != nil {
		t.Fatal(err)
	}
	if len(invoker.invocations) != 1 || len(invoker.invocations[0].Inputs) != 1 {
		t.Fatalf("unexpected invocations: %+v", invoker.invocations)
	}
	if got := invoker.invocations[0].Inputs[0].Content; !strings.Contains(got, "Scheduled request:\nbuild the API") {
		t.Fatalf("expected templated content to feed agent, got %q", got)
	}
	if len(run.Outputs) != 1 || !strings.Contains(run.Outputs[0].Content, "agent=agent") {
		t.Fatalf("unexpected final output: %+v", run.Outputs)
	}
}

func TestRunCompiledWorkflowPlanExecutesSwitchTransformWithoutAgent(t *testing.T) {
	workdir := t.TempDir()
	if _, err := EnsureDefaultBlueprint(DefaultBlueprintPath(workdir)); err != nil {
		t.Fatal(err)
	}
	store := NewStore(workdir)
	plan, err := store.CompileWorkflow(switchWorkflow())
	if err != nil {
		t.Fatal(err)
	}
	run, err := NewDryRunPlanExecutor().Execute(context.Background(), plan, "manual")
	if err != nil {
		t.Fatal(err)
	}
	if len(run.Steps) != 0 {
		t.Fatalf("switch-only workflow should not call agents: %+v", run.Steps)
	}
	if len(run.Outputs) != 1 || run.Outputs[0].Content != "Run the daytime prompt." {
		t.Fatalf("unexpected switch output: %+v", run.Outputs)
	}
}

func TestPlanExecutorSchedulesDependenciesIndependentOfPlanOrder(t *testing.T) {
	executor := PlanExecutor{Invoker: &recordingInvoker{}}
	plan := WorkflowCompiledPlan{
		WorkflowID: "review-pipeline",
		Name:       "Review Pipeline",
		AgentRuns: []WorkflowCompiledRun{
			{
				NodeID:  "reviewer",
				Inputs:  []WorkflowCompiledInput{{FromNode: "developer", FromPort: "output", TargetPort: "input"}},
				Outputs: []WorkflowCompiledRunOutput{{Port: "output"}},
			},
			{
				NodeID:  "developer",
				Inputs:  []WorkflowCompiledInput{{FromNode: "prompt", FromPort: "message", TargetPort: "input"}},
				Outputs: []WorkflowCompiledRunOutput{{Port: "output"}},
			},
		},
	}

	run, err := executor.Execute(context.Background(), plan, "build the API")
	if err != nil {
		t.Fatal(err)
	}
	reviewer := workflowRunStepByNodeID(run.Steps, "reviewer")
	if len(reviewer.Inputs) != 1 || !strings.Contains(reviewer.Inputs[0].Content, "agent=developer") {
		t.Fatalf("expected reviewer to receive developer output despite plan order: %+v", reviewer)
	}
}

func TestPlanExecutorUsesInvokerAndPropagatesResults(t *testing.T) {
	invoker := &recordingInvoker{}
	executor := PlanExecutor{Invoker: invoker}
	plan := WorkflowCompiledPlan{
		WorkflowID: "custom",
		Name:       "Custom",
		AgentRuns: []WorkflowCompiledRun{
			{
				NodeID:    "first",
				Inputs:    []WorkflowCompiledInput{{FromNode: "prompt", FromPort: "message", TargetPort: "input"}},
				Outputs:   []WorkflowCompiledRunOutput{{Port: "output", To: []Endpoint{{Node: "second", Port: "input"}}}},
				ToolNames: []string{"read_file"},
			},
			{
				NodeID:  "second",
				Inputs:  []WorkflowCompiledInput{{FromNode: "first", FromPort: "output", TargetPort: "input"}},
				Outputs: []WorkflowCompiledRunOutput{{Port: "output", To: []Endpoint{{Node: "out", Port: "message"}}}},
			},
		},
		Outputs: []WorkflowCompiledOutput{{
			NodeID: "out",
			Inputs: []WorkflowCompiledInput{{
				FromNode: "second", FromPort: "output", TargetPort: "message",
			}},
		}},
	}

	run, err := executor.Execute(context.Background(), plan, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(invoker.calls, ",") != "first,second" {
		t.Fatalf("unexpected invoker order: %+v", invoker.calls)
	}
	if len(run.Steps) != 2 || run.Steps[1].Inputs[0].Content != "agent=first inputs=1" {
		t.Fatalf("expected executor to propagate invoker output: %+v", run.Steps)
	}
	if run.Steps[0].Status != WorkflowRunStatusCompleted || run.Steps[1].Status != WorkflowRunStatusCompleted {
		t.Fatalf("expected completed step statuses: %+v", run.Steps)
	}
	if len(run.Outputs) != 1 || run.Outputs[0].Content != "agent=second inputs=1" {
		t.Fatalf("unexpected final output: %+v", run.Outputs)
	}
}

type concurrentProbeInvoker struct {
	mu        sync.Mutex
	active    int
	calls     []string
	bothReady chan struct{}
	once      sync.Once
}

func newConcurrentProbeInvoker() *concurrentProbeInvoker {
	return &concurrentProbeInvoker{bothReady: make(chan struct{})}
}

func (i *concurrentProbeInvoker) InvokeAgent(ctx context.Context, invocation AgentInvocation) (AgentInvocationResult, error) {
	i.mu.Lock()
	i.active++
	i.calls = append(i.calls, invocation.Run.NodeID)
	if i.active == 2 {
		i.once.Do(func() { close(i.bothReady) })
	}
	i.mu.Unlock()

	select {
	case <-i.bothReady:
	case <-ctx.Done():
		return AgentInvocationResult{}, ctx.Err()
	}
	return AgentInvocationResult{Content: "done:" + invocation.Run.NodeID}, nil
}

func TestPlanExecutorRunsIndependentAgentsConcurrently(t *testing.T) {
	invoker := newConcurrentProbeInvoker()
	executor := PlanExecutor{Invoker: invoker, Timeout: 200 * time.Millisecond, TimeoutMS: 200}
	plan := WorkflowCompiledPlan{
		WorkflowID: "parallel",
		Name:       "Parallel",
		AgentRuns: []WorkflowCompiledRun{
			{
				NodeID:  "left",
				Inputs:  []WorkflowCompiledInput{{FromNode: "prompt", FromPort: "message", TargetPort: "input"}},
				Outputs: []WorkflowCompiledRunOutput{{Port: "output", To: []Endpoint{{Node: "summary", Port: "input"}}}},
			},
			{
				NodeID:  "right",
				Inputs:  []WorkflowCompiledInput{{FromNode: "prompt", FromPort: "message", TargetPort: "input"}},
				Outputs: []WorkflowCompiledRunOutput{{Port: "output", To: []Endpoint{{Node: "summary", Port: "input"}}}},
			},
			{
				NodeID: "summary",
				Inputs: []WorkflowCompiledInput{
					{FromNode: "left", FromPort: "output", TargetPort: "input"},
					{FromNode: "right", FromPort: "output", TargetPort: "input"},
				},
				Outputs: []WorkflowCompiledRunOutput{{Port: "output", To: []Endpoint{{Node: "out", Port: "message"}}}},
			},
		},
		Outputs: []WorkflowCompiledOutput{{
			NodeID: "out",
			Inputs: []WorkflowCompiledInput{{FromNode: "summary", FromPort: "output", TargetPort: "message"}},
		}},
	}

	run, err := executor.Execute(context.Background(), plan, "fan out")
	if err != nil {
		t.Fatal(err)
	}
	if len(run.Steps) != 3 || run.Steps[0].NodeID != "left" || run.Steps[1].NodeID != "right" || run.Steps[2].NodeID != "summary" {
		t.Fatalf("expected stable compiled step order: %+v", run.Steps)
	}
	summary := workflowRunStepByNodeID(run.Steps, "summary")
	if len(summary.Inputs) != 2 || !strings.Contains(summary.Inputs[0].Content, "done:left") || !strings.Contains(summary.Inputs[1].Content, "done:right") {
		t.Fatalf("expected summary to receive both parallel outputs: %+v", summary)
	}
}

func TestExternalCommandAgentInvokerUsesJSONBoundary(t *testing.T) {
	dir := t.TempDir()
	capturePath := filepath.Join(dir, "invocation.json")
	scriptPath := filepath.Join(dir, "invoker.sh")
	script := "#!/bin/sh\ncat > \"$1\"\nprintf '{\"content\":\"external ok\",\"diagnostics\":[\"from command\"]}\\n'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	invoker := ExternalCommandAgentInvoker{Command: []string{scriptPath, capturePath}}

	result, err := invoker.InvokeAgent(context.Background(), AgentInvocation{
		Run: WorkflowCompiledRun{NodeID: "agent", BlueprintID: "default"},
		Inputs: []WorkflowPlanRunInput{{
			FromNode: "prompt", FromPort: "message", TargetPort: "input", Content: "hello",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "external ok" || strings.Join(result.Diagnostics, ",") != "from command" {
		t.Fatalf("unexpected command result: %+v", result)
	}
	raw, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"node_id":"agent"`) || !strings.Contains(string(raw), `"content":"hello"`) {
		t.Fatalf("external command did not receive invocation JSON: %s", string(raw))
	}
}

func TestPlanExecutorTimesOutExternalCommand(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "slow-invoker.sh")
	script := "#!/bin/sh\nsleep 1\nprintf '{\"content\":\"late\"}\\n'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	executor, mode, err := NewPlanExecutor(WorkflowPlanRunRequest{
		ExecutionMode:   "external_command",
		ExternalCommand: []string{scriptPath},
		TimeoutMS:       20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if mode != "external_command" || executor.TimeoutMS != 20 || executor.Timeout != 20*time.Millisecond {
		t.Fatalf("unexpected executor config: mode=%s executor=%+v", mode, executor)
	}

	run, err := executor.Execute(context.Background(), WorkflowCompiledPlan{
		WorkflowID: "timeout",
		AgentRuns: []WorkflowCompiledRun{{
			NodeID:  "agent",
			Inputs:  []WorkflowCompiledInput{{FromNode: "prompt", FromPort: "message", TargetPort: "input"}},
			Outputs: []WorkflowCompiledRunOutput{{Port: "output"}},
		}},
	}, "input")
	if err == nil || !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
		t.Fatalf("expected timeout error, got %v", err)
	}
	if run.Status != WorkflowRunStatusFailed || !strings.Contains(run.Error, context.DeadlineExceeded.Error()) || run.StartedAt == "" || run.FinishedAt == "" {
		t.Fatalf("expected failed run on timeout: %+v", run)
	}
	if len(run.Steps) != 1 {
		t.Fatalf("expected failed step to be recorded: %+v", run.Steps)
	}
	step := run.Steps[0]
	if step.Status != WorkflowRunStatusFailed || !strings.Contains(step.Error, context.DeadlineExceeded.Error()) {
		t.Fatalf("expected failed step error on timeout: %+v", step)
	}
	if step.StartedAt == "" || step.FinishedAt == "" || step.DurationMS < 0 {
		t.Fatalf("expected failed step timing: %+v", step)
	}
	if len(step.Inputs) != 1 || step.Inputs[0].Content != "input" {
		t.Fatalf("expected failed step inputs to be preserved: %+v", step)
	}
}

func TestStoreRunWorkflowPlanUsesInjectedExecutorFactory(t *testing.T) {
	workdir := t.TempDir()
	if _, err := EnsureDefaultBlueprint(DefaultBlueprintPath(workdir)); err != nil {
		t.Fatal(err)
	}
	store := NewStore(workdir)
	workflow := DefaultWorkflow()
	if err := store.WriteWorkflow("review-pipeline", workflow); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.SaveCompiledWorkflowPlan(workflow); err != nil {
		t.Fatal(err)
	}
	invoker := &recordingInvoker{}
	store.SetPlanExecutorFactory(func(request WorkflowPlanRunRequest) (PlanExecutor, string, error) {
		if request.ExecutionMode != WorkflowExecutionModeBeeAgent {
			return NewPlanExecutor(request)
		}
		timeout, timeoutMS := WorkflowRunTimeout(request)
		return PlanExecutor{Invoker: invoker, Timeout: timeout, TimeoutMS: timeoutMS}, request.ExecutionMode, nil
	})

	run, err := store.RunWorkflowPlan(context.Background(), "review-pipeline", WorkflowPlanRunRequest{
		Input:         "build the API",
		ExecutionMode: WorkflowExecutionModeBeeAgent,
		TimeoutMS:     50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if run.ExecutionMode != WorkflowExecutionModeBeeAgent || run.TimeoutMS != 50 {
		t.Fatalf("unexpected run metadata: %+v", run)
	}
	if len(invoker.calls) != 3 || strings.Join(invoker.calls, ",") != "developer,reviewer,summary" {
		t.Fatalf("expected injected invoker calls in workflow order, got %+v", invoker.calls)
	}
	if len(run.Outputs) != 1 || !strings.Contains(run.Outputs[0].Content, "agent=summary") {
		t.Fatalf("unexpected injected run output: %+v", run.Outputs)
	}
}

func TestStoreRunDefaultTimerWorkflowWithBeeAgentMode(t *testing.T) {
	workdir := t.TempDir()
	if _, err := EnsureDefaultBlueprint(DefaultBlueprintPath(workdir)); err != nil {
		t.Fatal(err)
	}
	store := NewStore(workdir)
	workflow := DefaultTimerWorkflow()
	if err := store.WriteWorkflow("timer-ci-check", workflow); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.SaveCompiledWorkflowPlan(workflow); err != nil {
		t.Fatal(err)
	}
	invoker := &recordingInvoker{}
	store.SetPlanExecutorFactory(func(request WorkflowPlanRunRequest) (PlanExecutor, string, error) {
		if request.ExecutionMode != WorkflowExecutionModeBeeAgent {
			return NewPlanExecutor(request)
		}
		timeout, timeoutMS := WorkflowRunTimeout(request)
		return PlanExecutor{Invoker: invoker, Timeout: timeout, TimeoutMS: timeoutMS}, request.ExecutionMode, nil
	})

	run, err := store.RunWorkflowPlan(context.Background(), "timer-ci-check", WorkflowPlanRunRequest{
		Input:         "manual input should be ignored for timer source",
		ExecutionMode: WorkflowExecutionModeBeeAgent,
		TimeoutMS:     50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if run.ExecutionMode != WorkflowExecutionModeBeeAgent || len(run.Steps) != 1 {
		t.Fatalf("unexpected timer bee_agent run: %+v", run)
	}
	if len(invoker.invocations) != 1 {
		t.Fatalf("expected one model invocation, got %+v", invoker.invocations)
	}
	inputs := invoker.invocations[0].Inputs
	if len(inputs) != 1 || inputs[0].FromNode != "timer" || !strings.Contains(inputs[0].Content, "Check CI status") {
		t.Fatalf("expected timer prompt to feed bee_agent invocation: %+v", inputs)
	}
	if len(run.Outputs) != 1 || !strings.Contains(run.Outputs[0].Content, "agent=ci-agent") {
		t.Fatalf("unexpected timer workflow output: %+v", run.Outputs)
	}
}

func TestExampleWorkflowDryInvokerScript(t *testing.T) {
	scriptPath := filepath.Join("..", "..", "..", "scripts", "workflow-dry-invoker")
	if _, err := os.Stat(scriptPath); err != nil {
		t.Fatal(err)
	}
	invoker := ExternalCommandAgentInvoker{Command: []string{scriptPath}}

	result, err := invoker.InvokeAgent(context.Background(), AgentInvocation{
		Run: WorkflowCompiledRun{
			NodeID:      "developer",
			Label:       "Developer Agent",
			BlueprintID: "default",
			Instruction: "Implement the change.",
			ToolNames:   []string{"read_file"},
		},
		Inputs: []WorkflowPlanRunInput{{
			FromNode: "prompt", FromPort: "message", TargetPort: "input", Content: "build the API",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "External dry invoker Developer Agent") {
		t.Fatalf("unexpected script content: %+v", result)
	}
	if !strings.Contains(result.Content, "build the API") {
		t.Fatalf("expected script to include invocation input: %+v", result)
	}
}

func workflowRunStepByNodeID(steps []WorkflowPlanRunStep, nodeID string) WorkflowPlanRunStep {
	for _, step := range steps {
		if step.NodeID == nodeID {
			return step
		}
	}
	return WorkflowPlanRunStep{}
}
