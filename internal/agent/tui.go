package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/alecthomas/chroma/v2/quick"
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

type agentDoneMsg struct {
	History []anthropic.MessageParam
}

type tuiEventMsg uiEvent

const (
	paneGapWidth     = 1
	minChatPaneWidth = 20
	minLogPaneWidth  = 20
)

type tuiTab int

const (
	tuiTabMain tuiTab = iota
	tuiTabDebug
)

type tuiModel struct {
	ctx             context.Context
	client          anthropic.Client
	rt              *agentRuntime
	styles          tuiStyles
	history         []anthropic.MessageParam
	chatView        viewport.Model
	logView         viewport.Model
	debugView       viewport.Model
	input           textarea.Model
	commands        *slashCommandRegistry
	events          chan uiEvent
	approvals       chan bool
	width           int
	height          int
	logPaneWidth    int
	draggingDivider bool
	activeTab       tuiTab
	running         bool
	approving       bool
	status          string
	logs            []string
}

type tuiStyles struct {
	title   lipgloss.Style
	help    lipgloss.Style
	user    lipgloss.Style
	ai      lipgloss.Style
	log     lipgloss.Style
	warn    lipgloss.Style
	command lipgloss.Style
	pane    lipgloss.Style
	divider lipgloss.Style
	box     lipgloss.Style
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
		command: lipgloss.NewStyle().
			Foreground(lipgloss.Color("228")).
			Background(lipgloss.Color("236")).
			Padding(0, 1),
		pane: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("245")),
		divider: lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")),
		box: lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("238")),
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
	rt.startCronScheduler()
	history := chooseInitialSession(rt)

	p := tea.NewProgram(newTUIModel(ctx, client, rt, events, approvals, history), tea.WithAltScreen(), tea.WithMouseCellMotion())
	model, err := p.Run()
	if err != nil {
		fmt.Println(colorRed("TUI error: " + err.Error()))
		return
	}
	if final, ok := model.(tuiModel); ok {
		final.rt.saveCurrentSession(final.history)
		fmt.Printf("Session saved: %s\n", final.rt.sessionID)
	} else {
		fmt.Printf("Session saved: %s\n", rt.sessionID)
	}
}

func newTUIModel(ctx context.Context, client anthropic.Client, rt *agentRuntime, events chan uiEvent, approvals chan bool, history []anthropic.MessageParam) tuiModel {
	chatView := viewport.New(56, 20)
	chatView.SetContent("准备就绪。输入问题后按 Ctrl+S 发送，Ctrl+C 退出。")

	logView := viewport.New(24, 20)
	logView.SetContent("暂无日志。")

	debugView := viewport.New(80, 20)
	debugView.SetContent("暂无 runtime 信息。")

	ti := textarea.New()
	ti.Placeholder = "输入你的问题..."
	ti.Focus()
	ti.ShowLineNumbers = false
	ti.SetHeight(4)

	return tuiModel{
		ctx:          ctx,
		client:       client,
		rt:           rt,
		styles:       newTUIStyles(),
		history:      history,
		chatView:     chatView,
		logView:      logView,
		debugView:    debugView,
		input:        ti,
		commands:     newSlashCommandRegistry(),
		events:       events,
		approvals:    approvals,
		logPaneWidth: 34,
		status:       "ready",
	}
}

func chooseInitialSession(rt *agentRuntime) []anthropic.MessageParam {
	if rt == nil || rt.sessions == nil {
		return nil
	}
	records := rt.sessions.list()
	if len(records) == 0 || sessionResumePromptDisabled() {
		_ = rt.sessions.saveSnapshot(rt.sessionID, nil)
		fmt.Printf("New session: %s\n", rt.sessionID)
		return nil
	}
	fmt.Println("Resume a session? Enter number, session id, or empty for new session:")
	for i, record := range records {
		fmt.Printf("%d. %s  messages=%d  updated=%s  %s\n", i+1, record.ID, record.MessageCount, record.UpdatedAt.Format("2006-01-02 15:04"), record.Preview)
	}
	fmt.Print("> ")
	reader := bufio.NewReader(os.Stdin)
	choice, _ := reader.ReadString('\n')
	choice = strings.TrimSpace(choice)
	if choice == "" {
		_ = rt.sessions.saveSnapshot(rt.sessionID, nil)
		fmt.Printf("New session: %s\n", rt.sessionID)
		return nil
	}
	selected := resolveSessionChoice(choice, records)
	messages, record, err := rt.resumeSession(selected)
	if err != nil {
		fmt.Printf("Resume failed: %v\n", err)
		_ = rt.sessions.saveSnapshot(rt.sessionID, nil)
		fmt.Printf("New session: %s\n", rt.sessionID)
		return nil
	}
	fmt.Printf("Resumed session: %s (%d messages)\n", record.ID, record.MessageCount)
	return messages
}

func sessionResumePromptDisabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("BEE_AGENT_RESUME_PROMPT"))) {
	case "0", "false", "no", "off", "skip":
		return true
	default:
		return false
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

	case tea.MouseMsg:
		m.handleMouse(msg)
		return m, nil

	case tea.KeyMsg:
		if isLeakedMouseReportKey(msg) {
			return m, nil
		}
		switch {
		case msg.String() == "1":
			m.activeTab = tuiTabMain
			m.refreshViews()
			return m, nil

		case msg.String() == "2":
			m.activeTab = tuiTabDebug
			m.refreshViews()
			return m, nil

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
			if command, ok := m.executeSlashCommand(query); ok {
				m.input.Reset()
				m.refreshViews()
				if command != nil {
					cmds = append(cmds, command)
				}
				return m, tea.Batch(cmds...)
			}
			if query == "q" || query == "exit" {
				return m, tea.Quit
			}
			m.rt.triggerHooks(EventUserPromptSubmit, query)
			m.history = append(m.history, anthropic.NewUserMessage(anthropic.NewTextBlock(query)))
			m.rt.saveCurrentSession(m.history)
			m.input.Reset()
			m.running = true
			m.status = "thinking"
			m.refreshViews()
			cmds = append(cmds, runAgentTurn(m.ctx, m.client, m.rt, append([]anthropic.MessageParam(nil), m.history...)))

		case !m.running && !m.approving && msg.String() == "tab":
			if m.completeSlashCommand() {
				m.refreshViews()
				return m, nil
			}

		case !m.approving:
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			m.cleanInputEscapeReports()
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

		case uiEventCronQueued:
			m.logs = append(m.logs, m.styles.log.Render(event.Text))
			if cmd := m.startScheduledCronIfIdle(); cmd != nil {
				cmds = append(cmds, cmd)
			}
			m.refreshViews()
			cmds = append(cmds, waitForUIEvent(m.events))
		}

	case agentDoneMsg:
		m.history = msg.History
		m.rt.saveCurrentSession(m.history)
		m.running = false
		m.status = "ready"
		if cmd := m.startScheduledCronIfIdle(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		m.refreshViews()
		cmds = append(cmds, waitForUIEvent(m.events))
	}

	var cmd tea.Cmd
	if m.activeTab == tuiTabDebug {
		m.debugView, cmd = m.debugView.Update(msg)
		cmds = append(cmds, cmd)
	} else {
		m.chatView, cmd = m.chatView.Update(msg)
		cmds = append(cmds, cmd)
		m.logView, cmd = m.logView.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m *tuiModel) startScheduledCronIfIdle() tea.Cmd {
	if m.running || m.approving || m.rt == nil || m.rt.cron == nil || !m.rt.cron.hasQueue() {
		return nil
	}
	m.running = true
	m.status = "scheduled"
	return runAgentTurn(m.ctx, m.client, m.rt, append([]anthropic.MessageParam(nil), m.history...))
}

func (m *tuiModel) resize() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	headerHeight := 1
	helpHeight := 1
	inputHeight := 5
	commandHintHeight := 1
	viewportHeight := m.height - headerHeight - helpHeight - commandHintHeight - inputHeight - 2
	if viewportHeight < 5 {
		viewportHeight = 5
	}
	if m.logPaneWidth <= 0 {
		m.logPaneWidth = defaultLogPaneWidth(m.width)
	}
	m.logPaneWidth = clampLogPaneWidth(m.width, m.logPaneWidth)
	chatWidth := m.chatPaneWidth()

	// viewport 宽度要扣掉边框左右各 1 列，避免内容挤破右边界。
	m.chatView.Width = maxInt(1, chatWidth-2)
	m.chatView.Height = viewportHeight - 2
	m.logView.Width = maxInt(1, m.logPaneWidth-2)
	m.logView.Height = viewportHeight - 2
	m.debugView.Width = maxInt(1, m.width-2)
	m.debugView.Height = viewportHeight - 2
	m.input.SetWidth(maxInt(1, m.width-2))
	m.input.SetHeight(inputHeight)
	m.refreshViews()
}

