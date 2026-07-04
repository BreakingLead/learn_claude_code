package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseDiceExpressionSupportsCommonForms(t *testing.T) {
	terms, normalized, err := parseDiceExpression("2d6 + 3 - d4")
	if err != nil {
		t.Fatal(err)
	}
	if normalized != "2d6+3-d4" || len(terms) != 3 {
		t.Fatalf("unexpected parse: normalized=%q terms=%+v", normalized, terms)
	}
	if terms[0].Count != 2 || terms[0].Sides != 6 || terms[1].Flat != 3 || terms[2].Sign != -1 || terms[2].Sides != 4 {
		t.Fatalf("unexpected terms: %+v", terms)
	}
}

func TestCoCSuccessLevels(t *testing.T) {
	cases := []struct {
		target int
		roll   int
		level  string
	}{
		{50, 1, "critical"},
		{50, 10, "extreme"},
		{50, 25, "hard"},
		{50, 50, "regular"},
		{50, 51, "failure"},
		{40, 96, "fumble"},
		{50, 100, "fumble"},
	}
	for _, tc := range cases {
		level, _ := cocSuccessLevel(tc.target, tc.roll)
		if level != tc.level {
			t.Fatalf("target=%d roll=%d expected %s got %s", tc.target, tc.roll, tc.level, level)
		}
	}
}

func TestCoCSkillCheckToolUsesFixedRoll(t *testing.T) {
	module := &cocModule{}
	result := module.runCoCSkillCheck([]byte(`{"skill":"Spot Hidden","target":60,"roll":24}`))
	var check cocCheckResult
	if err := json.Unmarshal([]byte(result), &check); err != nil {
		t.Fatalf("result is not json: %s", result)
	}
	if check.SuccessLevel != "hard" || check.Roll != 24 || check.Target != 60 {
		t.Fatalf("unexpected check: %+v", check)
	}
}

func TestCoCOpposedCheckPicksHigherSuccessRank(t *testing.T) {
	module := &cocModule{}
	result := module.runCoCOpposedCheck([]byte(`{"actor_skill":"Fighting","actor_target":60,"actor_roll":20,"opponent_skill":"Dodge","opponent_target":60,"opponent_roll":50}`))
	if !strings.Contains(result, `"winner":"actor"`) || !strings.Contains(result, `"success_level":"hard"`) {
		t.Fatalf("unexpected opposed result: %s", result)
	}
}

func TestCoCSanityCheckUsesFailureLoss(t *testing.T) {
	module := &cocModule{}
	result := module.runCoCSanityCheck([]byte(`{"current_san":55,"success_loss":"0","failure_loss":"3","roll":80}`))
	if !strings.Contains(result, `"remaining_san":52`) || !strings.Contains(result, `"success_level":"failure"`) {
		t.Fatalf("unexpected sanity result: %s", result)
	}
}

func TestCoCModeRestrictsToolsAndInjectsPrompt(t *testing.T) {
	rt := newAgentRuntime(testConfig(t.TempDir()), nil, nil)
	if err := rt.switchMode("coc"); err != nil {
		t.Fatal(err)
	}
	names := rt.mainAgentSpec().ToolNames
	for _, want := range []string{"coc_roll_dice", "coc_skill_check", "coc_opposed_check", "coc_sanity_check", "messaging_normalize"} {
		if !hasString(names, want) {
			t.Fatalf("coc mode missing %q in %v", want, names)
		}
	}
	if hasString(names, "write_file") || hasString(names, "bash") {
		t.Fatalf("coc mode should not expose build tools by default: %v", names)
	}
	prompt := rt.getSystemPrompt(names)
	if !strings.Contains(prompt, "Mode: coc") || !strings.Contains(prompt, "Call of Cthulhu keeper mode") || !strings.Contains(prompt, "CoC Keeper Tools") {
		t.Fatalf("missing coc mode/module prompt: %s", prompt)
	}
}

func TestPlanModeDoesNotInjectCoCOrMessagingPrompt(t *testing.T) {
	rt := newAgentRuntime(testConfig(t.TempDir()), nil, nil)
	if err := rt.switchMode("plan"); err != nil {
		t.Fatal(err)
	}
	prompt := rt.getSystemPrompt(rt.mainAgentSpec().ToolNames)
	if strings.Contains(prompt, "CoC Keeper Tools") || strings.Contains(prompt, "Messaging Middleware") {
		t.Fatalf("plan mode should not include unavailable module prompts: %s", prompt)
	}
}
