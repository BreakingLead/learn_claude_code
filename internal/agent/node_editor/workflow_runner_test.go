package nodeeditor

import (
	"strings"
	"testing"
)

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
