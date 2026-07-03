package agent

import (
	"context"
	"os"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/joho/godotenv"
)

// agentLoop 是主 agent 的薄包装；具体执行状态机由 runAgent 统一负责。
func (rt *agentRuntime) agentLoop(ctx context.Context, client anthropic.Client, messages *[]anthropic.MessageParam) {
	rt.runAgent(ctx, client, rt.mainAgentSpec(), messages)
}

// ── REPL 入口 ──────────────────────────────────────────

func Run() {
	godotenv.Load()

	// 创建 Anthropic 客户端
	opts := []option.RequestOption{}
	if base := os.Getenv("ANTHROPIC_BASE_URL"); base != "" {
		opts = append(opts, option.WithBaseURL(base))
	}
	client := anthropic.NewClient(opts...)
	ctx := context.Background()

	runTUI(ctx, client)
}
