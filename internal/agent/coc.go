package agent

// 模块说明：
// coc 模块提供 Call of Cthulhu 跑团常用工具：通用骰子表达式、D100 技能检定、
// 对抗检定和理智检定。规则以 CoC 7e 常用判定层级为核心：critical、extreme、
// hard、regular、failure、fumble。工具输出 JSON，方便 TUI、外部消息平台和 LLM 继续处理。

import (
	"context"
	crand "crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

var diceTermPattern = regexp.MustCompile(`^([+-]?)(?:(\d*)d(\d+)|(\d+))$`)

type cocModule struct{}

type diceTerm struct {
	Sign  int
	Count int
	Sides int
	Flat  int
}

type diceRollResult struct {
	Expression string     `json:"expression"`
	Total      int        `json:"total"`
	Terms      []diceTerm `json:"terms"`
	Rolls      [][]int    `json:"rolls"`
}

type cocCheckResult struct {
	Skill        string `json:"skill,omitempty"`
	Roll         int    `json:"roll"`
	Target       int    `json:"target"`
	SuccessLevel string `json:"success_level"`
	Rank         int    `json:"rank"`
	BonusDice    int    `json:"bonus_dice,omitempty"`
}

func (m *cocModule) ID() string { return "coc" }

func (m *cocModule) Init(ctx ModuleContext) error { return nil }

func (m *cocModule) PromptBlocks(ctx context.Context, req PromptRequest) ([]PromptBlock, error) {
	if !hasString(req.ToolNames, "coc_roll_dice") && !hasString(req.ToolNames, "coc_skill_check") {
		return nil, nil
	}
	return []PromptBlock{{
		Module:  m.ID(),
		Name:    "CoC Keeper Tools",
		Source:  "built-in coc module",
		Content: "CoC tools are available for tabletop play: coc_roll_dice for expressions like 1d100 or 2d6+3, coc_skill_check for D100 skill checks with bonus/penalty dice, coc_opposed_check for opposed rolls, and coc_sanity_check for SAN loss. Keep narration atmospheric but separate mechanics from story text.",
	}}, nil
}

func (m *cocModule) ToolDefinitions() []anthropic.ToolParam {
	return cocToolDefinitions()
}

func (m *cocModule) ToolHandlers() map[string]ToolHandler {
	return map[string]ToolHandler{
		"coc_roll_dice":     m.runCoCRollDice,
		"coc_skill_check":   m.runCoCSkillCheck,
		"coc_opposed_check": m.runCoCOpposedCheck,
		"coc_sanity_check":  m.runCoCSanityCheck,
	}
}

func (m *cocModule) RuntimeSnapshot() any {
	return map[string]any{
		"tools": []string{"coc_roll_dice", "coc_skill_check", "coc_opposed_check", "coc_sanity_check"},
		"rules": "CoC 7e D100 success levels",
	}
}

func cocToolDefinitions() []anthropic.ToolParam {
	return []anthropic.ToolParam{
		{
			Name:        "coc_roll_dice",
			Description: anthropic.String("Roll a dice expression such as 1d100, 2d6+3, or d3-1."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]any{
					"expression": map[string]any{"type": "string"},
					"label":      map[string]any{"type": "string"},
				},
				Required: []string{"expression"},
			},
		},
		{
			Name:        "coc_skill_check",
			Description: anthropic.String("Run a CoC 7e D100 skill check with optional bonus/penalty dice."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]any{
					"skill":      map[string]any{"type": "string"},
					"target":     map[string]any{"type": "integer"},
					"bonus_dice": map[string]any{"type": "integer", "description": "Positive for bonus dice, negative for penalty dice."},
					"roll":       map[string]any{"type": "integer", "description": "Optional fixed D100 roll for replay/testing."},
				},
				Required: []string{"target"},
			},
		},
		{
			Name:        "coc_opposed_check",
			Description: anthropic.String("Resolve an opposed CoC check by comparing success level, then lower roll on ties."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]any{
					"actor_skill":     map[string]any{"type": "string"},
					"actor_target":    map[string]any{"type": "integer"},
					"actor_roll":      map[string]any{"type": "integer"},
					"opponent_skill":  map[string]any{"type": "string"},
					"opponent_target": map[string]any{"type": "integer"},
					"opponent_roll":   map[string]any{"type": "integer"},
				},
				Required: []string{"actor_target", "opponent_target"},
			},
		},
		{
			Name:        "coc_sanity_check",
			Description: anthropic.String("Run a sanity check and roll SAN loss from success/failure loss expressions."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]any{
					"current_san":  map[string]any{"type": "integer"},
					"success_loss": map[string]any{"type": "string", "description": "Dice expression, e.g. 0 or 1d3."},
					"failure_loss": map[string]any{"type": "string", "description": "Dice expression, e.g. 1d6."},
					"roll":         map[string]any{"type": "integer"},
				},
				Required: []string{"current_san", "success_loss", "failure_loss"},
			},
		},
	}
}

