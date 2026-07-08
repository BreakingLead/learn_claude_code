package agent

import (
	nodeeditor "bee_agent/internal/agent/node_editor"
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

func newBeeAgentPlanExecutorFactory(config agentConfig, client anthropic.Client) nodeeditor.PlanExecutorFactory {
	return func(request nodeeditor.WorkflowPlanRunRequest) (nodeeditor.PlanExecutor, string, error) {
		mode := strings.TrimSpace(request.ExecutionMode)
		if mode == "" {
			mode = nodeeditor.WorkflowExecutionModeDryRun
		}
		if mode != nodeeditor.WorkflowExecutionModeBeeAgent {
			return nodeeditor.NewPlanExecutor(request)
		}
		timeout, timeoutMS := nodeeditor.WorkflowRunTimeout(request)
		return nodeeditor.PlanExecutor{
			Invoker:   beeAgentWorkflowInvoker{config: config, client: client},
			Timeout:   timeout,
			TimeoutMS: timeoutMS,
		}, mode, nil
	}
}

type beeAgentWorkflowInvoker struct {
	config agentConfig
	client anthropic.Client
}

func (i beeAgentWorkflowInvoker) InvokeAgent(ctx context.Context, invocation nodeeditor.AgentInvocation) (nodeeditor.AgentInvocationResult, error) {
	blueprintID := safeFileID(invocation.Run.BlueprintID)
	if blueprintID == "" {
		return nodeeditor.AgentInvocationResult{}, fmt.Errorf("workflow agent %q has empty blueprint_id", invocation.Run.NodeID)
	}

	config := i.config
	config.UseBlueprint = true
	config.BlueprintPath = filepath.Join(config.Workdir, ".agents", "blueprints", "agents", blueprintID+".json")
	rt := newAgentRuntime(config, nil, nil)
	if rt.blueprint == nil || rt.blueprint.Error != "" {
		if rt.blueprint != nil && rt.blueprint.Error != "" {
			return nodeeditor.AgentInvocationResult{}, fmt.Errorf("load blueprint %q: %s", blueprintID, rt.blueprint.Error)
		}
		return nodeeditor.AgentInvocationResult{}, fmt.Errorf("load blueprint %q", blueprintID)
	}

	systemPrompt := rt.getSystemPrompt(nil)
	if instruction := strings.TrimSpace(invocation.Run.Instruction); instruction != "" {
		systemPrompt += "\n\nWorkflow instruction:\n" + instruction
	}
	systemPrompt += "\n\nWorkflow execution mode: minimal bee_agent. Produce one final message for downstream workflow nodes. Tools are not available in this mode."

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(config.Model),
		MaxTokens: config.DefaultTokens,
		System:    []anthropic.TextBlockParam{{Text: systemPrompt}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(workflowInvocationUserText(invocation))),
		},
	}
	if params.MaxTokens <= 0 {
		params.MaxTokens = 8000
	}
	response, err := i.client.Messages.New(ctx, params)
	if err != nil {
		return nodeeditor.AgentInvocationResult{}, err
	}
	content := strings.TrimSpace(anthropicMessageText(response))
	if content == "" {
		return nodeeditor.AgentInvocationResult{}, fmt.Errorf("model returned empty workflow output")
	}
	return nodeeditor.AgentInvocationResult{
		Content: content,
		Diagnostics: []string{
			fmt.Sprintf("bee_agent invoked blueprint %q for workflow node %q without tools", blueprintID, invocation.Run.NodeID),
		},
	}, nil
}

func workflowInvocationUserText(invocation nodeeditor.AgentInvocation) string {
	var b strings.Builder
	label := strings.TrimSpace(invocation.Run.Label)
	if label == "" {
		label = invocation.Run.NodeID
	}
	if label != "" {
		fmt.Fprintf(&b, "Workflow node: %s", label)
		if invocation.Run.NodeID != "" && invocation.Run.NodeID != label {
			fmt.Fprintf(&b, " (%s)", invocation.Run.NodeID)
		}
		b.WriteString("\n\n")
	}
	if len(invocation.Inputs) == 0 {
		b.WriteString("Workflow input: (none)")
		return b.String()
	}
	b.WriteString("Workflow inputs:")
	for _, input := range invocation.Inputs {
		fmt.Fprintf(&b, "\n- %s.%s -> %s:\n%s", input.FromNode, input.FromPort, input.TargetPort, strings.TrimSpace(input.Content))
	}
	return b.String()
}

func anthropicMessageText(message *anthropic.Message) string {
	if message == nil {
		return ""
	}
	var parts []string
	for _, block := range message.Content {
		if text, ok := block.AsAny().(anthropic.TextBlock); ok {
			parts = append(parts, text.Text)
		}
	}
	return strings.Join(parts, "\n")
}
