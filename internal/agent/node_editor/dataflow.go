package nodeeditor

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

type EvaluationContext struct {
	Now time.Time
}

type PromptSource struct {
	Node Node
	OK   bool
}

func ResolvePromptSource(blueprint Blueprint, nodeID string, ctx EvaluationContext) PromptSource {
	return resolvePromptSource(blueprint, nodeID, ctx, nil)
}

func resolvePromptSource(blueprint Blueprint, nodeID string, ctx EvaluationContext, stack []string) PromptSource {
	if containsNodeID(stack, nodeID) {
		return PromptSource{}
	}
	nodes := nodeMap(blueprint)
	node, ok := nodes[nodeID]
	if !ok {
		return PromptSource{}
	}
	if node.Type != NodeTypeSelect {
		return PromptSource{Node: node, OK: true}
	}
	condition := evalBooleanInput(blueprint, nodes, node, "condition", boolNodeConfig(node.Config, "default"), ctx, nil)
	targetPort := "false"
	if condition {
		targetPort = "true"
	}
	sourceID := upstreamNodeForInput(blueprint, node.ID, targetPort)
	if sourceID == "" {
		return PromptSource{Node: node, OK: true}
	}
	return resolvePromptSource(blueprint, sourceID, ctx, append(stack, nodeID))
}

func PromptSourceLabel(blueprint Blueprint, nodeID string, ctx EvaluationContext) string {
	source := ResolvePromptSource(blueprint, nodeID, ctx)
	if !source.OK {
		return nodeID
	}
	name := strings.TrimSpace(source.Node.Label)
	if name == "" {
		name = source.Node.ID
	}
	if source.Node.ID == nodeID {
		return name
	}
	return fmt.Sprintf("%s via %s", name, nodeID)
}

func evalBooleanInput(blueprint Blueprint, nodes map[string]Node, node Node, portID string, fallback bool, ctx EvaluationContext, stack []string) bool {
	sourceID := upstreamNodeForInput(blueprint, node.ID, portID)
	if sourceID == "" {
		return fallback
	}
	return evalBooleanNode(blueprint, nodes, sourceID, fallback, ctx, stack)
}

func evalBooleanNode(blueprint Blueprint, nodes map[string]Node, nodeID string, fallback bool, ctx EvaluationContext, stack []string) bool {
	if containsNodeID(stack, nodeID) {
		return fallback
	}
	node, ok := nodes[nodeID]
	if !ok {
		return fallback
	}
	switch node.Type {
	case NodeTypeCompare:
		left := evalValueInput(blueprint, nodes, node, "a", anyNodeConfig(node.Config, "a"), ctx, append(stack, nodeID))
		right := evalValueInput(blueprint, nodes, node, "b", anyNodeConfig(node.Config, "b"), ctx, append(stack, nodeID))
		return compareValues(left, right, stringNodeConfig(node.Config, "operator"), fallback)
	default:
		return boolNodeConfigWithFallback(node.Config, "value", fallback)
	}
}

func evalValueInput(blueprint Blueprint, nodes map[string]Node, node Node, portID string, fallback any, ctx EvaluationContext, stack []string) any {
	sourceID := upstreamNodeForInput(blueprint, node.ID, portID)
	if sourceID == "" {
		return fallback
	}
	return evalValueNode(blueprint, nodes, sourceID, fallback, ctx, stack)
}

func evalValueNode(blueprint Blueprint, nodes map[string]Node, nodeID string, fallback any, ctx EvaluationContext, stack []string) any {
	if containsNodeID(stack, nodeID) {
		return fallback
	}
	node, ok := nodes[nodeID]
	if !ok {
		return fallback
	}
	switch node.Type {
	case NodeTypeTime:
		now := ctx.Now
		if now.IsZero() {
			now = time.Now()
		}
		switch strings.ToLower(stringNodeConfig(node.Config, "unit")) {
		case "unix":
			return float64(now.Unix())
		case "minute":
			return float64(now.Hour()*60 + now.Minute())
		case "second":
			return float64(now.Hour()*3600 + now.Minute()*60 + now.Second())
		default:
			return float64(now.Hour()) + float64(now.Minute())/60 + float64(now.Second())/3600
		}
	default:
		if value := anyNodeConfig(node.Config, "value"); value != nil {
			return value
		}
		return fallback
	}
}

func upstreamNodeForInput(blueprint Blueprint, nodeID string, portID string) string {
	for _, edge := range blueprint.Edges {
		if edge.Target.Node == nodeID && edge.Target.Port == portID {
			return edge.Source.Node
		}
	}
	return ""
}

func compareValues(left any, right any, operator string, fallback bool) bool {
	operator = strings.TrimSpace(operator)
	if operator == "" {
		operator = ">="
	}
	leftNumber, leftOK := numericValue(left)
	rightNumber, rightOK := numericValue(right)
	if !leftOK || !rightOK {
		return fallback
	}
	switch operator {
	case ">", "gt":
		return leftNumber > rightNumber
	case ">=", "gte":
		return leftNumber >= rightNumber
	case "<", "lt":
		return leftNumber < rightNumber
	case "<=", "lte":
		return leftNumber <= rightNumber
	case "==", "=", "eq":
		return nearlyEqual(leftNumber, rightNumber)
	case "!=", "ne":
		return !nearlyEqual(leftNumber, rightNumber)
	default:
		return fallback
	}
}

func numericValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case string:
		number, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return number, err == nil
	default:
		return 0, false
	}
}

func nearlyEqual(left float64, right float64) bool {
	return math.Abs(left-right) < 1e-9
}

func anyNodeConfig(config map[string]any, key string) any {
	if config == nil {
		return nil
	}
	return config[key]
}

func boolNodeConfig(config map[string]any, key string) bool {
	return boolNodeConfigWithFallback(config, key, false)
}

func boolNodeConfigWithFallback(config map[string]any, key string, fallback bool) bool {
	if config == nil {
		return fallback
	}
	value, ok := config[key]
	if !ok {
		return fallback
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		default:
			return fallback
		}
	default:
		return fallback
	}
}