func (m *cocModule) runCoCRollDice(raw json.RawMessage) string {
	var input struct {
		Expression string `json:"expression"`
		Label      string `json:"label"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	result, err := rollDiceExpression(input.Expression)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	response := map[string]any{"label": strings.TrimSpace(input.Label), "roll": result}
	return marshalCompactJSON(response)
}

func (m *cocModule) runCoCSkillCheck(raw json.RawMessage) string {
	var input struct {
		Skill     string `json:"skill"`
		Target    int    `json:"target"`
		BonusDice int    `json:"bonus_dice"`
		Roll      *int   `json:"roll"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	roll, err := checkedD100Roll(input.Roll, input.BonusDice)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	result, err := cocSkillCheck(strings.TrimSpace(input.Skill), input.Target, roll, input.BonusDice)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return marshalCompactJSON(result)
}

func (m *cocModule) runCoCOpposedCheck(raw json.RawMessage) string {
	var input struct {
		ActorSkill     string `json:"actor_skill"`
		ActorTarget    int    `json:"actor_target"`
		ActorRoll      *int   `json:"actor_roll"`
		OpponentSkill  string `json:"opponent_skill"`
		OpponentTarget int    `json:"opponent_target"`
		OpponentRoll   *int   `json:"opponent_roll"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	actorRoll, err := checkedD100Roll(input.ActorRoll, 0)
	if err != nil {
		return fmt.Sprintf("Error: actor %v", err)
	}
	opponentRoll, err := checkedD100Roll(input.OpponentRoll, 0)
	if err != nil {
		return fmt.Sprintf("Error: opponent %v", err)
	}
	actor, err := cocSkillCheck(input.ActorSkill, input.ActorTarget, actorRoll, 0)
	if err != nil {
		return fmt.Sprintf("Error: actor %v", err)
	}
	opponent, err := cocSkillCheck(input.OpponentSkill, input.OpponentTarget, opponentRoll, 0)
	if err != nil {
		return fmt.Sprintf("Error: opponent %v", err)
	}
	winner := opposedWinner(actor, opponent)
	return marshalCompactJSON(map[string]any{"actor": actor, "opponent": opponent, "winner": winner})
}

func (m *cocModule) runCoCSanityCheck(raw json.RawMessage) string {
	var input struct {
		CurrentSAN  int    `json:"current_san"`
		SuccessLoss string `json:"success_loss"`
		FailureLoss string `json:"failure_loss"`
		Roll        *int   `json:"roll"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	roll, err := checkedD100Roll(input.Roll, 0)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	check, err := cocSkillCheck("sanity", input.CurrentSAN, roll, 0)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	lossExpr := input.FailureLoss
	if check.Rank > 0 {
		lossExpr = input.SuccessLoss
	}
	loss, err := rollDiceExpression(lossExpr)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	remaining := input.CurrentSAN - loss.Total
	if remaining < 0 {
		remaining = 0
	}
	return marshalCompactJSON(map[string]any{"check": check, "loss": loss, "remaining_san": remaining})
}

func rollDiceExpression(expression string) (diceRollResult, error) {
	terms, normalized, err := parseDiceExpression(expression)
	if err != nil {
		return diceRollResult{}, err
	}
	result := diceRollResult{Expression: normalized, Terms: terms}
	for _, term := range terms {
		termRolls := []int{}
		if term.Sides > 0 {
			for range term.Count {
				roll, err := randomInt(term.Sides)
				if err != nil {
					return diceRollResult{}, err
				}
				termRolls = append(termRolls, roll)
				result.Total += term.Sign * roll
			}
		} else {
			result.Total += term.Sign * term.Flat
		}
		result.Rolls = append(result.Rolls, termRolls)
	}
	return result, nil
}

func parseDiceExpression(expression string) ([]diceTerm, string, error) {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(expression), " ", ""))
	if normalized == "" {
		return nil, "", fmt.Errorf("expression is required")
	}
	if normalized[0] != '+' && normalized[0] != '-' {
		normalized = "+" + normalized
	}
	parts := splitDiceTerms(normalized)
	if len(parts) == 0 {
		return nil, "", fmt.Errorf("invalid dice expression: %s", expression)
	}
	terms := make([]diceTerm, 0, len(parts))
	for _, part := range parts {
		match := diceTermPattern.FindStringSubmatch(part)
		if match == nil {
			return nil, "", fmt.Errorf("invalid dice term: %s", part)
		}
		sign := 1
		if match[1] == "-" {
			sign = -1
		}
		if match[3] != "" {
			count := 1
			if match[2] != "" {
				parsed, _ := strconv.Atoi(match[2])
				count = parsed
			}
			sides, _ := strconv.Atoi(match[3])
			if count <= 0 || count > 100 || sides <= 0 || sides > 10000 {
				return nil, "", fmt.Errorf("dice term out of range: %s", part)
			}
			terms = append(terms, diceTerm{Sign: sign, Count: count, Sides: sides})
			continue
		}
		flat, _ := strconv.Atoi(match[4])
		terms = append(terms, diceTerm{Sign: sign, Flat: flat})
	}
	return terms, strings.TrimPrefix(normalized, "+"), nil
}

