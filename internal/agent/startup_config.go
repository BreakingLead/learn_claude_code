package agent

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type startupConfigResult struct {
	Cancelled bool
}

type startupPreset struct {
	Name        string
	Description string
	Model       string
	BaseURL     string
	Custom      bool
}

var startupPresets = []startupPreset{
	{
		Name:        "Bee default",
		Description: "使用项目默认模型 deepseek-v4-flash；base URL 可在下方手动填写。",
		Model:       "deepseek-v4-flash",
	},
	{
		Name:        "Claude Sonnet",
		Description: "使用 Anthropic 官方 Claude Sonnet；base URL 留空。",
		Model:       "claude-sonnet-4-20250514",
	},
	{
		Name:        "Custom",
		Description: "手动输入模型和可选 base URL。",
		Custom:      true,
	},
}

func runStartupConfigIfNeeded(options RunOptions) (RunOptions, startupConfigResult, error) {
	if !startupConfigNeeded(options) {
		return options, startupConfigResult{}, nil
	}
	model := newStartupConfigModel(options)
	program := tea.NewProgram(model, tea.WithAltScreen())
	final, err := program.Run()
	if err != nil {
		return options, startupConfigResult{}, err
	}
	result, ok := final.(startupConfigModel)
	if !ok || result.cancelled {
		return options, startupConfigResult{Cancelled: true}, nil
	}
	return result.applyToOptions(options), startupConfigResult{}, nil
}

func startupConfigNeeded(options RunOptions) bool {
	return strings.TrimSpace(options.APIKey) == ""
}

type startupInput int

const (
	startupInputAPIKey startupInput = iota
	startupInputModel
	startupInputBaseURL
)

type startupConfigModel struct {
	styles     tuiStyles
	width      int
	height     int
	preset     int
	focus      int
	cancelled  bool
	apiKey     textinput.Model
	model      textinput.Model
	baseURL    textinput.Model
	statusText string
}

func newStartupConfigModel(options RunOptions) startupConfigModel {
	apiKey := textinput.New()
	apiKey.Placeholder = "--api-key"
	apiKey.EchoMode = textinput.EchoPassword
	apiKey.SetValue(options.APIKey)
	apiKey.Width = 48

	model := textinput.New()
	model.Placeholder = "--model"
	model.SetValue(firstNonEmpty(options.Model, startupPresets[0].Model))
	model.Width = 48

	baseURL := textinput.New()
	baseURL.Placeholder = "--base-url（可选）"
	baseURL.SetValue(options.BaseURL)
	baseURL.Width = 48

	return startupConfigModel{
		styles:  newTUIStyles(),
		preset:  0,
		apiKey:  apiKey,
		model:   model,
		baseURL: baseURL,
	}
}

func (m startupConfigModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m startupConfigModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeInputs()
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.cancelled = true
			return m, tea.Quit
		case "up", "k":
			if m.focus == 0 {
				m.preset = clampInt(m.preset-1, 0, len(startupPresets)-1)
				m.applyPreset()
			} else {
				m.focus--
				m.focusInput()
			}
			return m, nil
		case "down", "j", "tab":
			if m.focus == 0 {
				m.preset = clampInt(m.preset+1, 0, len(startupPresets)-1)
				m.applyPreset()
			} else {
				m.focus = clampInt(m.focus+1, 1, 4)
				m.focusInput()
			}
			return m, nil
		case "shift+tab":
			m.focus = clampInt(m.focus-1, 0, 3)
			m.focusInput()
			return m, nil
		case " ":
			if m.focus == 0 {
				m.preset = clampInt(m.preset+1, 0, len(startupPresets)-1)
				m.applyPreset()
				return m, nil
			}
		case "enter":
			if m.focus < 4 {
				m.focus++
				m.focusInput()
				return m, nil
			}
			if err := m.validate(); err != nil {
				m.statusText = err.Error()
				return m, nil
			}
			return m, tea.Quit
		}
	}

	var cmd tea.Cmd
	switch m.focus {
	case 1:
		m.apiKey, cmd = m.apiKey.Update(msg)
	case 2:
		m.model, cmd = m.model.Update(msg)
	case 3:
		m.baseURL, cmd = m.baseURL.Update(msg)
	}
	cmds = append(cmds, cmd)
	return m, tea.Batch(cmds...)
}

func (m startupConfigModel) View() string {
	width := m.width
	if width <= 0 {
		width = 86
	}
	bodyWidth := clampInt(width-6, 48, 96)
	title := m.styles.title.Render("bee agent setup")
	help := m.styles.help.Render("首次运行需要补齐配置。↑/↓ 选择，Tab 切换，Space 切换选项，Enter 确认，Esc 取消。")

	var presetLines []string
	for i, preset := range startupPresets {
		cursor := "  "
		style := m.styles.help
		if i == m.preset {
			cursor = "› "
			style = m.styles.command
		}
		presetLines = append(presetLines, style.Render(fmt.Sprintf("%s%s - %s", cursor, preset.Name, preset.Description)))
	}

	saveLine := "确认后仅用于本次启动"
	if m.focus == 4 {
		saveLine = m.styles.command.Render(saveLine)
	}
	status := ""
	if m.statusText != "" {
		status = "\n" + m.styles.warn.Render(m.statusText)
	}

	content := strings.Join([]string{
		title,
		help,
		"",
		m.styles.pane.Render("Preset"),
		strings.Join(presetLines, "\n"),
		"",
		m.styles.pane.Render("Credentials"),
		"API key",
		m.apiKey.View(),
		"",
		"Model",
		m.model.View(),
		"",
		"Base URL",
		m.baseURL.View(),
		"",
		saveLine,
		status,
	}, "\n")

	return lipgloss.NewStyle().
		Width(bodyWidth).
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("238")).
		Padding(1, 2).
		Render(content)
}

func (m *startupConfigModel) resizeInputs() {
	inputWidth := clampInt(m.width-12, 32, 72)
	m.apiKey.Width = inputWidth
	m.model.Width = inputWidth
	m.baseURL.Width = inputWidth
}

func (m *startupConfigModel) focusInput() {
	m.apiKey.Blur()
	m.model.Blur()
	m.baseURL.Blur()
	switch m.focus {
	case 1:
		m.apiKey.Focus()
	case 2:
		m.model.Focus()
	case 3:
		m.baseURL.Focus()
	}
}

func (m *startupConfigModel) applyPreset() {
	preset := startupPresets[m.preset]
	if preset.Custom {
		return
	}
	m.model.SetValue(preset.Model)
	m.baseURL.SetValue(preset.BaseURL)
}

func (m startupConfigModel) validate() error {
	if strings.TrimSpace(m.apiKey.Value()) == "" {
		return fmt.Errorf("请填写 --api-key")
	}
	if strings.TrimSpace(m.model.Value()) == "" {
		return fmt.Errorf("请填写 --model")
	}
	return nil
}

func (m startupConfigModel) applyToOptions(options RunOptions) RunOptions {
	options.APIKey = strings.TrimSpace(m.apiKey.Value())
	options.Model = strings.TrimSpace(m.model.Value())
	options.FallbackModel = strings.TrimSpace(m.model.Value())
	options.BaseURL = strings.TrimSpace(m.baseURL.Value())
	return options
}
