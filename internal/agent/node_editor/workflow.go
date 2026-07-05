package nodeeditor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	WorkflowNodeTypeInput  = "workflow_input"
	WorkflowNodeTypeAgent  = "workflow_agent"
	WorkflowNodeTypeOutput = "workflow_output"
)

type WorkflowDefinition struct {
	Version  int             `json:"version"`
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Nodes    []WorkflowNode  `json:"nodes"`
	Edges    []Edge          `json:"edges"`
	Metadata map[string]any  `json:"metadata,omitempty"`
	Config   json.RawMessage `json:"config,omitempty"`
}

type WorkflowNode struct {
	ID             string         `json:"id"`
	Type           string         `json:"type"`
	Label          string         `json:"label"`
	AgentBlueprint string         `json:"agent_blueprint,omitempty"`
	Position       Position       `json:"position"`
	Inputs         []Port         `json:"inputs,omitempty"`
	Outputs        []Port         `json:"outputs,omitempty"`
	Config         map[string]any `json:"config,omitempty"`
}

type WorkflowExecutionStep struct {
	NodeID         string   `json:"node_id"`
	Type           string   `json:"type"`
	Label          string   `json:"label"`
	AgentBlueprint string   `json:"agent_blueprint,omitempty"`
	InputsFrom     []string `json:"inputs_from,omitempty"`
	OutputsTo      []string `json:"outputs_to,omitempty"`
}

type WorkflowSimulationStep struct {
	NodeID         string                     `json:"node_id"`
	Type           string                     `json:"type"`
	Label          string                     `json:"label"`
	AgentBlueprint string                     `json:"agent_blueprint,omitempty"`
	Inputs         []WorkflowSimulationInput  `json:"inputs,omitempty"`
	Outputs        []WorkflowSimulationOutput `json:"outputs,omitempty"`
}

type WorkflowSimulationInput struct {
	FromNode   string `json:"from_node"`
	FromPort   string `json:"from_port"`
	TargetPort string `json:"target_port"`
	Content    string `json:"content"`
}

type WorkflowSimulationOutput struct {
	Port    string     `json:"port"`
	Content string     `json:"content"`
	To      []Endpoint `json:"to,omitempty"`
}

func DefaultWorkflow() WorkflowDefinition {
	return WorkflowDefinition{
		Version: SchemaVersion,
		ID:      "review-pipeline",
		Name:    "Review Pipeline",
		Nodes: []WorkflowNode{
			{
				ID:       "prompt",
				Type:     WorkflowNodeTypeInput,
				Label:    "Prompt",
				Position: Position{X: 80, Y: 180},
				Outputs:  []Port{{ID: "message", Type: PortTypeMessage, Label: "Message", Direction: DirectionOutput}},
			},
			{
				ID:             "developer",
				Type:           WorkflowNodeTypeAgent,
				Label:          "Developer Agent",
				AgentBlueprint: "default",
				Position:       Position{X: 340, Y: 80},
				Inputs:         []Port{{ID: "input", Type: PortTypeMessage, Label: "Input", Direction: DirectionInput}},
				Outputs:        []Port{{ID: "output", Type: PortTypeMessage, Label: "Output", Direction: DirectionOutput}},
			},
			{
				ID:             "reviewer",
				Type:           WorkflowNodeTypeAgent,
				Label:          "Reviewer Agent",
				AgentBlueprint: "default",
				Position:       Position{X: 340, Y: 280},
				Inputs:         []Port{{ID: "input", Type: PortTypeMessage, Label: "Input", Direction: DirectionInput}},
				Outputs:        []Port{{ID: "output", Type: PortTypeMessage, Label: "Output", Direction: DirectionOutput}},
			},
			{
				ID:             "summary",
				Type:           WorkflowNodeTypeAgent,
				Label:          "Summary Agent",
				AgentBlueprint: "default",
				Position:       Position{X: 620, Y: 180},
				Inputs:         []Port{{ID: "input", Type: PortTypeMessage, Label: "Inputs", Direction: DirectionInput, Multiple: true}},
				Outputs:        []Port{{ID: "output", Type: PortTypeMessage, Label: "Output", Direction: DirectionOutput}},
			},
			{
				ID:       "output",
				Type:     WorkflowNodeTypeOutput,
				Label:    "Output",
				Position: Position{X: 880, Y: 180},
				Inputs:   []Port{{ID: "message", Type: PortTypeMessage, Label: "Message", Direction: DirectionInput}},
			},
		},
		Edges: []Edge{
			{ID: "edge-prompt-developer", Source: Endpoint{Node: "prompt", Port: "message"}, Target: Endpoint{Node: "developer", Port: "input"}},
			{ID: "edge-prompt-reviewer", Source: Endpoint{Node: "prompt", Port: "message"}, Target: Endpoint{Node: "reviewer", Port: "input"}},
			{ID: "edge-developer-summary", Source: Endpoint{Node: "developer", Port: "output"}, Target: Endpoint{Node: "summary", Port: "input"}},
			{ID: "edge-reviewer-summary", Source: Endpoint{Node: "reviewer", Port: "output"}, Target: Endpoint{Node: "summary", Port: "input"}},
			{ID: "edge-summary-output", Source: Endpoint{Node: "summary", Port: "output"}, Target: Endpoint{Node: "output", Port: "message"}},
		},
		Metadata: map[string]any{
			"purpose": "default multi-agent workflow",
		},
	}
}

