package agent

import (
	"flag"
	"io"
	"time"
)

type RunMode string

const (
	RunModeTUI        RunMode = "tui"
	RunModeNodeEditor RunMode = "node-editor"
	RunModeTelegram   RunMode = "telegram"
)

type RunOptions struct {
	RunMode       RunMode
	APIKey        string
	BaseURL       string
	Model         string
	FallbackModel string

	Mode            string
	UseBlueprint    bool
	BlueprintID     string
	BlueprintPath   string
	DisabledModules string
	ResumePrompt    bool

	NodeEditorAddr string

	TelegramToken        string
	TelegramBaseURL      string
	TelegramAllowedChats string
	TelegramPollInterval time.Duration
	TelegramTimeout      time.Duration
}

func DefaultRunOptions() RunOptions {
	return RunOptions{
		RunMode:              RunModeTUI,
		Model:                "deepseek-v4-flash",
		FallbackModel:        "deepseek-v4-flash",
		BlueprintID:          "default",
		ResumePrompt:         true,
		NodeEditorAddr:       "127.0.0.1:8787",
		TelegramBaseURL:      "https://api.telegram.org",
		TelegramPollInterval: 2 * time.Second,
		TelegramTimeout:      30 * time.Second,
	}
}

func ParseRunOptions(args []string, output io.Writer) (RunOptions, error) {
	options := DefaultRunOptions()
	flags := flag.NewFlagSet("bee-agent", flag.ContinueOnError)
	if output != nil {
		flags.SetOutput(output)
	}

	mode := string(options.RunMode)
	flags.StringVar(&mode, "run-mode", mode, "runtime entrypoint: tui, node-editor, or telegram")
	flags.StringVar(&options.APIKey, "api-key", options.APIKey, "Anthropic API key")
	flags.StringVar(&options.BaseURL, "base-url", options.BaseURL, "Anthropic API base URL")
	flags.StringVar(&options.Model, "model", options.Model, "primary model")
	flags.StringVar(&options.FallbackModel, "fallback-model", options.FallbackModel, "fallback model")

	flags.StringVar(&options.Mode, "mode", options.Mode, "Bee Agent mode name")
	flags.BoolVar(&options.UseBlueprint, "use-blueprint", options.UseBlueprint, "load tools and prompt blocks from an Agent Blueprint")
	flags.StringVar(&options.BlueprintID, "blueprint-id", options.BlueprintID, "Agent Blueprint id under .agents/blueprints/agents")
	flags.StringVar(&options.BlueprintPath, "blueprint-path", options.BlueprintPath, "explicit Agent Blueprint JSON path")
	flags.StringVar(&options.DisabledModules, "disable-modules", options.DisabledModules, "comma-separated module ids to disable")
	flags.BoolVar(&options.ResumePrompt, "resume-prompt", options.ResumePrompt, "prompt to resume an existing session on TUI startup")

	flags.StringVar(&options.NodeEditorAddr, "node-editor-addr", options.NodeEditorAddr, "Node Editor listen address")

	flags.StringVar(&options.TelegramToken, "telegram-token", options.TelegramToken, "Telegram bot token")
	flags.StringVar(&options.TelegramBaseURL, "telegram-base-url", options.TelegramBaseURL, "Telegram API base URL")
	flags.StringVar(&options.TelegramAllowedChats, "telegram-allowed-chats", options.TelegramAllowedChats, "comma-separated Telegram chat id allow list")
	flags.DurationVar(&options.TelegramPollInterval, "telegram-poll-interval", options.TelegramPollInterval, "Telegram polling interval")
	flags.DurationVar(&options.TelegramTimeout, "telegram-timeout", options.TelegramTimeout, "Telegram long polling timeout")

	if err := flags.Parse(args); err != nil {
		return RunOptions{}, err
	}
	options.RunMode = RunMode(mode)
	return options, nil
}
