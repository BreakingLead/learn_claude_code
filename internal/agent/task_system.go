package agent

// 模块说明：
// 这个文件实现跨会话任务系统。它和 todo_write 不同：todo_write 是当前轮
// 的短期执行清单；.tasks/ 是长期任务图，记录任务、父子关系、状态和索引。
//
// 数据布局：
//   - .tasks/TASKS.md 是任务索引，方便人读和 system prompt 按需加载。
//   - .tasks/*.md 是单个任务文件，使用 frontmatter 保存 id、title、status、parent_id。
//
// 状态边界：
// 所有路径来自 agentConfig，所有工具函数挂在 agentRuntime 上，不使用包级变量。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type taskRecord struct {
	ID       string
	Title    string
	Status   string
	ParentID string
	Summary  string
	Content  string
	Path     string
}

// runTaskCreate 创建一条持久任务，并重建 TASKS.md 索引。
func (rt *agentRuntime) runTaskCreate(raw json.RawMessage) string {
	var input struct {
		Title    string `json:"title"`
		Summary  string `json:"summary"`
		Content  string `json:"content"`
		ParentID string `json:"parent_id"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return "Error: title is required"
	}

	task := taskRecord{
		ID:       taskID(title, input.Summary, time.Now()),
		Title:    title,
		Status:   "pending",
		ParentID: strings.TrimSpace(input.ParentID),
		Summary:  strings.TrimSpace(input.Summary),
		Content:  strings.TrimSpace(input.Content),
	}
	if task.Content == "" {
		task.Content = task.Summary
	}
	if err := rt.writeTask(task); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	rt.rebuildTaskIndex()
	return fmt.Sprintf("Created task %s", task.ID)
}

// runTaskUpdate 更新持久任务的状态、标题、摘要或正文。
func (rt *agentRuntime) runTaskUpdate(raw json.RawMessage) string {
	var input struct {
		ID      string `json:"id"`
		Title   string `json:"title"`
		Status  string `json:"status"`
		Summary string `json:"summary"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	id := strings.TrimSpace(input.ID)
	if id == "" {
		return "Error: id is required"
	}

	task, ok := rt.loadTask(id)
	if !ok {
		return fmt.Sprintf("Error: task not found: %s", id)
	}
	if strings.TrimSpace(input.Title) != "" {
		task.Title = strings.TrimSpace(input.Title)
	}
	if strings.TrimSpace(input.Status) != "" {
		task.Status = strings.TrimSpace(input.Status)
	}
	if strings.TrimSpace(input.Summary) != "" {
		task.Summary = strings.TrimSpace(input.Summary)
	}
	if strings.TrimSpace(input.Content) != "" {
		task.Content = strings.TrimSpace(input.Content)
	}
	if err := rt.writeTask(task); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	rt.rebuildTaskIndex()
	return fmt.Sprintf("Updated task %s", id)
}

// runTaskList 返回当前 TASKS.md 索引，索引不存在时按任务文件重建。
func (rt *agentRuntime) runTaskList(raw json.RawMessage) string {
	if _, err := os.Stat(rt.config.TaskIndex); os.IsNotExist(err) {
		rt.rebuildTaskIndex()
	}
	rawIndex, err := os.ReadFile(rt.config.TaskIndex)
	if err != nil {
		return "(no tasks)"
	}
	return strings.TrimSpace(string(rawIndex))
}

// loadTask 根据任务 ID 读取单个任务文件。
func (rt *agentRuntime) loadTask(id string) (taskRecord, bool) {
	path := filepath.Join(rt.config.TaskDir, safeFilename(id)+".md")
	raw, err := os.ReadFile(path)
	if err != nil {
		return taskRecord{}, false
	}
	task, ok := parseTaskRecord(path, string(raw))
	return task, ok
}

// loadTaskRecords 读取 .tasks/ 下除 TASKS.md 外的所有任务文件。
func (rt *agentRuntime) loadTaskRecords() []taskRecord {
	entries, err := os.ReadDir(rt.config.TaskDir)
	if err != nil {
		return nil
	}
	var tasks []taskRecord
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "TASKS.md" || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(rt.config.TaskDir, entry.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		task, ok := parseTaskRecord(path, string(raw))
		if ok {
			tasks = append(tasks, task)
		}
	}
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].ID < tasks[j].ID
	})
	return tasks
}

// parseTaskRecord 将一份任务 markdown 解析为 taskRecord。
func parseTaskRecord(path string, raw string) (taskRecord, bool) {
	meta, body := parseFrontmatter(raw)
	id := strings.TrimSpace(meta["id"])
	if id == "" {
		id = strings.TrimSuffix(filepath.Base(path), ".md")
	}
	title := strings.TrimSpace(meta["title"])
	if title == "" {
		title = id
	}
	content := strings.TrimSpace(body)
	return taskRecord{
		ID:       id,
		Title:    title,
		Status:   strings.TrimSpace(meta["status"]),
		ParentID: strings.TrimSpace(meta["parent_id"]),
		Summary:  strings.TrimSpace(meta["summary"]),
		Content:  content,
		Path:     path,
	}, true
}

// writeTask 将任务写成带 frontmatter 的 markdown 文件。
func (rt *agentRuntime) writeTask(task taskRecord) error {
	if err := os.MkdirAll(rt.config.TaskDir, 0o755); err != nil {
		return err
	}
	if task.Status == "" {
		task.Status = "pending"
	}
	path := filepath.Join(rt.config.TaskDir, safeFilename(task.ID)+".md")
	body := fmt.Sprintf("---\nid: %s\ntitle: %q\nstatus: %s\nparent_id: %q\nsummary: %q\nupdated: %s\n---\n\n%s\n",
		task.ID,
		task.Title,
		task.Status,
		task.ParentID,
		task.Summary,
		time.Now().Format(time.RFC3339),
		task.Content,
	)
	return os.WriteFile(path, []byte(body), 0o644)
}

// rebuildTaskIndex 根据任务文件重建 .tasks/TASKS.md。
func (rt *agentRuntime) rebuildTaskIndex() {
	tasks := rt.loadTaskRecords()
	if err := os.MkdirAll(rt.config.TaskDir, 0o755); err != nil {
		return
	}
	var lines []string
	lines = append(lines, "# Task Index", "")
	if len(tasks) == 0 {
		lines = append(lines, "(no tasks)")
	} else {
		for _, task := range tasks {
			indent := ""
			if task.ParentID != "" {
				indent = "  "
			}
			lines = append(lines, fmt.Sprintf("%s- [%s] %s (%s): %s", indent, task.Status, task.Title, task.ID, task.Summary))
		}
	}
	_ = os.WriteFile(rt.config.TaskIndex, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

// taskID 生成稳定前缀加时间后缀的任务 ID，避免标题重复覆盖。
func taskID(title string, summary string, now time.Time) string {
	base := safeFilename(strings.ToLower(title))
	if len(base) > 40 {
		base = base[:40]
	}
	return fmt.Sprintf("%s-%d", base, now.UnixNano())
}