func (m *tuiModel) executeSlashCommand(query string) (tea.Cmd, bool) {
	name, args, ok := parseSlashCommand(query)
	if !ok {
		return nil, false
	}
	if name == "" {
		m.logs = append(m.logs, m.styles.warn.Render("请输入命令名，按 Tab 查看可用命令"))
		return nil, true
	}
	command, found := m.commands.get(name)
	if !found {
		m.logs = append(m.logs, m.styles.warn.Render(fmt.Sprintf("未知命令：/%s。输入 /help 查看可用命令。", name)))
		return nil, true
	}
	return command.Handler(m, args), true
}

func (m *tuiModel) completeSlashCommand() bool {
	prefix, ok := slashCommandPrefix(m.input.Value())
	if !ok {
		return false
	}
	candidates := m.commands.complete(prefix)
	if len(candidates) == 0 {
		return false
	}
	m.input.SetValue(candidates[0].Usage + " ")
	return true
}

func (m *tuiModel) cleanInputEscapeReports() {
	cleaned := stripSGRMouseReports(m.input.Value())
	if cleaned != m.input.Value() {
		m.input.SetValue(cleaned)
	}
}

func isLeakedMouseReportKey(msg tea.KeyMsg) bool {
	key := tea.Key(msg)
	if key.Type == tea.KeyRunes {
		return isSGRMouseReport(string(key.Runes))
	}
	return isSGRMouseReport(msg.String())
}

func stripSGRMouseReports(text string) string {
	var cleaned strings.Builder
	for i := 0; i < len(text); {
		if end, ok := sgrMouseReportEnd(text, i); ok {
			i = end
			continue
		}
		cleaned.WriteByte(text[i])
		i++
	}
	return cleaned.String()
}

func isSGRMouseReport(text string) bool {
	end, ok := sgrMouseReportEnd(text, 0)
	return ok && end == len(text)
}

func sgrMouseReportEnd(text string, start int) (int, bool) {
	i := start
	if i < len(text) && text[i] == '\x1b' {
		i++
	}
	if i+2 >= len(text) || text[i] != '[' || text[i+1] != '<' {
		return 0, false
	}
	i += 2
	for part := 0; part < 3; part++ {
		partStart := i
		for i < len(text) && text[i] >= '0' && text[i] <= '9' {
			i++
		}
		if partStart == i {
			return 0, false
		}
		if part < 2 {
			if i >= len(text) || text[i] != ';' {
				return 0, false
			}
			i++
		}
	}
	if i >= len(text) || (text[i] != 'M' && text[i] != 'm') {
		return 0, false
	}
	return i + 1, true
}

func (m tuiModel) chatPaneWidth() int {
	return m.width - m.logPaneWidth - paneGapWidth
}

func defaultLogPaneWidth(totalWidth int) int {
	logWidth := 34
	if totalWidth < 100 {
		logWidth = totalWidth / 3
	}
	return clampLogPaneWidth(totalWidth, logWidth)
}

func clampLogPaneWidth(totalWidth int, desired int) int {
	available := totalWidth - paneGapWidth
	if available <= 0 {
		return 1
	}
	minLog := minLogPaneWidth
	minChat := minChatPaneWidth
	if available < minLog+minChat {
		if available <= 2 {
			return maxInt(1, available-1)
		}
		minLog = available / 3
		if minLog < 1 {
			minLog = 1
		}
		minChat = available - minLog
		if minChat < 1 {
			minChat = 1
			minLog = available - minChat
		}
	}
	maxLog := available - minChat
	if maxLog < minLog {
		maxLog = minLog
	}
	return clampInt(desired, minLog, maxLog)
}