func DefaultWorkflowPath(workdir string) string {
	return filepath.Join(workdir, ".agents", "blueprints", "workflows", "review-pipeline.json")
}

func EnsureDefaultWorkflow(path string) (bool, error) {
	if strings.TrimSpace(path) == "" {
		return false, fmt.Errorf("default workflow path is required")
	}
	if _, err := os.Stat(path); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, err
	}
	workflow := DefaultWorkflow()
	if err := ValidateWorkflow(workflow); err != nil {
		return false, err
	}
	return true, WriteWorkflow(path, workflow)
}

func ReadWorkflow(path string) (WorkflowDefinition, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return WorkflowDefinition{}, err
	}
	var workflow WorkflowDefinition
	if err := json.Unmarshal(raw, &workflow); err != nil {
		return WorkflowDefinition{}, err
	}
	return workflow, nil
}

func WriteWorkflow(path string, workflow WorkflowDefinition) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(workflow, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(path, raw, 0o644)
}

func ValidateWorkflow(workflow WorkflowDefinition) error {
	if workflow.Version != SchemaVersion {
		return fmt.Errorf("unsupported workflow version %d", workflow.Version)
	}
	if strings.TrimSpace(workflow.ID) == "" {
		return fmt.Errorf("workflow id is required")
	}
	nodes := map[string]WorkflowNode{}
	outputCount := 0
	for _, node := range workflow.Nodes {
		if strings.TrimSpace(node.ID) == "" {
			return fmt.Errorf("workflow node id is required")
		}
		if _, exists := nodes[node.ID]; exists {
			return fmt.Errorf("duplicate workflow node id %q", node.ID)
		}
		if err := validateWorkflowNode(node); err != nil {
			return err
		}
		if node.Type == WorkflowNodeTypeOutput {
			outputCount++
		}
		nodes[node.ID] = node
	}
	if outputCount == 0 {
		return fmt.Errorf("workflow requires at least one output node")
	}
	incoming := map[string]int{}
	edgeIDs := map[string]bool{}
	for _, edge := range workflow.Edges {
		if strings.TrimSpace(edge.ID) == "" {
			return fmt.Errorf("workflow edge id is required")
		}
		if edgeIDs[edge.ID] {
			return fmt.Errorf("duplicate workflow edge id %q", edge.ID)
		}
		edgeIDs[edge.ID] = true
		sourcePort, err := findWorkflowPort(nodes, edge.Source, DirectionOutput)
		if err != nil {
			return fmt.Errorf("edge %q source: %w", edge.ID, err)
		}
		targetPort, err := findWorkflowPort(nodes, edge.Target, DirectionInput)
		if err != nil {
			return fmt.Errorf("edge %q target: %w", edge.ID, err)
		}
		if sourcePort.Type != targetPort.Type {
			return fmt.Errorf("edge %q connects %s to %s", edge.ID, sourcePort.Type, targetPort.Type)
		}
		key := edge.Target.Node + ":" + edge.Target.Port
		incoming[key]++
		if incoming[key] > 1 && !targetPort.Multiple {
			return fmt.Errorf("input port %s on workflow node %s allows only one edge", edge.Target.Port, edge.Target.Node)
		}
	}
	if err := validateWorkflowDAG(workflow, nodes); err != nil {
		return err
	}
	return nil
}

func WorkflowExecutionOrder(workflow WorkflowDefinition) ([]string, error) {
	if err := ValidateWorkflow(workflow); err != nil {
		return nil, err
	}
	nodes := map[string]bool{}
	indegree := map[string]int{}
	children := map[string][]string{}
	for _, node := range workflow.Nodes {
		nodes[node.ID] = true
		indegree[node.ID] = 0
	}
	for _, edge := range workflow.Edges {
		children[edge.Source.Node] = append(children[edge.Source.Node], edge.Target.Node)
		indegree[edge.Target.Node]++
	}
	var ready []string
	for nodeID := range nodes {
		if indegree[nodeID] == 0 {
			ready = append(ready, nodeID)
		}
	}
	sort.Strings(ready)
	var order []string
	for len(ready) > 0 {
		nodeID := ready[0]
		ready = ready[1:]
		order = append(order, nodeID)
		sort.Strings(children[nodeID])
		for _, child := range children[nodeID] {
			indegree[child]--
			if indegree[child] == 0 {
				ready = append(ready, child)
				sort.Strings(ready)
			}
		}
	}
	if len(order) != len(nodes) {
		return nil, fmt.Errorf("workflow contains a cycle")
	}
	return order, nil
}

