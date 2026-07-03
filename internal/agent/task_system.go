package agent

// 模块说明：
// 这个文件实现跨会话任务系统。它和 todo_write 不同：todo_write 是当前轮
// 的短期执行清单；.agents/.tasks/ 是长期任务图，记录任务、认领人、状态和依赖。
//
// 数据布局：
//   - .agents/.tasks/TASKS.md 是任务索引，方便人读和 system prompt 按需加载。
//   - .agents/.tasks/*.json 是单个任务文件，保存 id、subject、description、status、owner、blockedBy。
//
// 状态边界：
// 所有路径来自 agentConfig，所有工具函数挂在 agentRuntime 上，不使用包级变量。

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type taskRecord struct {
	ID          string   `json:"id"`
	Subject     string   `json:"subject"`
	Description string   `json:"description"`
	Status      string   `json:"status"`
	Owner       string   `json:"owner"`
	BlockedBy   []string `json:"blockedBy"`
	Path        string   `json:"-"`
}

// runTaskCreate 创建一条持久任务，并写入 .agents/.tasks/{id}.json。
func (rt *agentRuntime) runTaskCreate(raw json.RawMessage) string {
	var input struct {
		Subject     string   `json:"subject"`
		Description string   `json:"description"`
		BlockedBy   []string `json:"blockedBy"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	subject := strings.TrimSpace(input.Subject)
	if subject == "" {
		return "Error: subject is required"
	}

	task := taskRecord{
		ID:          taskID(time.Now()),
		Subject:     subject,
		Description: strings.TrimSpace(input.Description),
		Status:      "pending",
		BlockedBy:   cleanTaskIDs(input.BlockedBy),
	}
	if err := rt.writeTask(task); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	rt.rebuildTaskIndex()
	return fmt.Sprintf("Created %s (%s)", task.ID, task.Subject)
}

// runTaskList 返回当前 TASKS.md 索引，索引不存在时按任务 JSON 文件重建。
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

// runTaskGet 返回单个任务的完整 JSON，供跨会话恢复时读取细节。
func (rt *agentRuntime) runTaskGet(raw json.RawMessage) string {
	var input struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	task, ok := rt.loadTask(input.ID)
	if !ok {
		return fmt.Sprintf("Error: task not found: %s", input.ID)
	}
	return formatTaskJSON(task)
}

// runTaskClaim 将 pending 任务认领为 in_progress，依赖未完成时拒绝。
func (rt *agentRuntime) runTaskClaim(raw json.RawMessage) string {
	var input struct {
		ID    string `json:"id"`
		Owner string `json:"owner"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	id := strings.TrimSpace(input.ID)
	if id == "" {
		return "Error: id is required"
	}
	owner := strings.TrimSpace(input.Owner)
	if owner == "" {
		owner = "agent"
	}

	task, ok := rt.loadTask(id)
	if !ok {
		return fmt.Sprintf("Error: task not found: %s", id)
	}
	if task.Status != "pending" {
		return fmt.Sprintf("Task %s is %s, cannot claim", task.ID, task.Status)
	}
	if blocked := rt.blockingDependencies(task); len(blocked) > 0 {
		return fmt.Sprintf("Blocked by: %s", strings.Join(blocked, ", "))
	}

	task.Owner = owner
	task.Status = "in_progress"
	if err := rt.writeTask(task); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	rt.rebuildTaskIndex()
	return fmt.Sprintf("Claimed %s (%s)", task.ID, task.Subject)
}

// runTaskComplete 将任务标记为 completed，并报告因此被解锁的 pending 任务。
func (rt *agentRuntime) runTaskComplete(raw json.RawMessage) string {
	var input struct {
		ID string `json:"id"`
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
	task.Status = "completed"
	if err := rt.writeTask(task); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	var unblocked []string
	for _, candidate := range rt.loadTaskRecords() {
		if candidate.ID == task.ID || candidate.Status != "pending" || len(candidate.BlockedBy) == 0 {
			continue
		}
		if len(rt.blockingDependencies(candidate)) == 0 {
			unblocked = append(unblocked, candidate.Subject)
		}
	}
	rt.rebuildTaskIndex()

	result := fmt.Sprintf("Completed %s (%s)", task.ID, task.Subject)
	if len(unblocked) > 0 {
		result += "\nUnblocked: " + strings.Join(unblocked, ", ")
	}
	return result
}

// loadTask 根据任务 ID 读取单个 JSON 任务文件。
func (rt *agentRuntime) loadTask(id string) (taskRecord, bool) {
	path := filepath.Join(rt.config.TaskDir, safeFilename(strings.TrimSpace(id))+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return taskRecord{}, false
	}
	task, ok := parseTaskRecord(path, raw)
	return task, ok
}

// loadTaskRecords 读取 .agents/.tasks/ 下除 TASKS.md 外的所有 JSON 任务文件。
func (rt *agentRuntime) loadTaskRecords() []taskRecord {
	entries, err := os.ReadDir(rt.config.TaskDir)
	if err != nil {
		return nil
	}
	var tasks []taskRecord
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(rt.config.TaskDir, entry.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		task, ok := parseTaskRecord(path, raw)
		if ok {
			tasks = append(tasks, task)
		}
	}
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].ID < tasks[j].ID
	})
	return tasks
}

