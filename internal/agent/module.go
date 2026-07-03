package agent

// 模块说明：
// 这个文件定义 agent 内部模块的共通 API。模块仍是编译进二进制的 Go 代码，
// 但通过小接口按能力接入 prompt、工具或 turn hook，避免 runtime 直接知道每个系统细节。

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

// Module 是所有内部模块的最小身份和初始化接口。
type Module interface {
	ID() string
	Init(ctx ModuleContext) error
}

// PromptContributor 表示模块可以向 system prompt 贡献上下文块。
type PromptContributor interface {
	PromptBlocks(ctx context.Context, req PromptRequest) ([]PromptBlock, error)
}

// ToolContributor 表示模块可以注册工具 schema 和对应 handler。
type ToolContributor interface {
	ToolDefinitions() []anthropic.ToolParam
	ToolHandlers() map[string]ToolHandler
}

// RuntimeSnapshotContributor 表示模块可以向 Debug tab 暴露可 JSON 化的运行时状态。
type RuntimeSnapshotContributor interface {
	RuntimeSnapshot() any
}

// TurnContributor 表示模块可以在模型调用前或工具调用后更新状态/注入消息。
type TurnContributor interface {
	BeforeModel(ctx context.Context, req TurnRequest) []anthropic.MessageParam
	AfterToolUse(ctx context.Context, event ToolUseEvent)
	AfterToolRound(ctx context.Context, event ToolRoundEvent)
}

// ModuleContext 是 runtime 显式传给模块的初始化上下文。
type ModuleContext struct {
	Workdir string
	Config  agentConfig
	Log     func(format string, args ...any)
}

// PromptRequest 保存一次 system prompt 组装时传给模块的请求信息。
type PromptRequest struct {
	ToolNames []string
}

// PromptBlock 是 system prompt 的统一上下文片段。
type PromptBlock struct {
	Module  string
	Name    string
	Source  string
	Content string
}

// TurnRequest 是一次模型调用前传给模块的只读上下文。
type TurnRequest struct {
	AgentID   string
	ToolNames []string
	Messages  []anthropic.MessageParam
}

// ToolUseEvent 是工具执行后传给模块的事件。
type ToolUseEvent struct {
	AgentID string
	Name    string
	Output  string
}

// ToolRoundEvent 是一轮 tool_use 全部执行完成后的事件。
type ToolRoundEvent struct {
	AgentID   string
	ToolNames []string
}

type moduleManager struct {
	modules []Module
}

type promptFileCandidate struct {
	name   string
	path   string
	module string
}

// newModuleManager 创建模块管理器，模块顺序决定 prompt block 的稳定输出顺序。
func newModuleManager(modules ...Module) *moduleManager {
	return &moduleManager{modules: modules}
}

// init 显式初始化所有模块。
func (m *moduleManager) init(ctx ModuleContext) error {
	for _, module := range m.modules {
		if err := module.Init(ctx); err != nil {
			return fmt.Errorf("init module %s: %w", module.ID(), err)
		}
	}
	return nil
}

// promptBlocks 收集所有实现 PromptContributor 的模块上下文块。
func (m *moduleManager) promptBlocks(ctx context.Context, req PromptRequest) []PromptBlock {
	var blocks []PromptBlock
	for _, module := range m.modules {
		contributor, ok := module.(PromptContributor)
		if !ok {
			continue
		}
		moduleBlocks, err := contributor.PromptBlocks(ctx, req)
		if err != nil {
			continue
		}
		blocks = append(blocks, moduleBlocks...)
	}
	return blocks
}

// toolDefinitions 收集所有模块贡献的工具 schema。
func (m *moduleManager) toolDefinitions() []anthropic.ToolParam {
	var tools []anthropic.ToolParam
	for _, module := range m.modules {
		contributor, ok := module.(ToolContributor)
		if !ok {
			continue
		}
		tools = append(tools, contributor.ToolDefinitions()...)
	}
	return tools
}

// toolHandlers 收集所有模块贡献的工具 handler。
func (m *moduleManager) toolHandlers() map[string]ToolHandler {
	handlers := map[string]ToolHandler{}
	for _, module := range m.modules {
		contributor, ok := module.(ToolContributor)
		if !ok {
			continue
		}
		for name, handler := range contributor.ToolHandlers() {
			handlers[name] = handler
		}
	}
	return handlers
}

