package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type agentDoneMsg struct {
	History []anthropic.MessageParam
}

type tuiEventMsg uiEvent

type tuiModel struct {
	ctx       context.Context
	client    anthropic.Client
	rt        *agentRuntime
	styles    tuiStyles
	history   []anthropic.MessageParam
	chatView  viewport.Model
	logView   viewport.Model
	input     textarea.Model
	events    chan uiEvent
	approvals chan bool
	width     int
	height    int
	running   bool
	approving bool
	status    string
	logs      []string
}

type tuiStyles struct {
	title lipgloss.Style
	help  lipgloss.Style
	user  lipgloss.Style
	ai    lipgloss.Style
	log   lipgloss.Style
	warn  lipgloss.Style
	pane  lipgloss.Style
	box   lipgloss.Style
}

func newTUIStyles() tuiStyles {
	return tuiStyles{
		title: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("15")).
			Background(lipgloss.Color("63")).
			Padding(0, 1),
		help: lipgloss.NewStyle().Foreground(lipgloss.Color("241")),
		user: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39")),
		ai:   lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("82")),
		log:  lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
		warn: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214")),
		pane: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("245")),
		box:  lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("238")),
	}
}

func runTUI(ctx context.Context, client anthropic.Client) {
	events := make(chan uiEvent, 64)
	approvals := make(chan bool)
	config, err := newAgentConfig()
	if err != nil {
		fmt.Println(colorRed("Config error: " + err.Error()))
		return
	}
	rt := newAgentRuntime(config, events, approvals)

	p := tea.NewProgram(newTUIModel(ctx, client, rt, events, approvals), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Println(colorRed("TUI error: " + err.Error()))
	}
}

func newTUIModel(ctx context.Context, client anthropic.Client, rt *agentRuntime, events chan uiEvent, approvals chan bool) tuiModel {
	chatView := viewport.New(56, 20)
	chatView.SetContent("准备就绪。输入问题后按 Ctrl+S 发送，Ctrl+C 退出。")

	logView := viewport.New(24, 20)
	logView.SetContent("暂无日志。")

	ti := textarea.New()
	ti.Placeholder = "输入你的问题..."
	ti.Focus()
	ti.ShowLineNumbers = false
	ti.SetHeight(4)

	return tuiModel{
		ctx:       ctx,
		client:    client,
		rt:        rt,
		styles:    newTUIStyles(),
		chatView:  chatView,
		logView:   logView,
		input:     ti,
		events:    events,
		approvals: approvals,
		status:    "ready",
	}
}

// Init 在 Bubble Tea 程序启动时调用；这里启动一个等待后台事件的命令。
func (m tuiModel) Init() tea.Cmd {
	return waitForUIEvent(m.events)
}

func waitForUIEvent(events <-chan uiEvent) tea.Cmd {
	return func() tea.Msg {
		return tuiEventMsg(<-events)
	}
}

func runAgentTurn(ctx context.Context, client anthropic.Client, rt *agentRuntime, history []anthropic.MessageParam) tea.Cmd {
	return func() tea.Msg {
		rt.agentLoop(ctx, client, &history)
		return agentDoneMsg{History: history}
	}
}

// Update 是 Bubble Tea 的状态机入口；键盘、窗口尺寸、后台事件都会走到这里。
func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resize()

	case tea.KeyMsg:
		switch {
		case m.approving && (msg.String() == "y" || msg.String() == "Y"):
			m.approving = false
			m.status = "running"
			m.logs = append(m.logs, m.styles.warn.Render("权限确认：已允许"))
			m.approvals <- true
			m.refreshViews()
			return m, waitForUIEvent(m.events)

		case m.approving && (msg.String() == "n" || msg.String() == "N" || msg.String() == "esc"):
			m.approving = false
			m.status = "running"
			m.logs = append(m.logs, m.styles.warn.Render("权限确认：已拒绝"))
			m.approvals <- false
			m.refreshViews()
			return m, waitForUIEvent(m.events)

		case msg.String() == "ctrl+c":
			return m, tea.Quit

		case !m.running && !m.approving && msg.String() == "ctrl+s":
			query := strings.TrimSpace(m.input.Value())
			if query == "" {
				return m, nil
			}
			if query == "q" || query == "exit" {
				return m, tea.Quit
			}
			m.rt.triggerHooks(EventUserPromptSubmit, query)
			m.history = append(m.history, anthropic.NewUserMessage(anthropic.NewTextBlock(query)))
			m.input.Reset()
			m.running = true
			m.status = "thinking"
			m.refreshViews()
			cmds = append(cmds, runAgentTurn(m.ctx, m.client, m.rt, append([]anthropic.MessageParam(nil), m.history...)))

		case !m.approving:
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			cmds = append(cmds, cmd)
		}

	case tuiEventMsg:
		event := uiEvent(msg)
		switch event.Kind {
		case uiEventLog:
			m.logs = append(m.logs, m.styles.log.Render(event.Text))
			m.refreshViews()
			cmds = append(cmds, waitForUIEvent(m.events))

		case uiEventApproval:
			m.approving = true
			m.status = "approval"
			m.logs = append(m.logs, m.styles.warn.Render(event.Text), m.styles.warn.Render("按 y 允许，按 n 拒绝。"))
			m.refreshViews()
		}

	case agentDoneMsg:
		m.history = msg.History
		m.running = false
		m.status = "ready"
		m.refreshViews()
		cmds = append(cmds, waitForUIEvent(m.events))
	}

	var cmd tea.Cmd
	m.chatView, cmd = m.chatView.Update(msg)
	cmds = append(cmds, cmd)
	m.logView, cmd = m.logView.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m *tuiModel) resize() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	headerHeight := 1
	helpHeight := 1
	inputHeight := 5
	viewportHeight := m.height - headerHeight - helpHeight - inputHeight - 2
	if viewportHeight < 5 {
		viewportHeight = 5
	}
	logWidth := 34
	if m.width < 100 {
		logWidth = m.width / 3
	}
	if logWidth < 24 {
		logWidth = 24
	}
	if m.width-logWidth < 40 {
		logWidth = m.width - 40
	}
	if logWidth < 20 {
		logWidth = 20
	}
	gapWidth := 1
	chatWidth := m.width - logWidth - gapWidth
	if chatWidth < 20 {
		chatWidth = 20
	}

	// viewport 宽度要扣掉边框左右各 1 列，避免内容挤破右边界。
	m.chatView.Width = chatWidth - 2
	m.chatView.Height = viewportHeight - 2
	m.logView.Width = logWidth - 2
	m.logView.Height = viewportHeight - 2
	m.input.SetWidth(m.width - 2)
	m.input.SetHeight(inputHeight)
	m.refreshViews()
}