// parseTaskRecord 将一份任务 JSON 解析为 taskRecord，并补齐默认状态。
func parseTaskRecord(path string, raw []byte) (taskRecord, bool) {
	var task taskRecord
	if err := json.Unmarshal(raw, &task); err != nil {
		return taskRecord{}, false
	}
	task.ID = strings.TrimSpace(task.ID)
	if task.ID == "" {
		task.ID = strings.TrimSuffix(filepath.Base(path), ".json")
	}
	task.Subject = strings.TrimSpace(task.Subject)
	if task.Subject == "" {
		task.Subject = task.ID
	}
	task.Description = strings.TrimSpace(task.Description)
	task.Status = normalizeTaskStatus(task.Status)
	task.Owner = strings.TrimSpace(task.Owner)
	task.BlockedBy = cleanTaskIDs(task.BlockedBy)
	task.Path = path
	return task, true
}

// writeTask 将任务写成格式化 JSON 文件。
func (rt *agentRuntime) writeTask(task taskRecord) error {
	if err := os.MkdirAll(rt.config.TaskDir, 0o755); err != nil {
		return err
	}
	task.ID = strings.TrimSpace(task.ID)
	if task.ID == "" {
		task.ID = taskID(time.Now())
	}
	task.Subject = strings.TrimSpace(task.Subject)
	if task.Subject == "" {
		task.Subject = task.ID
	}
	task.Description = strings.TrimSpace(task.Description)
	task.Status = normalizeTaskStatus(task.Status)
	task.Owner = strings.TrimSpace(task.Owner)
	task.BlockedBy = cleanTaskIDs(task.BlockedBy)

	path := filepath.Join(rt.config.TaskDir, safeFilename(task.ID)+".json")
	raw, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}

// blockingDependencies 返回所有缺失或尚未 completed 的依赖任务 ID。
func (rt *agentRuntime) blockingDependencies(task taskRecord) []string {
	var blocked []string
	for _, depID := range task.BlockedBy {
		dep, ok := rt.loadTask(depID)
		if !ok || dep.Status != "completed" {
			blocked = append(blocked, depID)
		}
	}
	return blocked
}

// rebuildTaskIndex 根据任务 JSON 文件重建 .agents/.tasks/TASKS.md。
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
			owner := ""
			if task.Owner != "" {
				owner = " owner=" + task.Owner
			}
			blockedBy := ""
			if len(task.BlockedBy) > 0 {
				blockedBy = " blockedBy=" + strings.Join(task.BlockedBy, ",")
			}
			lines = append(lines, fmt.Sprintf("- [%s] %s (%s)%s%s", task.Status, task.Subject, task.ID, owner, blockedBy))
		}
	}
	_ = os.WriteFile(rt.config.TaskIndex, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

// taskID 生成 timestamp 加短后缀的任务 ID，便于跨会话引用。
func taskID(now time.Time) string {
	var suffix [2]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return fmt.Sprintf("task_%d_%04x", now.Unix(), now.Nanosecond()&0xffff)
	}
	return fmt.Sprintf("task_%d_%x", now.Unix(), suffix)
}

// cleanTaskIDs 清理依赖 ID 列表，去掉空值和重复项并保持输入顺序。
func cleanTaskIDs(ids []string) []string {
	seen := make(map[string]struct{})
	var cleaned []string
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		cleaned = append(cleaned, id)
	}
	return cleaned
}

// normalizeTaskStatus 将未知状态收敛为 pending。
func normalizeTaskStatus(status string) string {
	switch strings.TrimSpace(status) {
	case "pending":
		return "pending"
	case "in_progress":
		return "in_progress"
	case "completed":
		return "completed"
	default:
		return "pending"
	}
}

// formatTaskJSON 返回不包含本地 Path 字段的任务 JSON。
func formatTaskJSON(task taskRecord) string {
	task.Path = ""
	raw, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return string(raw)
}
