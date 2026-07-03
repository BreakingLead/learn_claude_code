package agent

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SkillInfo 保存扫描到的技能信息
type SkillInfo struct {
	Name        string
	Description string
	Content     string // SKILL.md 原始内容
}

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

// scanSkills 扫描 .agents/skills/ 目录，发现所有可用技能。
func (rt *agentRuntime) scanSkills() map[string]SkillInfo {
	found := make(map[string]SkillInfo)
	skillsDir := filepath.Join(rt.config.Workdir, ".agents", "skills")

	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return found // 目录不存在时返回空
	}

	// 排序以保证稳定输出
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		manifest := filepath.Join(skillsDir, entry.Name(), "SKILL.md")
		raw, err := os.ReadFile(manifest)
		if err != nil {
			continue
		}

		content := string(raw)
		meta, _ := parseFrontmatter(content)

		name := meta["name"]
		if name == "" {
			name = entry.Name()
		}
		desc := meta["description"]
		if desc == "" {
			// 取正文第一行作为描述
			lines := strings.SplitN(content, "\n", 2)
			desc = strings.TrimLeft(lines[0], "# ")
		}

		found[name] = SkillInfo{Name: name, Description: desc, Content: content}
	}
	return found
}

// parseFrontmatter 解析 SKILL.md 中的 YAML frontmatter（简化实现）
func parseFrontmatter(raw string) (map[string]string, string) {
	lines := strings.Split(raw, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil, raw
	}

	endIdx := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			endIdx = i
			break
		}
	}
	if endIdx < 0 {
		return nil, raw
	}

	meta := make(map[string]string)
	for i := 1; i < endIdx; i++ {
		line := lines[i]
		if !strings.Contains(line, ":") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		key := strings.TrimSpace(parts[0])
		value := strings.Trim(strings.TrimSpace(parts[1]), "\"'")
		meta[key] = value
	}

	body := strings.Join(lines[endIdx+1:], "\n")
	return meta, body
}

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

func (rt *agentRuntime) loadMemoryBlocks() []contextBlock {
	candidates := []struct {
		name string
		path string
	}{
		{name: "Memory", path: rt.config.MemoryIndex},
	}
	return rt.readContextBlocks(candidates, 6000)
}

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

// loadSkill 加载指定技能的完整内容
func (rt *agentRuntime) loadSkill(raw json.RawMessage) string {
	var input struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	skills := rt.scanSkills()
	skill, ok := skills[input.Name]
	if !ok {
		return fmt.Sprintf("Skill not found: %s", input.Name)
	}
	return skill.Content
}
