package nodeeditor

import (
	"fmt"
	"strings"
)

// WorkflowRunMarkdownReport renders a saved workflow run as a portable evidence report.
func WorkflowRunMarkdownReport(run WorkflowPlanRun) string {
	var b strings.Builder
	title := strings.TrimSpace(run.Name)
	if title == "" {
		title = run.WorkflowID
	}
	fmt.Fprintf(&b, "# Workflow Run Report\n\n")
	fmt.Fprintf(&b, "- Workflow: %s\n", markdownInline(title))
	fmt.Fprintf(&b, "- Run ID: %s\n", markdownInline(run.ID))
	fmt.Fprintf(&b, "- Status: %s\n", markdownInline(defaultString(run.Status, WorkflowRunStatusCompleted)))
	if run.RerunOf != "" {
		fmt.Fprintf(&b, "- Rerun of: %s\n", markdownInline(run.RerunOf))
	}
	fmt.Fprintf(&b, "- Mode: %s\n", markdownInline(defaultString(run.ExecutionMode, "dry_run")))
	if len(run.ExternalCommand) > 0 {
		fmt.Fprintf(&b, "- External command: %s\n", markdownInline(strings.Join(run.ExternalCommand, " ")))
	}
	if run.TimeoutMS > 0 {
		fmt.Fprintf(&b, "- Timeout: %dms\n", run.TimeoutMS)
	}
	if run.StartedAt != "" {
		fmt.Fprintf(&b, "- Started: %s\n", markdownInline(run.StartedAt))
	}
	if run.FinishedAt != "" {
		fmt.Fprintf(&b, "- Finished: %s\n", markdownInline(run.FinishedAt))
	}
	if run.DurationMS > 0 {
		fmt.Fprintf(&b, "- Duration: %dms\n", run.DurationMS)
	}
	if run.SourceHash != "" {
		fmt.Fprintf(&b, "- Source hash: %s\n", markdownInline(run.SourceHash))
	}
	if run.CurrentHash != "" {
		fmt.Fprintf(&b, "- Current hash: %s\n", markdownInline(run.CurrentHash))
	}
	if run.Stale {
		b.WriteString("- Stale: true\n")
	}
	if run.Error != "" {
		fmt.Fprintf(&b, "- Error: %s\n", markdownInline(run.Error))
	}

	b.WriteString("\n## Input\n\n")
	writeMarkdownBlock(&b, run.Input)

	if len(run.Diagnostics) > 0 {
		b.WriteString("\n## Diagnostics\n\n")
		for _, diagnostic := range run.Diagnostics {
			fmt.Fprintf(&b, "- %s\n", markdownInline(diagnostic))
		}
	}

	if len(run.Steps) > 0 {
		b.WriteString("\n## Steps\n")
		for index, step := range run.Steps {
			label := strings.TrimSpace(step.Label)
			if label == "" {
				label = step.NodeID
			}
			fmt.Fprintf(&b, "\n### %d. %s\n\n", index+1, markdownInline(label))
			fmt.Fprintf(&b, "- Node: %s\n", markdownInline(step.NodeID))
			if step.BlueprintID != "" {
				fmt.Fprintf(&b, "- Blueprint: %s\n", markdownInline(step.BlueprintID))
			}
			fmt.Fprintf(&b, "- Status: %s\n", markdownInline(defaultString(step.Status, WorkflowRunStatusCompleted)))
			if step.DurationMS > 0 {
				fmt.Fprintf(&b, "- Duration: %dms\n", step.DurationMS)
			}
			if step.Error != "" {
				fmt.Fprintf(&b, "- Error: %s\n", markdownInline(step.Error))
			}
			if step.Instruction != "" {
				b.WriteString("\nInstruction:\n\n")
				writeMarkdownBlock(&b, step.Instruction)
			}
			if len(step.Inputs) > 0 {
				b.WriteString("\nInputs:\n")
				for _, input := range step.Inputs {
					fmt.Fprintf(&b, "\n- `%s.%s -> %s`\n\n", input.FromNode, input.FromPort, input.TargetPort)
					writeMarkdownBlock(&b, input.Content)
				}
			}
			if len(step.Outputs) > 0 {
				b.WriteString("\nOutputs:\n")
				for _, output := range step.Outputs {
					fmt.Fprintf(&b, "\n- `%s`\n\n", output.Port)
					writeMarkdownBlock(&b, output.Content)
				}
			}
		}
	}

	if len(run.Outputs) > 0 {
		b.WriteString("\n## Final Outputs\n")
		for _, output := range run.Outputs {
			label := strings.TrimSpace(output.Label)
			if label == "" {
				label = output.NodeID
			}
			fmt.Fprintf(&b, "\n### %s\n\n", markdownInline(label))
			writeMarkdownBlock(&b, output.Content)
		}
	}
	return b.String()
}

func writeMarkdownBlock(b *strings.Builder, value string) {
	if strings.TrimSpace(value) == "" {
		value = "(empty)"
	}
	b.WriteString("````text\n")
	b.WriteString(value)
	if !strings.HasSuffix(value, "\n") {
		b.WriteByte('\n')
	}
	b.WriteString("````\n")
}

func markdownInline(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "(empty)"
	}
	return strings.ReplaceAll(value, "\n", " ")
}

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
