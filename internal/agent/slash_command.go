package agent

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// slashCommandHandler 直接修改 TUI 状态；返回 tea.Cmd 用于需要退出等副作用的命令。
type slashCommandHandler func(m *tuiModel, args string) tea.Cmd

type slashCommand struct {
	Name        string
	Usage       string
	Description string
	Handler     slashCommandHandler
}

type slashCommandRegistry struct {
	commands map[string]slashCommand
	order    []string
}

// newSlashCommandRegistry 显式注册内置命令，避免运行期依赖包级可变全局表。
func newSlashCommandRegistry() *slashCommandRegistry {
	registry := &slashCommandRegistry{
		commands: make(map[string]slashCommand),
		order:    []string{},
	}
	registry.register(slashCommand{
		Name:        "help",
		Usage:       "/help",
		Description: "显示可用命令",
		Handler: func(m *tuiModel, _ string) tea.Cmd {
			m.logs = append(m.logs, m.styles.log.Render(m.commands.helpText()))
			return nil
		},
	})
	registry.register(slashCommand{
		Name:        "clear",
		Usage:       "/clear",
		Description: "清空当前对话历史",
		Handler: func(m *tuiModel, _ string) tea.Cmd {
			m.history = nil
			m.logs = append(m.logs, m.styles.log.Render("已清空对话历史"))
			return nil
		},
	})
	registry.register(slashCommand{
		Name:        "new",
		Usage:       "/new",
		Description: "新建 session",
		Handler: func(m *tuiModel, _ string) tea.Cmd {
			old := ""
			current := ""
			if m.rt != nil {
				old = m.rt.sessionID
				m.rt.saveCurrentSession(m.history)
				current = m.rt.startNewSession(time.Now())
				m.rt.saveCurrentSession(nil)
			}
			m.history = nil
			m.logs = append(m.logs, m.styles.log.Render(fmt.Sprintf("已新建 session：%s（上一 session：%s）", current, old)))
			return nil
		},
	})
	registry.register(slashCommand{
		Name:        "resume",
		Usage:       "/resume [id|number]",
		Description: "列出或恢复 session",
		Handler: func(m *tuiModel, args string) tea.Cmd {
			if m.rt == nil || m.rt.sessions == nil {
				m.logs = append(m.logs, m.styles.warn.Render("session store 未初始化"))
				return nil
			}
			records := m.rt.sessions.list()
			choice := strings.TrimSpace(args)
			if choice == "" {
				m.logs = append(m.logs, m.styles.log.Render(formatSessionList(records)))
				return nil
			}
			selected := resolveSessionChoice(choice, records)
			m.rt.saveCurrentSession(m.history)
			messages, record, err := m.rt.resumeSession(selected)
			if err != nil {
				m.logs = append(m.logs, m.styles.warn.Render(fmt.Sprintf("恢复失败：%v", err)))
				return nil
			}
			m.history = messages
			m.logs = append(m.logs, m.styles.log.Render(fmt.Sprintf("已恢复 session：%s（%d messages）", record.ID, record.MessageCount)))
			return nil
		},
	})
	registry.register(slashCommand{
		Name:        "debug",
		Usage:       "/debug",
		Description: "切换到 Debug 标签页",
		Handler: func(m *tuiModel, _ string) tea.Cmd {
			m.activeTab = tuiTabDebug
			m.logs = append(m.logs, m.styles.log.Render("已切换到 Debug 标签页"))
			return nil
		},
	})
	registry.register(slashCommand{
		Name:        "chat",
		Usage:       "/chat",
		Description: "切换到对话标签页",
		Handler: func(m *tuiModel, _ string) tea.Cmd {
			m.activeTab = tuiTabMain
			m.logs = append(m.logs, m.styles.log.Render("已切换到对话标签页"))
			return nil
		},
	})
	registry.register(slashCommand{
		Name:        "mode",
		Usage:       "/mode [name]",
		Description: "查看或切换 agent mode",
		Handler: func(m *tuiModel, args string) tea.Cmd {
			name := strings.TrimSpace(args)
			if name == "" {
				m.logs = append(m.logs, m.styles.log.Render(m.rt.modes.listText()))
				return nil
			}
			if err := m.rt.switchMode(name); err != nil {
				m.logs = append(m.logs, m.styles.warn.Render(err.Error()))
				return nil
			}
			mode := m.rt.activeMode()
			m.logs = append(m.logs, m.styles.log.Render(fmt.Sprintf("已切换到 %s mode：%s", mode.Name, mode.Description)))
			return nil
		},
	})
	registry.register(slashCommand{
		Name:        "quit",
		Usage:       "/quit",
		Description: "退出 TUI",
		Handler: func(_ *tuiModel, _ string) tea.Cmd {
			return tea.Quit
		},
	})
	return registry
}

func (r *slashCommandRegistry) register(command slashCommand) {
	name := strings.TrimPrefix(strings.TrimSpace(command.Name), "/")
	if name == "" || command.Handler == nil {
		return
	}
	command.Name = name
	if _, ok := r.commands[name]; !ok {
		r.order = append(r.order, name)
		sort.Strings(r.order)
	}
	r.commands[name] = command
}

func (r *slashCommandRegistry) get(name string) (slashCommand, bool) {
	command, ok := r.commands[strings.TrimPrefix(name, "/")]
	return command, ok
}

func (r *slashCommandRegistry) complete(prefix string) []slashCommand {
	prefix = strings.TrimPrefix(prefix, "/")
	candidates := []slashCommand{}
	for _, name := range r.order {
		if strings.HasPrefix(name, prefix) {
			candidates = append(candidates, r.commands[name])
		}
	}
	return candidates
}

func (r *slashCommandRegistry) helpText() string {
	lines := []string{"可用命令："}
	for _, name := range r.order {
		command := r.commands[name]
		lines = append(lines, fmt.Sprintf("  %-10s %s", command.Usage, command.Description))
	}
	return strings.Join(lines, "\n")
}

func formatSessionList(records []sessionRecord) string {
	if len(records) == 0 {
		return "没有可恢复的 session。"
	}
	lines := []string{"可恢复的 session："}
	for i, record := range records {
		updated := "unknown"
		if !record.UpdatedAt.IsZero() {
			updated = record.UpdatedAt.Format("2006-01-02 15:04")
		}
		lines = append(lines, fmt.Sprintf("  %d. %s  messages=%d  updated=%s  %s", i+1, record.ID, record.MessageCount, updated, record.Preview))
	}
	lines = append(lines, "使用 /resume 1 或 /resume <session-id> 恢复。")
	return strings.Join(lines, "\n")
}

func resolveSessionChoice(choice string, records []sessionRecord) string {
	choice = strings.TrimSpace(choice)
	if n, err := strconv.Atoi(choice); err == nil && n >= 1 && n <= len(records) {
		return records[n-1].ID
	}
	return choice
}

func parseSlashCommand(input string) (string, string, bool) {
	text := strings.TrimSpace(input)
	if !strings.HasPrefix(text, "/") {
		return "", "", false
	}
	text = strings.TrimPrefix(text, "/")
	separator := strings.IndexFunc(text, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
	if separator < 0 {
		return text, "", true
	}
	return text[:separator], strings.TrimSpace(text[separator:]), true
}

func slashCommandPrefix(input string) (string, bool) {
	text := strings.TrimLeft(input, " \t\r\n")
	if !strings.HasPrefix(text, "/") {
		return "", false
	}
	token := strings.Fields(text)
	if len(token) == 0 {
		return "", false
	}
	return strings.TrimPrefix(token[0], "/"), true
}
