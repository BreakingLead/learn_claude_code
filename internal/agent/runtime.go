package agent

import (
	nodeeditor "bee_agent/internal/agent/node_editor"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type uiEventKind int

const (
	uiEventLog uiEventKind = iota
	uiEventApproval
	uiEventCronQueued
)

type uiEvent struct {
	Kind uiEventKind
	Text string
}

type agentRuntime struct {
	config      agentConfig
	hooks       map[HookEvent][]HookCallback
	events      chan<- uiEvent
	approvals   <-chan bool
	modules     *moduleManager
	todo        *todoModule
	background  *backgroundRegistry
	cron        *cronScheduler
	team        *teamRegistry
	modes       *modeRegistry
	sessions    *sessionStore
	blueprint   *runtimeBlueprint
	sessionID   string
	promptCache promptCache
	memoryTurns int
	recovery    recoveryState
}

type agentConfig struct {
	APIKey                string
	BaseURL               string
	Model                 string
	FallbackModel         string
	Workdir               string
	CompactDir            string
	ToolResultsDir        string
	TranscriptDir         string
	MemoryDir             string
	MemoryIndex           string
	TaskDir               string
	TaskIndex             string
	ScheduledTasksPath    string
	TeamDir               string
	TeamMessagesPath      string
	SessionDir            string
	DefaultBlueprintPath  string
	BlueprintPath         string
	UseBlueprint          bool
	ModeConfigPath        string
	Mode                  string
	ResumePrompt          bool
	DefaultTokens         int64
	EscalatedTokens       int64
	MaxRecoveryRetries    int
	MaxTokenContinuations int
	RetryBaseDelay        time.Duration
	RetryMaxDelay         time.Duration
	BackgroundTimeout     time.Duration
	DisabledModules       map[string]bool
}

type promptCache struct {
	contextKey string
	prompt     string
}

func newAgentConfig(options RunOptions) (agentConfig, error) {
	workdir, err := os.Getwd()
	if err != nil {
		return agentConfig{}, err
	}
	compactDir := filepath.Join(workdir, ".agents", "compact")
	return agentConfig{
		APIKey:                strings.TrimSpace(options.APIKey),
		BaseURL:               strings.TrimSpace(options.BaseURL),
		Model:                 firstNonEmpty(options.Model, "deepseek-v4-flash"),
		FallbackModel:         firstNonEmpty(options.FallbackModel, options.Model, "deepseek-v4-flash"),
		Workdir:               workdir,
		CompactDir:            compactDir,
		ToolResultsDir:        filepath.Join(compactDir, "tool_results"),
		TranscriptDir:         filepath.Join(compactDir, "transcripts"),
		MemoryDir:             filepath.Join(workdir, ".agents", ".memory"),
		MemoryIndex:           filepath.Join(workdir, ".agents", ".memory", "MEMORY.md"),
		TaskDir:               filepath.Join(workdir, ".agents", ".tasks"),
		TaskIndex:             filepath.Join(workdir, ".agents", ".tasks", "TASKS.md"),
		ScheduledTasksPath:    filepath.Join(workdir, ".scheduled_tasks.json"),
		TeamDir:               filepath.Join(workdir, ".agents", "team"),
		TeamMessagesPath:      filepath.Join(workdir, ".agents", "team", "messages.jsonl"),
		SessionDir:            filepath.Join(workdir, ".agents", "sessions"),
		DefaultBlueprintPath:  nodeeditor.DefaultBlueprintPath(workdir),
		BlueprintPath:         blueprintPathFromOptions(workdir, options),
		UseBlueprint:          options.UseBlueprint,
		ModeConfigPath:        filepath.Join(workdir, ".agents", "modes.json"),
		Mode:                  strings.TrimSpace(options.Mode),
		ResumePrompt:          options.ResumePrompt,
		DefaultTokens:         8000,
		EscalatedTokens:       16000,
		MaxRecoveryRetries:    3,
		MaxTokenContinuations: 1,
		RetryBaseDelay:        500 * time.Millisecond,
		RetryMaxDelay:         8 * time.Second,
		BackgroundTimeout:     10 * time.Minute,
		DisabledModules:       parseDisabledModules(options.DisabledModules),
	}, nil
}

func blueprintPathFromOptions(workdir string, options RunOptions) string {
	if path := strings.TrimSpace(options.BlueprintPath); path != "" {
		if filepath.IsAbs(path) {
			return path
		}
		return filepath.Join(workdir, path)
	}
	id := strings.TrimSpace(options.BlueprintID)
	if id == "" {
		id = "default"
	}
	id = safeFileID(id)
	if id == "" {
		id = "default"
	}
	return filepath.Join(workdir, ".agents", "blueprints", "agents", id+".json")
}

func safeFileID(id string) string {
	id = strings.TrimSpace(id)
	var b strings.Builder
	for _, r := range id {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
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
	if rt.moduleEnabled("background") {
		rt.background = newBackgroundRegistry(rt.emitLine)
	}
	if rt.moduleEnabled("cron") {
		rt.cron = newCronScheduler(config.ScheduledTasksPath, rt.emitLine, rt.notifyCronQueued)
		if err := rt.cron.loadDurableJobs(); err != nil {
			rt.emitLine("[cron] load durable jobs: %v", err)
		}
	}
	if rt.moduleEnabled("team") {
		rt.team = newTeamRegistry(config.TeamMessagesPath, rt.emitLine)
	}
	rt.recovery = newRecoveryState(config)
	if _, err := nodeeditor.EnsureDefaultBlueprint(config.DefaultBlueprintPath); err != nil {
		rt.emitLine("[blueprint] default blueprint: %v", err)
	}
	rt.modes = newModeRegistry(config.ModeConfigPath, config.Mode, rt.emitLine)
	rt.blueprint = rt.loadRuntimeBlueprint()
	rt.sessions = newSessionStore(config.SessionDir)
	rt.sessionID = rt.sessions.newID(time.Now())
	rt.todo = &todoModule{}
	rt.modules = newModuleManager(rt.configuredModules()...)
	if err := rt.modules.init(ModuleContext{
		Workdir: config.Workdir,
		Config:  config,
		Log:     rt.emitLine,
	}); err != nil {
		rt.emitLine("[module] %v", err)
	}
	rt.initHooks()
	return rt
}

func parseDisabledModules(raw string) map[string]bool {
	disabled := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		name := strings.ToLower(strings.TrimSpace(part))
		if name == "" {
			continue
		}
		disabled[name] = true
	}
	return disabled
}

func (rt *agentRuntime) moduleEnabled(id string) bool {
	if rt == nil {
		return false
	}
	return !rt.config.DisabledModules[id]
}

func (rt *agentRuntime) configuredModules() []Module {
	candidates := []Module{
		&projectContextModule{},
		&skillModule{rt: rt},
		rt.todo,
		&memoryContextModule{},
		&messagingModule{},
		&cocModule{},
		&subagentModule{rt: rt},
		&taskSystemModule{rt: rt},
		&teamModule{rt: rt},
		&backgroundModule{rt: rt},
		&cronModule{rt: rt},
	}
	modules := make([]Module, 0, len(candidates))
	for _, module := range candidates {
		if rt.moduleEnabled(module.ID()) {
			modules = append(modules, module)
		}
	}
	return modules
}

func (rt *agentRuntime) startCronScheduler() {
	if rt != nil && rt.cron != nil {
		rt.cron.start()
	}
}

func (rt *agentRuntime) notifyCronQueued(count int) {
	if count <= 0 {
		return
	}
	text := fmt.Sprintf("[cron] queued %d scheduled task(s)", count)
	if rt == nil || rt.events == nil {
		fmt.Println(text)
		return
	}
	rt.events <- uiEvent{Kind: uiEventCronQueued, Text: text}
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
