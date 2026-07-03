package agent

// 模块说明：
// 这个文件负责把运行时配置、工具列表、技能目录、项目文档和持久记忆组装成
// system prompt。技能的扫描和加载仍保留在 skills.go，这里只处理提示词上下文。

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
)

type promptContext struct {
	Workdir      string
	Model        string
	ToolNames    []string
	PromptBlocks []PromptBlock
}

// sortedSkills 把技能 map 转成按名称排序的切片，保证提示词稳定。
func sortedSkills(skills map[string]SkillInfo) []SkillInfo {
	names := make([]string, 0, len(skills))
	for name := range skills {
		names = append(names, name)
	}
	sort.Strings(names)

	result := make([]SkillInfo, 0, len(names))
	for _, name := range names {
		result = append(result, skills[name])
	}
	return result
}

// listSkills 格式化技能列表用于系统提示。
func listSkills(skills []SkillInfo) string {
	if len(skills) == 0 {
		return "(no skills available)"
	}
	var lines []string
	for _, skill := range skills {
		lines = append(lines, fmt.Sprintf("- **%s**: %s", skill.Name, skill.Description))
	}
	return strings.Join(lines, "\n")
}

// promptContext 收集组装 system prompt 所需的运行时上下文。
func (rt *agentRuntime) promptContext(toolNames []string) promptContext {
	sortedToolNames := append([]string(nil), toolNames...)
	sort.Strings(sortedToolNames)
	blocks := rt.modules.promptBlocks(context.Background(), PromptRequest{ToolNames: sortedToolNames})
	mode := rt.activeMode()
	if strings.TrimSpace(mode.Prompt) != "" {
		blocks = append([]PromptBlock{
			{
				Module:  "mode",
				Name:    fmt.Sprintf("Mode: %s", mode.Name),
				Source:  "active mode",
				Content: mode.Prompt,
			},
		}, blocks...)
	}
	return promptContext{
		Workdir:      rt.config.Workdir,
		Model:        rt.config.Model,
		ToolNames:    sortedToolNames,
		PromptBlocks: blocks,
	}
}

// contextKey 根据提示词上下文生成稳定缓存键。
func (ctx promptContext) contextKey() string {
	var sb strings.Builder
	sb.WriteString(ctx.Workdir)
	sb.WriteString("\n")
	sb.WriteString(ctx.Model)
	sb.WriteString("\n")
	sb.WriteString(strings.Join(ctx.ToolNames, ","))
	sb.WriteString("\n")
	for _, block := range ctx.PromptBlocks {
		sb.WriteString(block.Module)
		sb.WriteString(":")
		sb.WriteString(block.Name)
		sb.WriteString(":")
		sb.WriteString(block.Source)
		sb.WriteString(":")
		sb.WriteString(block.Content)
		sb.WriteString("\n")
	}
	sum := sha256.Sum256([]byte(sb.String()))
	return fmt.Sprintf("%x", sum)
}

// assembleSystemPrompt 只根据 promptContext 组装文本，便于缓存和测试。
func assembleSystemPrompt(ctx promptContext) string {
	var sections []string
	sections = append(sections,
		fmt.Sprintf("You are a coding agent working in `%s` directory.", ctx.Workdir),
		fmt.Sprintf("Model: %s.", ctx.Model),
		"Available tools: "+strings.Join(ctx.ToolNames, ", "),
	)

	if len(ctx.PromptBlocks) > 0 {
		var blocks []string
		for _, block := range ctx.PromptBlocks {
			blocks = append(blocks, renderPromptBlock(block))
		}
		sections = append(sections, "Module context:\n"+strings.Join(blocks, "\n\n"))
	}
	return strings.Join(sections, "\n\n")
}

// renderPromptBlock 将统一 prompt block 渲染成 system prompt 片段。
func renderPromptBlock(block PromptBlock) string {
	return fmt.Sprintf("## %s\nSource: %s\n%s", block.Name, block.Source, block.Content)
}

// getSystemPrompt 使用稳定 context key 缓存 system prompt，避免每轮重复组装。
func (rt *agentRuntime) getSystemPrompt(toolNames []string) string {
	ctx := rt.promptContext(toolNames)
	key := ctx.contextKey()
	if rt.promptCache.contextKey == key {
		rt.emitLine("[cache hit] system prompt context key: %s", key[:12])
		return rt.promptCache.prompt
	}
	prompt := assembleSystemPrompt(ctx)
	rt.promptCache = promptCache{contextKey: key, prompt: prompt}
	rt.emitLine("[assembled] system prompt sections: base, tools, module_blocks=%d", len(ctx.PromptBlocks))
	return prompt
}