func (m *tuiModel) refreshViews() {
	m.refreshChat()
	m.refreshLogs()
}

func (m *tuiModel) refreshChat() {
	var sections []string
	for _, message := range m.history {
		rendered := m.renderMessage(message)
		if rendered != "" {
			sections = append(sections, rendered)
		}
	}
	if len(sections) == 0 {
		sections = append(sections, "准备就绪。输入问题后按 Ctrl+S 发送，Ctrl+C 退出。")
	}
	m.chatView.SetContent(strings.Join(sections, "\n\n"))
	m.chatView.GotoBottom()
}

func (m *tuiModel) refreshLogs() {
	content := "暂无日志。"
	if len(m.logs) > 0 {
		content = strings.Join(m.logs, "\n")
	}
	m.logView.SetContent(content)
	m.logView.GotoBottom()
}

func (m tuiModel) renderMessage(message anthropic.MessageParam) string {
	if isInjectedContext(extractResponseText(message)) {
		return ""
	}
	var parts []string
	for _, block := range message.Content {
		switch {
		case block.OfText != nil:
			parts = append(parts, block.OfText.Text)
		case block.OfThinking != nil:
			parts = append(parts, "Thinking: "+block.OfThinking.Thinking)
		case block.OfToolUse != nil:
			parts = append(parts, fmt.Sprintf("[tool_use] %s", block.OfToolUse.Name))
		case block.OfToolResult != nil:
			parts = append(parts, "[tool_result]")
		}
	}
	if len(parts) == 0 {
		return ""
	}
	body := strings.Join(parts, "\n")
	switch message.Role {
	case "user":
		return m.styles.user.Render("You") + "\n" + body
	case "assistant":
		return m.styles.ai.Render("Agent") + "\n" + body
	default:
		return body
	}
}

// View 每次状态变化后重新渲染整屏；Bubble Tea 会负责 diff 和绘制。
func (m tuiModel) View() string {
	status := m.status
	if m.running {
		status = "thinking"
	}
	if m.approving {
		status = "approval required"
	}

	header := lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.styles.title.Render("go_agent"),
		" ",
		m.styles.help.Render(status),
	)
	help := m.styles.help.Render("Ctrl+S 发送 | Enter 换行 | Ctrl+C 退出")
	if m.approving {
		help = m.styles.warn.Render("权限确认中：按 y 允许，按 n 拒绝")
	}

	chatPane := m.renderPane("对话", m.chatView.View(), m.chatView.Width+2, m.chatView.Height+2)
	logPane := m.renderPane("日志", m.logView.View(), m.logView.Width+2, m.logView.Height+2)
	body := lipgloss.JoinHorizontal(lipgloss.Top, chatPane, " ", logPane)

	return strings.Join([]string{
		header,
		body,
		m.input.View(),
		help,
	}, "\n")
}

func (m tuiModel) renderPane(title string, content string, width int, height int) string {
	// 每个栏位都有独立标题和边框，阅读时能快速区分对话与运行日志。
	titleLine := m.styles.pane.Width(width - 2).Render(title)
	body := lipgloss.JoinVertical(lipgloss.Left, titleLine, content)
	return m.styles.box.Width(width - 2).Height(height - 2).Render(body)
}