func WorkflowExecutionPlan(workflow WorkflowDefinition) ([]WorkflowExecutionStep, error) {
	order, err := WorkflowExecutionOrder(workflow)
	if err != nil {
		return nil, err
	}
	nodes := map[string]WorkflowNode{}
	inputs := map[string][]string{}
	outputs := map[string][]string{}
	for _, node := range workflow.Nodes {
		nodes[node.ID] = node
	}
	for _, edge := range workflow.Edges {
		inputs[edge.Target.Node] = append(inputs[edge.Target.Node], edge.Source.Node)
		outputs[edge.Source.Node] = append(outputs[edge.Source.Node], edge.Target.Node)
	}
	steps := make([]WorkflowExecutionStep, 0, len(order))
	for _, nodeID := range order {
		node := nodes[nodeID]
		steps = append(steps, WorkflowExecutionStep{
			NodeID:         node.ID,
			Type:           node.Type,
			Label:          node.Label,
			AgentBlueprint: node.AgentBlueprint,
			InputsFrom:     sortedUniqueStrings(inputs[node.ID]),
			OutputsTo:      sortedUniqueStrings(outputs[node.ID]),
		})
	}
	return steps, nil
}

func SimulateWorkflow(workflow WorkflowDefinition, input string) ([]WorkflowSimulationStep, error) {
	order, err := WorkflowExecutionOrder(workflow)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(input) == "" {
		input = "Sample workflow input"
	}
	nodes := map[string]WorkflowNode{}
	incoming := map[string][]Edge{}
	outgoing := map[string][]Edge{}
	for _, node := range workflow.Nodes {
		nodes[node.ID] = node
	}
	for _, edge := range workflow.Edges {
		incoming[edge.Target.Node] = append(incoming[edge.Target.Node], edge)
		outgoing[edge.Source.Node] = append(outgoing[edge.Source.Node], edge)
	}

	messages := map[string]string{}
	steps := make([]WorkflowSimulationStep, 0, len(order))
	for _, nodeID := range order {
		node := nodes[nodeID]
		stepInputs := workflowSimulationInputs(incoming[node.ID], messages)
		outputs := workflowSimulationOutputs(node, stepInputs, input, outgoing[node.ID])
		for _, output := range outputs {
			messages[endpointKey(Endpoint{Node: node.ID, Port: output.Port})] = output.Content
		}
		steps = append(steps, WorkflowSimulationStep{
			NodeID:         node.ID,
			Type:           node.Type,
			Label:          node.Label,
			AgentBlueprint: node.AgentBlueprint,
			Inputs:         stepInputs,
			Outputs:        outputs,
		})
	}
	return steps, nil
}

func WorkflowDiagnostics(workflow WorkflowDefinition) []string {
	nodes := map[string]WorkflowNode{}
	incoming := map[string]int{}
	outgoing := map[string]int{}
	var inputNodes []string
	for _, node := range workflow.Nodes {
		nodes[node.ID] = node
		if node.Type == WorkflowNodeTypeInput {
			inputNodes = append(inputNodes, node.ID)
		}
	}
	for _, edge := range workflow.Edges {
		incoming[edge.Target.Node]++
		outgoing[edge.Source.Node]++
	}

	var diagnostics []string
	if len(inputNodes) == 0 {
		diagnostics = append(diagnostics, "workflow has no input node")
	}
	for _, node := range workflow.Nodes {
		switch node.Type {
		case WorkflowNodeTypeAgent:
			if incoming[node.ID] == 0 {
				diagnostics = append(diagnostics, fmt.Sprintf("agent node %q has no incoming message", node.ID))
			}
			if outgoing[node.ID] == 0 {
				diagnostics = append(diagnostics, fmt.Sprintf("agent node %q has no outgoing message", node.ID))
			}
		case WorkflowNodeTypeInput:
			if outgoing[node.ID] == 0 {
				diagnostics = append(diagnostics, fmt.Sprintf("input node %q is not connected", node.ID))
			}
		case WorkflowNodeTypeOutput:
			if incoming[node.ID] == 0 {
				diagnostics = append(diagnostics, fmt.Sprintf("output node %q has no incoming message", node.ID))
			}
		}
	}
	for _, nodeID := range unreachableWorkflowNodes(nodes, workflow.Edges, inputNodes) {
		diagnostics = append(diagnostics, fmt.Sprintf("node %q is not reachable from a workflow input", nodeID))
	}
	return diagnostics
}

