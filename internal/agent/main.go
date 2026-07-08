package agent

import (
	nodeeditor "bee_agent/internal/agent/node_editor"
	"context"
	"fmt"
	"net/http"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// agentLoop 是主 agent 的薄包装；具体执行状态机由 runAgent 统一负责。
func (rt *agentRuntime) agentLoop(ctx context.Context, client anthropic.Client, messages *[]anthropic.MessageParam) {
	rt.injectScheduledCronMessages(messages)
	rt.runAgent(ctx, client, rt.mainAgentSpec(), messages)
}

// ── REPL 入口 ──────────────────────────────────────────

func Run(options RunOptions) {
	ctx := context.Background()
	if options.RunMode == RunModeNodeEditor {
		runNodeEditor(options)
		return
	}
	// 普通 TUI 首次启动时先补齐必要配置，再创建客户端和运行时。
	if options.RunMode == RunModeTUI {
		next, result, err := runStartupConfigIfNeeded(options)
		if err != nil {
			fmt.Println(colorRed("Startup config error: " + err.Error()))
			return
		}
		if result.Cancelled {
			fmt.Println("Startup cancelled.")
			return
		}
		options = next
	}

	config, err := newAgentConfig(options)
	if err != nil {
		fmt.Println(colorRed("Config error: " + err.Error()))
		return
	}
	client := newAnthropicClient(config)
	switch options.RunMode {
	case RunModeTelegram:
		runTelegram(ctx, client, config, newTelegramConfigFromOptions(options))
	case RunModeTUI:
		runTUI(ctx, client, config)
	default:
		fmt.Println(colorRed("Unknown run mode: " + string(options.RunMode)))
		return
	}
}

func newAnthropicClient(config agentConfig) anthropic.Client {
	opts := []option.RequestOption{}
	if config.APIKey != "" {
		opts = append(opts, option.WithAPIKey(config.APIKey))
	}
	if config.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(config.BaseURL))
	}
	return anthropic.NewClient(opts...)
}

func runNodeEditor(options RunOptions) {
	config, err := newAgentConfig(options)
	if err != nil {
		fmt.Println(colorRed("Config error: " + err.Error()))
		return
	}
	if _, err := nodeeditor.EnsureDefaultBlueprint(config.DefaultBlueprintPath); err != nil {
		fmt.Println(colorRed("Blueprint error: " + err.Error()))
		return
	}
	if _, err := nodeeditor.EnsureDefaultWorkflow(nodeeditor.DefaultWorkflowPath(config.Workdir)); err != nil {
		fmt.Println(colorRed("Workflow error: " + err.Error()))
		return
	}
	if _, err := nodeeditor.EnsureDefaultTimerWorkflow(nodeeditor.DefaultTimerWorkflowPath(config.Workdir)); err != nil {
		fmt.Println(colorRed("Timer workflow error: " + err.Error()))
		return
	}
	addr := options.NodeEditorAddr
	if addr == "" {
		addr = "127.0.0.1:8787"
	}
	fmt.Printf("Bee Agent Builder: http://%s\n", addr)
	client := newAnthropicClient(config)
	server := nodeeditor.NewServerWithPlanExecutorFactory(config.Workdir, newBeeAgentPlanExecutorFactory(config, client))
	if err := http.ListenAndServe(addr, server.Handler()); err != nil {
		fmt.Println(colorRed("Node editor error: " + err.Error()))
	}
}
