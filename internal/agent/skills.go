package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

// SkillInfo 保存扫描到的技能信息
type SkillInfo struct {
	Name        string
	Description string
	Content     string // SKILL.md 原始内容
}

type skillModule struct {
	rt      *agentRuntime
	workdir string
}

// ID 返回技能模块标识。
func (m *skillModule) ID() string {
	return "skills"
}

// Init 保存技能扫描所需的工作区路径。
func (m *skillModule) Init(ctx ModuleContext) error {
	m.workdir = ctx.Workdir
	return nil
}

// PromptBlocks 把技能目录作为模块 prompt block 贡献给 system prompt。
func (m *skillModule) PromptBlocks(ctx context.Context, req PromptRequest) ([]PromptBlock, error) {
	if m.rt == nil {
		return nil, nil
	}
	content := "Skills available:\n" + listSkills(sortedSkills(m.rt.scanSkills()))
	if hasString(req.ToolNames, "load_skill") {
		content += "\n\nUse load_skill to get full skill details when needed."
	}
	return []PromptBlock{
		{
			Module:  m.ID(),
			Name:    "Skill Catalog",
			Source:  filepath.Join(m.workdir, ".agents", "skills"),
			Content: content,
		},
	}, nil
}

// ToolDefinitions 注册技能加载工具。
func (m *skillModule) ToolDefinitions() []anthropic.ToolParam {
	return skillToolDefinitions()
}

// ToolHandlers 绑定技能加载工具到当前 runtime。
func (m *skillModule) ToolHandlers() map[string]ToolHandler {
	if m.rt == nil {
		return map[string]ToolHandler{}
	}
	return m.rt.skillToolHandlers()
}

// RuntimeSnapshot 暴露已发现技能的摘要信息。
func (m *skillModule) RuntimeSnapshot() any {
	if m.rt == nil {
		return nil
	}
	skills := sortedSkills(m.rt.scanSkills())
	names := make([]string, 0, len(skills))
	for _, skill := range skills {
		names = append(names, skill.Name)
	}
	return map[string]any{
		"count": len(skills),
		"names": names,
	}
}

func (rt *agentRuntime) skillToolHandlers() map[string]ToolHandler {
	return map[string]ToolHandler{
		"load_skill": rt.loadSkill,
	}
}

func skillToolDefinitions() []anthropic.ToolParam {
	return []anthropic.ToolParam{
		{
			Name:        "load_skill",
			Description: anthropic.String("Load the full instructions for an available skill by name."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]any{
					"name": map[string]any{"type": "string"},
				},
				Required: []string{"name"},
			},
		},
	}
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