func clampInt(value int, minValue int, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func (m *tuiModel) handleMouse(msg tea.MouseMsg) bool {
	if m.activeTab == tuiTabDebug {
		m.scrollDebugByMouse(msg)
		return true
	}

	switch {
	case isMouseRelease(msg):
		if m.draggingDivider {
			m.draggingDivider = false
		}
		return true

	case isMouseLeftPress(msg) && m.isDividerHit(msg.X, msg.Y):
		m.draggingDivider = true
		m.resizeFromDividerX(msg.X)
		return true

	case m.draggingDivider && isMouseDrag(msg):
		m.resizeFromDividerX(msg.X)
		return true

	case isMouseWheel(msg):
		m.scrollMainPaneByMouse(msg)
		return true
	}
	return true
}

func isMouseLeftPress(msg tea.MouseMsg) bool {
	return msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft || msg.Type == tea.MouseLeft
}

func isMouseRelease(msg tea.MouseMsg) bool {
	return msg.Action == tea.MouseActionRelease || msg.Type == tea.MouseRelease
}

func isMouseDrag(msg tea.MouseMsg) bool {
	return msg.Action == tea.MouseActionMotion || msg.Type == tea.MouseMotion
}

func isMouseWheel(msg tea.MouseMsg) bool {
	if msg.Action != tea.MouseActionPress {
		return false
	}
	return msg.Button == tea.MouseButtonWheelUp ||
		msg.Button == tea.MouseButtonWheelDown ||
		msg.Button == tea.MouseButtonWheelLeft ||
		msg.Button == tea.MouseButtonWheelRight
}

func (m tuiModel) isDividerHit(x int, y int) bool {
	if m.width <= 0 || m.height <= 0 {
		return false
	}
	dividerX := m.chatPaneWidth()
	return m.isMainBodyY(y) && x >= dividerX-1 && x <= dividerX+1
}

func (m *tuiModel) resizeFromDividerX(x int) {
	desiredLogWidth := m.width - x - paneGapWidth
	m.logPaneWidth = clampLogPaneWidth(m.width, desiredLogWidth)
	m.resize()
}

func (m *tuiModel) scrollMainPaneByMouse(msg tea.MouseMsg) {
	var cmd tea.Cmd
	switch {
	case m.isChatHit(msg.X, msg.Y):
		m.chatView, cmd = m.chatView.Update(msg)
	case m.isLogHit(msg.X, msg.Y):
		m.logView, cmd = m.logView.Update(msg)
	}
	_ = cmd
}

func (m *tuiModel) scrollDebugByMouse(msg tea.MouseMsg) {
	if !isMouseWheel(msg) || !m.isDebugHit(msg.X, msg.Y) {
		return
	}
	var cmd tea.Cmd
	m.debugView, cmd = m.debugView.Update(msg)
	_ = cmd
}

func (m tuiModel) isMainBodyY(y int) bool {
	bodyTop := 1
	bodyBottom := bodyTop + m.chatView.Height + 1
	return y >= bodyTop && y <= bodyBottom
}

func (m tuiModel) isChatHit(x int, y int) bool {
	return m.isMainBodyY(y) && x >= 0 && x < m.chatPaneWidth()
}

func (m tuiModel) isLogHit(x int, y int) bool {
	logStart := m.chatPaneWidth() + paneGapWidth
	return m.isMainBodyY(y) && x >= logStart && x < logStart+m.logPaneWidth
}

func (m tuiModel) isDebugHit(x int, y int) bool {
	bodyTop := 1
	bodyBottom := bodyTop + m.debugView.Height + 1
	return y >= bodyTop && y <= bodyBottom && x >= 0 && x < m.width
}

func (m *tuiModel) refreshViews() {
	m.refreshChat()
	m.refreshLogs()
	m.refreshDebug()
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

func (m *tuiModel) refreshDebug() {
	m.debugView.SetContent(m.renderDebug())
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
		return m.styles.ai.Render("Agent") + "\n" + m.renderMarkdown(body, m.chatView.Width-2)
	default:
		return body
	}
}

func (m tuiModel) renderMarkdown(content string, wrapWidth int) string {
	width := maxInt(20, wrapWidth)
	renderer, err := glamour.NewTermRenderer(
		// 固定主题避免 WithAutoStyle 触发 OSC 11 背景色查询；查询响应可能漏进 textarea。
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return content
	}
	rendered, err := renderer.Render(content)
	if err != nil {
		return content
	}
	return strings.TrimRight(rendered, "\n")
}

func (m tuiModel) renderDebug() string {
	var sections []string
	sections = append(sections, "Runtime")
	sections = append(sections, highlightCode(m.renderRuntimeDebug(), "json"))
	sections = append(sections, "")
	sections = append(sections, "System Prompt")
	sections = append(sections, m.renderMarkdown(m.renderCurrentSystemPrompt(), m.debugView.Width-2))
	sections = append(sections, "")
	sections = append(sections, "Messages")
	sections = append(sections, highlightCode(marshalDebugJSON(m.history), "json"))
	return strings.Join(sections, "\n")
}

func (m tuiModel) renderRuntimeDebug() string {
	hookCounts := make(map[string]int)
	for event, callbacks := range m.rt.hooks {
		hookCounts[string(event)] = len(callbacks)
	}

	moduleIDs := []string{}
	moduleSnapshots := map[string]any{}
	if m.rt.modules != nil {
		moduleIDs = m.rt.modules.moduleIDs()
		moduleSnapshots = m.rt.modules.runtimeSnapshots()
	}

	snapshot := map[string]any{
		"status":            m.status,
		"sessionID":         m.rt.sessionID,
		"running":           m.running,
		"approving":         m.approving,
		"activeTab":         m.activeTabName(),
		"mode":              m.rt.activeMode(),
		"modeRegistry":      m.rt.modes.snapshot(),
		"blueprint":         m.rt.blueprintSnapshot(),
		"messageCount":      len(m.history),
		"logCount":          len(m.logs),
		"memoryTurns":       m.rt.memoryTurns,
		"promptCacheKey":    m.rt.promptCache.contextKey,
		"promptCacheLength": len(m.rt.promptCache.prompt),
		"recovery": map[string]any{
			"model":                 m.rt.recovery.model,
			"maxTokens":             m.rt.recovery.maxTokens,
			"escalatedMaxTokens":    m.rt.recovery.escalatedMaxTokens,
			"retries":               m.rt.recovery.retries,
			"maxTokenContinuations": m.rt.recovery.maxTokenContinuations,
		},
		"config":      m.rt.config,
		"hooks":       hookCounts,
		"modules":     moduleIDs,
		"moduleState": moduleSnapshots,
	}
	return marshalDebugJSON(snapshot)
}

func (m tuiModel) activeTabName() string {
	if m.activeTab == tuiTabDebug {
		return "debug"
	}
	return "main"
}

func marshalDebugJSON(value any) string {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return string(raw)
}

func highlightCode(source string, lexer string) string {
	var buf bytes.Buffer
	if err := quick.Highlight(&buf, source, lexer, "terminal256", "monokai"); err != nil {
		return source
	}
	return strings.TrimRight(buf.String(), "\n")
}

func (m tuiModel) renderCurrentSystemPrompt() string {
	return assembleSystemPrompt(m.rt.promptContext(m.rt.mainAgentSpec().ToolNames))
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
		m.styles.title.Render("bee agent"),
		" ",
		m.styles.help.Render("[1 对话] [2 Debug]"),
		" ",
		m.styles.help.Render("mode:"+m.rt.activeMode().Name),
		" ",
		m.styles.help.Render("session:"+shortSessionID(m.rt.sessionID)),
		" ",
		m.styles.help.Render(status),
	)
	help := m.styles.help.Render("1 对话 | 2 Debug | /new 新建 | /resume 恢复 | /mode 切换 | Tab 补全 | Ctrl+S 发送 | Ctrl+C 退出")
	if m.approving {
		help = m.styles.warn.Render("权限确认中：按 y 允许，按 n 拒绝")
	}

	body := ""
	if m.activeTab == tuiTabDebug {
		body = m.renderPane("Debug", m.debugView.View(), m.debugView.Width+2, m.debugView.Height+2)
	} else {
		chatPane := m.renderPane("对话", m.chatView.View(), m.chatView.Width+2, m.chatView.Height+2)
		logPane := m.renderPane("日志", m.logView.View(), m.logView.Width+2, m.logView.Height+2)
		body = lipgloss.JoinHorizontal(lipgloss.Top, chatPane, m.renderDivider(m.chatView.Height+2), logPane)
	}

	return strings.Join([]string{
		header,
		body,
		m.renderSlashCommandHint(),
		m.input.View(),
		help,
	}, "\n")
}