func splitDiceTerms(expression string) []string {
	var terms []string
	start := 0
	for i := 1; i < len(expression); i++ {
		if expression[i] == '+' || expression[i] == '-' {
			terms = append(terms, expression[start:i])
			start = i
		}
	}
	terms = append(terms, expression[start:])
	return terms
}

func checkedD100Roll(fixed *int, bonusDice int) (int, error) {
	if fixed != nil {
		if *fixed < 1 || *fixed > 100 {
			return 0, fmt.Errorf("roll must be between 1 and 100")
		}
		return *fixed, nil
	}
	return rollD100WithBonusPenalty(bonusDice)
}

func rollD100WithBonusPenalty(bonusDice int) (int, error) {
	if bonusDice > 2 {
		bonusDice = 2
	}
	if bonusDice < -2 {
		bonusDice = -2
	}
	ones, err := randomInt(10)
	if err != nil {
		return 0, err
	}
	ones = ones % 10
	tensCount := 1
	if bonusDice != 0 {
		tensCount += absInt(bonusDice)
	}
	tensValues := make([]int, 0, tensCount)
	for range tensCount {
		roll, err := randomInt(10)
		if err != nil {
			return 0, err
		}
		tensValues = append(tensValues, (roll%10)*10)
	}
	selected := tensValues[0]
	for _, value := range tensValues[1:] {
		if bonusDice > 0 && value < selected {
			selected = value
		}
		if bonusDice < 0 && value > selected {
			selected = value
		}
	}
	roll := selected + ones
	if roll == 0 {
		return 100, nil
	}
	return roll, nil
}

func cocSkillCheck(skill string, target int, roll int, bonusDice int) (cocCheckResult, error) {
	if target < 1 || target > 100 {
		return cocCheckResult{}, fmt.Errorf("target must be between 1 and 100")
	}
	if roll < 1 || roll > 100 {
		return cocCheckResult{}, fmt.Errorf("roll must be between 1 and 100")
	}
	level, rank := cocSuccessLevel(target, roll)
	return cocCheckResult{Skill: skill, Roll: roll, Target: target, SuccessLevel: level, Rank: rank, BonusDice: bonusDice}, nil
}

func cocSuccessLevel(target int, roll int) (string, int) {
	if roll == 1 {
		return "critical", 4
	}
	if roll == 100 || target < 50 && roll >= 96 {
		return "fumble", -1
	}
	if roll > target {
		return "failure", 0
	}
	if roll <= target/5 {
		return "extreme", 3
	}
	if roll <= target/2 {
		return "hard", 2
	}
	return "regular", 1
}

func opposedWinner(actor cocCheckResult, opponent cocCheckResult) string {
	if actor.Rank <= 0 && opponent.Rank <= 0 {
		return "none"
	}
	if actor.Rank > opponent.Rank {
		return "actor"
	}
	if opponent.Rank > actor.Rank {
		return "opponent"
	}
	if actor.Roll < opponent.Roll {
		return "actor"
	}
	if opponent.Roll < actor.Roll {
		return "opponent"
	}
	return "tie"
}

func randomInt(max int) (int, error) {
	if max <= 0 {
		return 0, fmt.Errorf("max must be positive")
	}
	n, err := crand.Int(crand.Reader, big.NewInt(int64(max)))
	if err != nil {
		return 0, err
	}
	return int(n.Int64()) + 1, nil
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
