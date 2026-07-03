package agent

// 模块说明：
// 这个文件负责把运行时配置、工具列表、技能目录、项目文档和持久记忆组装成
// system prompt。技能的扫描和加载仍保留在 skills.go，这里只处理提示词上下文。

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type promptContext struct {
	Workdir       string
	Model         string
	ToolNames     []string
	Skills        []SkillInfo
	ProjectBlocks []contextBlock
	MemoryBlocks  []contextBlock
}

type contextBlock struct {
	Name    string
	Path    string
	Content string
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
	return promptContext{
		Workdir:       rt.config.Workdir,
		Model:         rt.config.Model,
		ToolNames:     sortedToolNames,
		Skills:        sortedSkills(rt.scanSkills()),
		ProjectBlocks: rt.loadProjectBlocks(),
		MemoryBlocks:  rt.loadMemoryBlocks(),
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
	for _, skill := range ctx.Skills {
		sb.WriteString(skill.Name)
		sb.WriteString(":")
		sb.WriteString(skill.Description)
		sb.WriteString("\n")
	}
	for _, block := range ctx.ProjectBlocks {
		sb.WriteString(block.Name)
		sb.WriteString(":")
		sb.WriteString(block.Path)
		sb.WriteString(":")
		sb.WriteString(block.Content)
		sb.WriteString("\n")
	}
	for _, block := range ctx.MemoryBlocks {
		sb.WriteString(block.Name)
		sb.WriteString(":")
		sb.WriteString(block.Path)
		sb.WriteString(":")
		sb.WriteString(block.Content)
		sb.WriteString("\n")
	}
	sum := sha256.Sum256([]byte(sb.String()))
	return fmt.Sprintf("%x", sum)
}

// loadProjectBlocks 读取会影响系统提示词的项目级上下文文件。
func (rt *agentRuntime) loadProjectBlocks() []contextBlock {
	candidates := []struct {
		name string
		path string
	}{
		{name: "Repository Guidelines", path: filepath.Join(rt.config.Workdir, "AGENTS.md")},
		{name: "Project README", path: filepath.Join(rt.config.Workdir, "README.md")},
		{name: "Task Index", path: rt.config.TaskIndex},
	}

	return rt.readContextBlocks(candidates, 6000)
}

// loadMemoryBlocks 读取会注入系统提示词的持久记忆索引。
func (rt *agentRuntime) loadMemoryBlocks() []contextBlock {
	candidates := []struct {
		name string
		path string
	}{
		{name: "Memory", path: rt.config.MemoryIndex},
	}
	return rt.readContextBlocks(candidates, 6000)
}

// readContextBlocks 读取一组候选上下文文件，并按字符上限裁剪单块内容。
func (rt *agentRuntime) readContextBlocks(candidates []struct {
	name string
	path string
}, limit int) []contextBlock {
	var blocks []contextBlock
	for _, candidate := range candidates {
		raw, err := os.ReadFile(candidate.path)
		if err != nil {
			continue
		}
		content := strings.TrimSpace(string(raw))
		if content == "" {
			continue
		}
		if limit > 0 && len(content) > limit {
			content = content[:limit] + "\n[truncated]"
		}
		blocks = append(blocks, contextBlock{
			Name:    candidate.name,
			Path:    candidate.path,
			Content: content,
		})
	}
	return blocks
}

// assembleSystemPrompt 只根据 promptContext 组装文本，便于缓存和测试。
func assembleSystemPrompt(ctx promptContext) string {
	var sections []string
	sections = append(sections,
		fmt.Sprintf("You are a coding agent working in %s.", ctx.Workdir),
		fmt.Sprintf("Model: %s.", ctx.Model),
		"Use explicit runtime state. Do not introduce package-level mutable state for agent behavior.",
		"Available tools: "+strings.Join(ctx.ToolNames, ", "),
		"Skills available:\n"+listSkills(ctx.Skills),
	)

	if len(ctx.ProjectBlocks) > 0 {
		var project []string
		for _, block := range ctx.ProjectBlocks {
			project = append(project, fmt.Sprintf("## %s\nSource: %s\n%s", block.Name, block.Path, block.Content))
		}
		sections = append(sections, "Project context:\n"+strings.Join(project, "\n\n"))
	}

	if len(ctx.MemoryBlocks) > 0 {
		var memories []string
		for _, block := range ctx.MemoryBlocks {
			memories = append(memories, fmt.Sprintf("## %s\nSource: %s\n%s", block.Name, block.Path, block.Content))
		}
		sections = append(sections, "Memory sections:\n"+strings.Join(memories, "\n\n"))
	}

	sections = append(sections, "Use load_skill to get full skill details when needed.")
	return strings.Join(sections, "\n\n")
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
	rt.emitLine("[assembled] system prompt sections: base, tools, skills, project=%d, memory=%d", len(ctx.ProjectBlocks), len(ctx.MemoryBlocks))
	return prompt
}
