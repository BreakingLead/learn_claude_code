package agent

import (
	nodeeditor "bee_agent/internal/agent/node_editor"
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/joho/godotenv"
)

// agentLoop 是主 agent 的薄包装；具体执行状态机由 runAgent 统一负责。
func (rt *agentRuntime) agentLoop(ctx context.Context, client anthropic.Client, messages *[]anthropic.MessageParam) {
	rt.injectScheduledCronMessages(messages)
	rt.runAgent(ctx, client, rt.mainAgentSpec(), messages)
}

// ── REPL 入口 ──────────────────────────────────────────

func Run() {
	godotenv.Load()

	ctx := context.Background()
	if truthyEnv("BEE_AGENT_NODE_EDITOR") {
		runNodeEditor()
		return
	}
	// 普通 TUI 首次启动时先补齐必要配置，再创建客户端和运行时。
	if !truthyEnv("BEE_AGENT_TELEGRAM") {
		result, err := runStartupConfigIfNeeded()
		if err != nil {
			fmt.Println(colorRed("Startup config error: " + err.Error()))
			return
		}
		if result.Cancelled {
			fmt.Println("Startup cancelled.")
			return
		}
	}

	client := newAnthropicClientFromEnv()
	if truthyEnv("BEE_AGENT_TELEGRAM") {
		runTelegram(ctx, client)
		return
	}

	runTUI(ctx, client)
}

func newAnthropicClientFromEnv() anthropic.Client {
	opts := []option.RequestOption{}
	if base := os.Getenv("ANTHROPIC_BASE_URL"); base != "" {
		opts = append(opts, option.WithBaseURL(base))
	}
	return anthropic.NewClient(opts...)
}

func runNodeEditor() {
	config, err := newAgentConfig()
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
	addr := getEnvOr("BEE_AGENT_NODE_EDITOR_ADDR", "127.0.0.1:8787")
	fmt.Printf("Bee Agent Builder: http://%s\n", addr)
	if err := http.ListenAndServe(addr, nodeeditor.NewServer(config.Workdir).Handler()); err != nil {
		fmt.Println(colorRed("Node editor error: " + err.Error()))
	}
}
