package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

// ── 拒绝列表（Gate 1）──────────────────────────────────

var denyList = []string{
	"rm -rf / --",
	"sudo",
	"shutdown",
	"reboot",
	"mkfs",
	"dd if=",
	"> /dev/",
}

// checkDenyList 检查命令是否命中绝对禁止列表
func checkDenyList(command string) *string {
	for _, pattern := range denyList {
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

var permissionRules = []permissionRule{
	{
		tools: []string{"write_file", "edit_file"},
		check: func(raw json.RawMessage) bool {
			var input struct {
				Path string `json:"path"`
			}
			json.Unmarshal(raw, &input)
			_, err := safePath(input.Path)
			return err != nil // 路径逃逸 → 触发拦截
		},
		message: "Writing outside workspace",
	},
	{
		tools: []string{"bash"},
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

// checkRules 遍历权限规则，返回命中的拦截信息
func checkRules(toolName string, input json.RawMessage) *string {
	for _, rule := range permissionRules {
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
func askUser(toolName string, input json.RawMessage, reason string) bool {
	fmt.Printf("%s ⚠  %s: %s(%s)\n", colorYellow("WARNING"), reason, toolName, truncate(string(input), 60))
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
func permissionHook(args ...any) *string {
	block, ok := args[0].(anthropic.ToolUseBlock)
	if !ok {
		return nil
	}

	rawInput, _ := json.Marshal(block.Input)

	// Gate 1: 拒绝列表
	if block.Name == "bash" {
		var input struct {
			Command string `json:"command"`
		}
		json.Unmarshal(rawInput, &input)
		if reason := checkDenyList(input.Command); reason != nil {
			msg := "Permission denied by deny list."
			fmt.Println(colorRed("ERROR"), *reason)
			return &msg
		}
	}

	// Gate 2: 规则检查
	if reason := checkRules(block.Name, rawInput); reason != nil {
		// Gate 3: 用户确认
		if !askUser(block.Name, rawInput, *reason) {
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