// moduleIDs 返回当前启用模块的稳定顺序。
func (m *moduleManager) moduleIDs() []string {
	ids := make([]string, 0, len(m.modules))
	for _, module := range m.modules {
		ids = append(ids, module.ID())
	}
	return ids
}

// runtimeSnapshots 收集模块自报状态，Debug tab 不再直接读取每个模块内部字段。
func (m *moduleManager) runtimeSnapshots() map[string]any {
	snapshots := map[string]any{}
	for _, module := range m.modules {
		contributor, ok := module.(RuntimeSnapshotContributor)
		if !ok {
			continue
		}
		snapshots[module.ID()] = contributor.RuntimeSnapshot()
	}
	return snapshots
}

// beforeModel 让模块在模型调用前注入内部消息。
func (m *moduleManager) beforeModel(ctx context.Context, req TurnRequest) []anthropic.MessageParam {
	var messages []anthropic.MessageParam
	for _, module := range m.modules {
		contributor, ok := module.(TurnContributor)
		if !ok {
			continue
		}
		messages = append(messages, contributor.BeforeModel(ctx, req)...)
	}
	return messages
}

// afterToolUse 通知模块工具执行结果，用于更新模块内部状态。
func (m *moduleManager) afterToolUse(ctx context.Context, event ToolUseEvent) {
	for _, module := range m.modules {
		contributor, ok := module.(TurnContributor)
		if !ok {
			continue
		}
		contributor.AfterToolUse(ctx, event)
	}
}

// afterToolRound 通知模块本轮 tool_use 已全部执行完成。
func (m *moduleManager) afterToolRound(ctx context.Context, event ToolRoundEvent) {
	for _, module := range m.modules {
		contributor, ok := module.(TurnContributor)
		if !ok {
			continue
		}
		contributor.AfterToolRound(ctx, event)
	}
}

type projectContextModule struct {
	workdir   string
	taskIndex string
}

// ID 返回项目上下文模块标识。
func (m *projectContextModule) ID() string {
	return "project"
}

// Init 保存项目上下文模块需要的路径配置。
func (m *projectContextModule) Init(ctx ModuleContext) error {
	m.workdir = ctx.Workdir
	m.taskIndex = ctx.Config.TaskIndex
	return nil
}

// PromptBlocks 读取项目说明、README 和任务索引。
func (m *projectContextModule) PromptBlocks(ctx context.Context, req PromptRequest) ([]PromptBlock, error) {
	candidates := []promptFileCandidate{
		{module: m.ID(), name: "Repository Guidelines", path: filepath.Join(m.workdir, "AGENTS.md")},
		{module: m.ID(), name: "Project README", path: filepath.Join(m.workdir, "README.md")},
		{module: m.ID(), name: "Task Index", path: m.taskIndex},
	}
	return readPromptFiles(candidates, 6000), nil
}

// RuntimeSnapshot 返回项目上下文模块读取的主要文件位置。
func (m *projectContextModule) RuntimeSnapshot() any {
	return map[string]any{
		"workdir":   m.workdir,
		"taskIndex": m.taskIndex,
	}
}

type memoryContextModule struct {
	memoryIndex string
}

// ID 返回持久记忆上下文模块标识。
func (m *memoryContextModule) ID() string {
	return "memory"
}

// Init 保存持久记忆模块需要的索引路径。
func (m *memoryContextModule) Init(ctx ModuleContext) error {
	m.memoryIndex = ctx.Config.MemoryIndex
	return nil
}

// PromptBlocks 读取持久记忆索引。
func (m *memoryContextModule) PromptBlocks(ctx context.Context, req PromptRequest) ([]PromptBlock, error) {
	candidates := []promptFileCandidate{
		{module: m.ID(), name: "Memory", path: m.memoryIndex},
	}
	return readPromptFiles(candidates, 6000), nil
}

// RuntimeSnapshot 返回记忆索引路径；具体记忆提取仍由 memory.go 负责。
func (m *memoryContextModule) RuntimeSnapshot() any {
	return map[string]any{
		"memoryIndex": m.memoryIndex,
	}
}

// readPromptFiles 读取一组候选文件，并按字符上限裁剪单块内容。
func readPromptFiles(candidates []promptFileCandidate, limit int) []PromptBlock {
	var blocks []PromptBlock
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
		blocks = append(blocks, PromptBlock{
			Module:  candidate.module,
			Name:    candidate.name,
			Source:  candidate.path,
			Content: content,
		})
	}
	return blocks
}
