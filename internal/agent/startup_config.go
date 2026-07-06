package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
		Description: "使用项目默认模型 deepseek-v4-flash，base URL 由你的网关或环境决定。",
		Model:       "deepseek-v4-flash",
	},
	{
		Name:        "Claude Sonnet",
		Description: "使用 Anthropic 官方 Claude Sonnet；base URL 留空。",
		Model:       "claude-sonnet-4-20250514",
	},
	{
		Name:        "Custom",
		Description: "手动输入 MODEL 和可选 ANTHROPIC_BASE_URL。",
		Custom:      true,
	},
}

func runStartupConfigIfNeeded() (startupConfigResult, error) {
	if !startupConfigNeeded() {
		return startupConfigResult{}, nil
	}
	model := newStartupConfigModel()
	program := tea.NewProgram(model, tea.WithAltScreen())
	final, err := program.Run()
	if err != nil {
		return startupConfigResult{}, err
	}
	result, ok := final.(startupConfigModel)
	if !ok || result.cancelled {
		return startupConfigResult{Cancelled: true}, nil
	}
	values := result.values()
	applyStartupConfig(values)
	if result.saveEnv {
		if err := writeDotenvValues(dotenvWritePath(), values); err != nil {
			return startupConfigResult{}, err
		}
	}
	return startupConfigResult{}, nil
}

func startupConfigNeeded() bool {
	// MODEL/FALLBACK_MODEL 有运行时默认值；只有真正无法发请求的 API key 缺失时才打断启动。
	return strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")) == ""
}

func dotenvSearchPaths() []string {
	wd, err := os.Getwd()
	if err != nil {
		return nil
	}
	paths := []string{}
	for dir := wd; ; dir = filepath.Dir(dir) {
		path := filepath.Join(dir, ".env")
		if _, err := os.Stat(path); err == nil {
			paths = append(paths, path)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}
	return paths
}

func dotenvWritePath() string {
	paths := dotenvSearchPaths()
	if len(paths) > 0 {
		return paths[0]
	}
	return ".env"
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
	saveEnv    bool
	cancelled  bool
	apiKey     textinput.Model
	model      textinput.Model
	baseURL    textinput.Model
	statusText string
}

func newStartupConfigModel() startupConfigModel {
	apiKey := textinput.New()
	apiKey.Placeholder = "ANTHROPIC_API_KEY"
	apiKey.EchoMode = textinput.EchoPassword
	apiKey.Width = 48

	model := textinput.New()
	model.Placeholder = "MODEL"
	model.SetValue(firstNonEmpty(os.Getenv("MODEL"), startupPresets[0].Model))
	model.Width = 48

	baseURL := textinput.New()
	baseURL.Placeholder = "ANTHROPIC_BASE_URL（可选）"
	baseURL.SetValue(os.Getenv("ANTHROPIC_BASE_URL"))
	baseURL.Width = 48

	return startupConfigModel{
		styles:  newTUIStyles(),
		preset:  0,
		saveEnv: true,
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
			if m.focus == 4 {
				m.saveEnv = !m.saveEnv
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

	saveMark := "[ ]"
	if m.saveEnv {
		saveMark = "[x]"
	}
	saveLine := fmt.Sprintf("%s 保存到 .env", saveMark)
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
	if strings.TrimSpace(m.apiKey.Value()) == "" && strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")) == "" {
		return fmt.Errorf("请填写 ANTHROPIC_API_KEY")
	}
	if strings.TrimSpace(m.model.Value()) == "" {
		return fmt.Errorf("请填写 MODEL")
	}
	return nil
}

func (m startupConfigModel) values() map[string]string {
	values := map[string]string{
		"MODEL":              strings.TrimSpace(m.model.Value()),
		"FALLBACK_MODEL":     strings.TrimSpace(m.model.Value()),
		"ANTHROPIC_BASE_URL": strings.TrimSpace(m.baseURL.Value()),
	}
	if apiKey := strings.TrimSpace(m.apiKey.Value()); apiKey != "" {
		values["ANTHROPIC_API_KEY"] = apiKey
	}
	return values
}

func applyStartupConfig(values map[string]string) {
	for key, value := range values {
		_ = os.Setenv(key, value)
	}
}

func writeDotenvValues(path string, values map[string]string) error {
	existing, _ := os.ReadFile(path)
	lines := []string{}
	if len(existing) > 0 {
		lines = strings.Split(strings.TrimRight(string(existing), "\n"), "\n")
	}

	seen := map[string]bool{}
	for i, line := range lines {
		key, ok := dotenvLineKey(line)
		if !ok {
			continue
		}
		if value, exists := values[key]; exists {
			lines[i] = key + "=" + shellQuoteEnvValue(value)
			seen[key] = true
		}
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		if !seen[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		lines = append(lines, key+"="+shellQuoteEnvValue(values[key]))
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0600)
}

func dotenvLineKey(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") || !strings.Contains(trimmed, "=") {
		return "", false
	}
	key := strings.TrimSpace(strings.SplitN(trimmed, "=", 2)[0])
	if key == "" {
		return "", false
	}
	return key, true
}

func shellQuoteEnvValue(value string) string {
	if value == "" {
		return `""`
	}
	if strings.IndexFunc(value, func(r rune) bool {
		return r == '"' || r == '\'' || r == '#' || r == ' ' || r == '\t' || r == '\n'
	}) == -1 {
		return value
	}
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}