func (m tuiModel) renderSlashCommandHint() string {
	prefix, ok := slashCommandPrefix(m.input.Value())
	if !ok {
		return ""
	}
	candidates := m.commands.complete(prefix)
	if len(candidates) == 0 {
		return m.styles.warn.Render("无匹配命令。输入 /help 查看可用命令。")
	}

	items := make([]string, 0, len(candidates))
	for i, command := range candidates {
		if i >= 5 {
			items = append(items, "...")
			break
		}
		items = append(items, fmt.Sprintf("%s %s", command.Usage, command.Description))
	}
	// 候选栏只做轻量提示，真正执行仍由 Ctrl+S 统一拦截处理。
	return m.styles.command.Width(maxInt(1, m.width-2)).Render("命令补全： " + strings.Join(items, "  |  "))
}

func (m tuiModel) renderPane(title string, content string, width int, height int) string {
	// 每个栏位都有独立标题和边框，阅读时能快速区分对话与运行日志。
	titleLine := m.styles.pane.Width(width - 2).Render(title)
	body := lipgloss.JoinVertical(lipgloss.Left, titleLine, content)
	return m.styles.box.Width(width - 2).Height(height - 2).Render(body)
}

func (m tuiModel) renderDivider(height int) string {
	if height <= 0 {
		return ""
	}
	lines := make([]string, height)
	for i := range lines {
		lines[i] = "│"
	}
	return m.styles.divider.Render(strings.Join(lines, "\n"))
}
