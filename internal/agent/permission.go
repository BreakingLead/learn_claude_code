package agent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

// checkDenyList 检查命令是否命中绝对禁止列表
func checkDenyList(command string) *string {
	for _, pattern := range []string{
		"rm -rf / --",
		"sudo",
		"shutdown",
		"reboot",
		"mkfs",
		"dd if=",
		"> /dev/",
	} {
		if strings.Contains(command, pattern) {
			msg := fmt.Sprintf("Blocked: '%s' is on the deny list", pattern)
			return &msg
		}
	}
	return nil
}

// ── 规则检查（Gate 2）──────────────────────────────────

type permissionRule struct {
	tools   []string
	check   func(input json.RawMessage) bool
	message string
}

func (rt *agentRuntime) permissionRules() []permissionRule {
	return []permissionRule{
		{
			tools: []string{"write_file", "edit_file"},
			check: func(raw json.RawMessage) bool {
				var input struct {
					Path string `json:"path"`
				}
				json.Unmarshal(raw, &input)
				_, err := rt.safePath(input.Path)
				return err != nil // 路径逃逸 → 触发拦截
			},
			message: "Writing outside workspace",
		},
		{
			tools: []string{"bash", "background_bash"},
			check: func(raw json.RawMessage) bool {
				var input struct {
					Command string `json:"command"`
				}
				json.Unmarshal(raw, &input)
				for _, kw := range []string{"rm ", "> /etc/", "chmod 777"} {
					if strings.Contains(input.Command, kw) {
						return true
					}
				}
				return false
			},
			message: "Potentially destructive command",
		},
	}
}

// checkRules 遍历权限规则，返回命中的拦截信息
func (rt *agentRuntime) checkRules(toolName string, input json.RawMessage) *string {
	for _, rule := range rt.permissionRules() {
		for _, t := range rule.tools {
			if t == toolName && rule.check(input) {
				return &rule.message
			}
		}
	}
	return nil
}

// ── 用户确认（Gate 3）──────────────────────────────────

// askUser 在规则命中后询问用户是否允许
func (rt *agentRuntime) askUser(toolName string, input json.RawMessage, reason string) bool {
	prompt := fmt.Sprintf("%s ⚠  %s: %s(%s)", colorYellow("WARNING"), reason, toolName, truncate(string(input), 60))
	if answer, ok := rt.requestApproval(prompt); ok {
		return answer
	}

	fmt.Println(prompt)
	fmt.Print("   Allow? [y/N]: ")

	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
		return answer == "y" || answer == "yes"
	}
	return false
}

// ── 三层门控串联 ──────────────────────────────────────

// permissionHook 作为 PreToolUse 钩子，串联三层权限检查
// 返回 nil 表示放行，返回 *string 表示拒绝（内容作为 tool_result 返回给模型）
func (rt *agentRuntime) permissionHook(args ...any) *string {
	block, ok := args[0].(anthropic.ToolUseBlock)
	if !ok {
		return nil
	}

	rawInput, _ := json.Marshal(block.Input)

	// Gate 1: 拒绝列表
	if block.Name == "bash" || block.Name == "background_bash" {
		var input struct {
			Command string `json:"command"`
		}
		json.Unmarshal(rawInput, &input)
		if reason := checkDenyList(input.Command); reason != nil {
			msg := "Permission denied by deny list."
			rt.emitLine("%s %s", colorRed("ERROR"), *reason)
			return &msg
		}
	}

	// Gate 2: 规则检查
	if reason := rt.checkRules(block.Name, rawInput); reason != nil {
		// Gate 3: 用户确认
		if !rt.askUser(block.Name, rawInput, *reason) {
			msg := "Permission denied by user."
			return &msg
		}
	}

	return nil
}

// truncate 截断字符串用于预览
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
