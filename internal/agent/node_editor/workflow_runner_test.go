package nodeeditor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type recordingInvoker struct {
	calls []string
}

func (i *recordingInvoker) InvokeAgent(ctx context.Context, invocation AgentInvocation) (AgentInvocationResult, error) {
	if err := ctx.Err(); err != nil {
		return AgentInvocationResult{}, err
	}
	i.calls = append(i.calls, invocation.Run.NodeID)
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
				Inputs:      []WorkflowCompiledInput{{FromNode: "prompt", FromPort: "message", TargetPort: "input"}},
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
	if len(run.Outputs) != 1 || run.Outputs[0].Content != "agent=second inputs=1" {
		t.Fatalf("unexpected final output: %+v", run.Outputs)
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
