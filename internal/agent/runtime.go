package agent

import (
	"fmt"
	"os"
	"path/filepath"
)

type uiEventKind int

const (
	uiEventLog uiEventKind = iota
	uiEventApproval
)

type uiEvent struct {
	Kind uiEventKind
	Text string
}

type agentRuntime struct {
	config          agentConfig
	hooks           map[HookEvent][]HookCallback
	events          chan<- uiEvent
	approvals       <-chan bool
	currentTodos    []Todo
	roundsSinceTodo int
	promptCache     promptCache
	memoryTurns     int
}

type agentConfig struct {
	Model          string
	Workdir        string
	CompactDir     string
	ToolResultsDir string
	TranscriptDir  string
	MemoryDir      string
	MemoryIndex    string
}

type promptCache struct {
	contextKey string
	prompt     string
}

func newAgentConfig() (agentConfig, error) {
	workdir, err := os.Getwd()
	if err != nil {
		return agentConfig{}, err
	}
	compactDir := filepath.Join(workdir, ".agent_state", "compact")
	return agentConfig{
		Model:          getEnvOr("MODEL", "deepseek-v4-flash"),
		Workdir:        workdir,
		CompactDir:     compactDir,
		ToolResultsDir: filepath.Join(compactDir, "tool_results"),
		TranscriptDir:  filepath.Join(compactDir, "transcripts"),
		MemoryDir:      filepath.Join(workdir, ".memory"),
		MemoryIndex:    filepath.Join(workdir, ".memory", "MEMORY.md"),
	}, nil
}

func newAgentRuntime(config agentConfig, events chan<- uiEvent, approvals <-chan bool) *agentRuntime {
	rt := &agentRuntime{
		config: config,
		hooks: map[HookEvent][]HookCallback{
			EventUserPromptSubmit: {},
			EventPreToolUse:       {},
			EventPostToolUse:      {},
			EventStop:             {},
		},
		events:    events,
		approvals: approvals,
	}
	rt.initHooks()
	return rt
}

// emitLine 是运行时日志出口；显式持有 UI 通道，避免包级全局变量。
func (rt *agentRuntime) emitLine(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	if rt == nil || rt.events == nil {
		fmt.Println(line)
		return
	}
	rt.events <- uiEvent{Kind: uiEventLog, Text: line}
}

// requestApproval 把权限确认纳入运行时状态机；没有 UI 时回退到传统 stdin。
func (rt *agentRuntime) requestApproval(text string) (bool, bool) {
	if rt == nil || rt.events == nil || rt.approvals == nil {
		return false, false
	}
	rt.events <- uiEvent{Kind: uiEventApproval, Text: text}
	answer, ok := <-rt.approvals
	return answer, ok
}

// safePath 将相对路径拼接到工作区后 resolve，并校验不会逃逸工作区。
func (rt *agentRuntime) safePath(p string) (string, error) {
	abs := filepath.Join(rt.config.Workdir, p)
	resolved, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		// 目录可能不存在（如 write_file 创建新文件），退化到 Clean。
		resolved = filepath.Clean(abs)
	} else {
		resolved = filepath.Join(resolved, filepath.Base(abs))
	}
	rel, err := filepath.Rel(rt.config.Workdir, resolved)
	if err != nil || len(rel) >= 2 && rel[:2] == ".." {
		return "", fmt.Errorf("路径逃逸工作区: %s", p)
	}
	return resolved, nil
}