func validateWorkflowNode(node WorkflowNode) error {
	switch node.Type {
	case WorkflowNodeTypeInput, WorkflowNodeTypeOutput:
	case WorkflowNodeTypeAgent:
		if strings.TrimSpace(node.AgentBlueprint) == "" {
			return fmt.Errorf("workflow agent node %q requires agent_blueprint", node.ID)
		}
	default:
		return fmt.Errorf("workflow node %q has unknown type %q", node.ID, node.Type)
	}
	return validatePorts(Node{ID: node.ID, Inputs: node.Inputs, Outputs: node.Outputs})
}

func findWorkflowPort(nodes map[string]WorkflowNode, endpoint Endpoint, direction string) (Port, error) {
	node, ok := nodes[endpoint.Node]
	if !ok {
		return Port{}, fmt.Errorf("node %q not found", endpoint.Node)
	}
	ports := node.Outputs
	if direction == DirectionInput {
		ports = node.Inputs
	}
	for _, port := range ports {
		if port.ID == endpoint.Port {
			return port, nil
		}
	}
	return Port{}, fmt.Errorf("%s port %q not found on node %q", direction, endpoint.Port, endpoint.Node)
}

func validateWorkflowDAG(workflow WorkflowDefinition, nodes map[string]WorkflowNode) error {
	visiting := map[string]bool{}
	visited := map[string]bool{}
	var visit func(string) error
	visit = func(nodeID string) error {
		if visiting[nodeID] {
			return fmt.Errorf("workflow contains a cycle at %q", nodeID)
		}
		if visited[nodeID] {
			return nil
		}
		visiting[nodeID] = true
		for _, edge := range workflow.Edges {
			if edge.Source.Node == nodeID {
				if err := visit(edge.Target.Node); err != nil {
					return err
				}
			}
		}
		visiting[nodeID] = false
		visited[nodeID] = true
		return nil
	}
	for nodeID := range nodes {
		if err := visit(nodeID); err != nil {
			return err
		}
	}
	return nil
}

func workflowSimulationInputs(edges []Edge, messages map[string]string) []WorkflowSimulationInput {
	inputs := make([]WorkflowSimulationInput, 0, len(edges))
	for _, edge := range edges {
		inputs = append(inputs, WorkflowSimulationInput{
			FromNode:   edge.Source.Node,
			FromPort:   edge.Source.Port,
			TargetPort: edge.Target.Port,
			Content:    messages[endpointKey(edge.Source)],
		})
	}
	return inputs
}

func workflowSimulationOutputs(node WorkflowNode, inputs []WorkflowSimulationInput, input string, outgoing []Edge) []WorkflowSimulationOutput {
	var outputs []WorkflowSimulationOutput
	for _, port := range node.Outputs {
		if port.Direction != DirectionOutput || port.Type != PortTypeMessage {
			continue
		}
		outputs = append(outputs, WorkflowSimulationOutput{
			Port:    port.ID,
			Content: workflowSimulationContent(node, inputs, input),
			To:      workflowSimulationTargets(port.ID, outgoing),
		})
	}
	return outputs
}

func workflowSimulationContent(node WorkflowNode, inputs []WorkflowSimulationInput, input string) string {
	label := node.Label
	if strings.TrimSpace(label) == "" {
		label = node.ID
	}
	switch node.Type {
	case WorkflowNodeTypeInput:
		return input
	case WorkflowNodeTypeAgent:
		var from []string
		for _, input := range inputs {
			from = append(from, input.FromNode+"."+input.FromPort)
		}
		return fmt.Sprintf("[Simulated %s via %s]\nInputs: %s", label, node.AgentBlueprint, strings.Join(from, ", "))
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

func workflowSimulationTargets(portID string, outgoing []Edge) []Endpoint {
	var targets []Endpoint
	for _, edge := range outgoing {
		if edge.Source.Port == portID {
			targets = append(targets, edge.Target)
		}
	}
	return targets
}

func endpointKey(endpoint Endpoint) string {
	return endpoint.Node + ":" + endpoint.Port
}

func unreachableWorkflowNodes(nodes map[string]WorkflowNode, edges []Edge, roots []string) []string {
	if len(roots) == 0 {
		return nil
	}
	children := map[string][]string{}
	for _, edge := range edges {
		children[edge.Source.Node] = append(children[edge.Source.Node], edge.Target.Node)
	}
	seen := map[string]bool{}
	stack := append([]string(nil), roots...)
	for len(stack) > 0 {
		nodeID := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[nodeID] {
			continue
		}
		seen[nodeID] = true
		stack = append(stack, children[nodeID]...)
	}
	var unreachable []string
	for nodeID := range nodes {
		if !seen[nodeID] {
			unreachable = append(unreachable, nodeID)
		}
	}
	sort.Strings(unreachable)
	return unreachable
}
