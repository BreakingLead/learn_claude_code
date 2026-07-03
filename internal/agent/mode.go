package agent

// mode 负责定义“同一个 agent 在不同工作方式下的提示词和工具边界”。
// 内置 plan/build 永远可用；.agents/modes.json 可以新增或覆盖 mode。

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

const (
	defaultModeName = "build"
	planModeName    = "plan"
)

type agentMode struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Prompt       string   `json:"prompt"`
	Tools        []string `json:"tools"`
	DisableTools []string `json:"disable_tools"`
}

type modeConfigFile struct {
	Default string      `json:"default"`
	Modes   []agentMode `json:"modes"`
}

type modeRegistry struct {
	modes  map[string]agentMode
	order  []string
	active string
	path   string
	log    func(format string, args ...any)
}

func newModeRegistry(path string, requested string, log func(format string, args ...any)) *modeRegistry {
	registry := &modeRegistry{
		modes: map[string]agentMode{},
		order: []string{},
		path:  path,
		log:   log,
	}
	registry.register(builtinBuildMode())
	registry.register(builtinPlanMode())

	defaultName := defaultModeName
	if configDefault := registry.loadUserModes(path); configDefault != "" {
		defaultName = configDefault
	}
	if strings.TrimSpace(requested) != "" {
		defaultName = requested
	}
	if err := registry.setActive(defaultName); err != nil {
		registry.active = defaultModeName
		registry.emit("[mode] %v; fallback to %s", err, defaultModeName)
	}
	return registry
}

func builtinBuildMode() agentMode {
	return agentMode{
		Name:        defaultModeName,
		Description: "完整构建模式，可以读写文件并执行已启用模块工具。",
		Prompt: "You are in build mode. Implement requested changes directly when the user asks for code changes. " +
			"Use tools pragmatically, verify focused changes, and keep edits scoped.",
	}
}

func builtinPlanMode() agentMode {
	return agentMode{
		Name:        planModeName,
		Description: "规划模式，不写文件，倾向先分析方案和步骤。",
		Prompt: "You are in plan mode. Do not modify files or persistent project state. " +
			"Focus on understanding the request, inspecting read-only context, and producing a concrete plan before implementation.",
		Tools: []string{"read_file", "glob", "load_skill", "todo_write"},
	}
}

func (r *modeRegistry) loadUserModes(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			r.emit("[mode] load %s: %v", path, err)
		}
		return ""
	}
	var config modeConfigFile
	if err := json.Unmarshal(raw, &config); err != nil {
		r.emit("[mode] parse %s: %v", path, err)
		return ""
	}
	for _, mode := range config.Modes {
		r.register(mode)
	}
	return normalizeModeName(config.Default)
}

func (r *modeRegistry) register(mode agentMode) {
	mode.Name = normalizeModeName(mode.Name)
	if mode.Name == "" {
		return
	}
	mode.Tools = normalizeToolNames(mode.Tools)
	mode.DisableTools = normalizeToolNames(mode.DisableTools)
	if _, ok := r.modes[mode.Name]; !ok {
		r.order = append(r.order, mode.Name)
		sort.Strings(r.order)
	}
	r.modes[mode.Name] = mode
}

func (r *modeRegistry) setActive(name string) error {
	name = normalizeModeName(name)
	if name == "" {
		return fmt.Errorf("mode name is required")
	}
	if _, ok := r.modes[name]; !ok {
		return fmt.Errorf("unknown mode %q", name)
	}
	r.active = name
	return nil
}

func (r *modeRegistry) activeMode() agentMode {
	if r == nil {
		return builtinBuildMode()
	}
	if mode, ok := r.modes[r.active]; ok {
		return mode
	}
	return r.modes[defaultModeName]
}

func (r *modeRegistry) names() []string {
	if r == nil {
		return []string{defaultModeName, planModeName}
	}
	return append([]string(nil), r.order...)
}

func (r *modeRegistry) listText() string {
	if r == nil {
		return "No modes registered."
	}
	lines := []string{"可用 mode："}
	for _, name := range r.order {
		mode := r.modes[name]
		marker := " "
		if name == r.active {
			marker = "*"
		}
		lines = append(lines, fmt.Sprintf("%s %-10s %s", marker, name, mode.Description))
	}
	lines = append(lines, "用法：/mode plan 或 /mode build")
	return strings.Join(lines, "\n")
}

func (r *modeRegistry) filterTools(all []string) []string {
	mode := r.activeMode()
	allowed := map[string]bool{}
	if len(mode.Tools) == 0 {
		for _, name := range all {
			allowed[name] = true
		}
	} else {
		allSet := map[string]bool{}
		for _, name := range all {
			allSet[name] = true
		}
		for _, name := range mode.Tools {
			if allSet[name] {
				allowed[name] = true
			}
		}
	}
	for _, name := range mode.DisableTools {
		delete(allowed, name)
	}
	filtered := make([]string, 0, len(all))
	for _, name := range all {
		if allowed[name] {
			filtered = append(filtered, name)
		}
	}
	return filtered
}

func (r *modeRegistry) snapshot() any {
	if r == nil {
		return nil
	}
	return map[string]any{
		"active": r.active,
		"path":   r.path,
		"modes":  r.names(),
	}
}

func (r *modeRegistry) emit(format string, args ...any) {
	if r != nil && r.log != nil {
		r.log(format, args...)
	}
}

func normalizeModeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func normalizeToolNames(names []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		result = append(result, name)
	}
	return result
}

func (rt *agentRuntime) switchMode(name string) error {
	if rt == nil || rt.modes == nil {
		return fmt.Errorf("mode registry is not initialized")
	}
	if err := rt.modes.setActive(name); err != nil {
		return err
	}
	rt.promptCache = promptCache{}
	return nil
}

func (rt *agentRuntime) activeMode() agentMode {
	if rt == nil || rt.modes == nil {
		return builtinBuildMode()
	}
	return rt.modes.activeMode()
}

func (rt *agentRuntime) modeToolNames(all []string) []string {
	if rt == nil || rt.modes == nil {
		return all
	}
	return rt.modes.filterTools(all)
}
