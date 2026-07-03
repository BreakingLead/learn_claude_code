package agent

// 模块说明：
// 这个文件实现后台 shell 任务和完成通知。普通 bash 工具会阻塞当前轮；
// background_bash 则立即返回 job id，命令在 goroutine 中执行。agent loop
// 每次调用模型前都会收集已完成 job，并注入一条内部 <background> 消息。
//
// 设计边界：
//   - 后台任务只保存在 agentRuntime 持有的 registry 中，不使用全局变量。
//   - registry 只负责进程状态和完成通知，不直接修改对话历史。
//   - 注入消息由 agent loop 显式调用 injectBackgroundNotifications 完成。

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

type backgroundRegistry struct {
	mu        sync.Mutex
	nextID    int
	jobs      map[string]*backgroundJob
	completed []backgroundResult
	emit      func(format string, args ...any)
}

type backgroundJob struct {
	ID        string
	Command   string
	Status    string
	Output    string
	Error     string
	Timeout   time.Duration
	StartedAt time.Time
	EndedAt   time.Time
}

type backgroundResult struct {
	ID      string
	Command string
	Output  string
	Error   string
}

// newBackgroundRegistry 创建后台任务注册表，并注入日志出口。
func newBackgroundRegistry(emit func(format string, args ...any)) *backgroundRegistry {
	return &backgroundRegistry{
		jobs: make(map[string]*backgroundJob),
		emit: emit,
	}
}

// runBackgroundBash 启动后台 bash 命令并立即返回 job id。
func (rt *agentRuntime) runBackgroundBash(raw json.RawMessage) string {
	var input struct {
		Command        string `json:"command"`
		TimeoutSeconds *int   `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	command := strings.TrimSpace(input.Command)
	if command == "" {
		return "Error: command is required"
	}
	timeout, err := backgroundTimeout(rt.config.BackgroundTimeout, input.TimeoutSeconds)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	job := rt.background.start(command, rt.config.Workdir, timeout)
	return fmt.Sprintf("Started background job %s with timeout %s: %s", job.ID, job.Timeout, job.Command)
}

// runBackgroundStatus 返回单个后台 job 的当前状态和输出预览。
func (rt *agentRuntime) runBackgroundStatus(raw json.RawMessage) string {
	var input struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	job, ok := rt.background.status(strings.TrimSpace(input.ID))
	if !ok {
		return fmt.Sprintf("Error: background job not found: %s", input.ID)
	}
	return formatBackgroundJob(job)
}

// runBackgroundList 列出所有后台 job 的简要状态。
func (rt *agentRuntime) runBackgroundList(raw json.RawMessage) string {
	jobs := rt.background.list()
	if len(jobs) == 0 {
		return "(no background jobs)"
	}
	var lines []string
	for _, job := range jobs {
		lines = append(lines, fmt.Sprintf("- %s [%s] %s", job.ID, job.Status, job.Command))
	}
	return strings.Join(lines, "\n")
}

// injectBackgroundNotifications 将已完成后台任务作为内部消息注入对话历史。
func (rt *agentRuntime) injectBackgroundNotifications(messages *[]anthropic.MessageParam) {
	results := rt.background.drainCompleted()
	if len(results) == 0 {
		return
	}
	var lines []string
	lines = append(lines, "<background>")
	lines = append(lines, "Completed background jobs:")
	for _, result := range results {
		lines = append(lines, fmt.Sprintf("## %s\nCommand: %s\nError: %s\nOutput:\n%s", result.ID, result.Command, result.Error, truncate(result.Output, 4000)))
	}
	lines = append(lines, "</background>")
	*messages = append(*messages, anthropic.NewUserMessage(anthropic.NewTextBlock(strings.Join(lines, "\n"))))
	rt.emitLine("[background] injected %d completed job notifications", len(results))
}

// start 创建 job 并在 goroutine 中运行 shell 命令。
func (r *backgroundRegistry) start(command string, workdir string, timeout time.Duration) backgroundJob {
	r.mu.Lock()
	r.nextID++
	id := fmt.Sprintf("bg-%d", r.nextID)
	job := &backgroundJob{
		ID:        id,
		Command:   command,
		Status:    "running",
		Timeout:   timeout,
		StartedAt: time.Now(),
	}
	r.jobs[id] = job
	r.mu.Unlock()

	if r.emit != nil {
		r.emit("[background] started %s", id)
	}

	go r.run(job, workdir, timeout)
	return *job
}

// run 执行 shell 命令，记录输出，并把完成事件放入队列。
func (r *backgroundRegistry) run(job *backgroundJob, workdir string, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", job.Command)
	cmd.Dir = workdir
	out, err := cmd.CombinedOutput()

	r.mu.Lock()
	defer r.mu.Unlock()
	job.EndedAt = time.Now()
	job.Output = strings.TrimSpace(string(out))
	if ctx.Err() == context.DeadlineExceeded {
		job.Status = "timeout"
		job.Error = fmt.Sprintf("background job timed out after %s", timeout)
	} else if err != nil {
		job.Status = "failed"
		job.Error = err.Error()
	} else {
		job.Status = "completed"
	}
	r.completed = append(r.completed, backgroundResult{
		ID:      job.ID,
		Command: job.Command,
		Output:  job.Output,
		Error:   job.Error,
	})
	if r.emit != nil {
		r.emit("[background] %s %s", job.ID, job.Status)
	}
}

func backgroundTimeout(defaultTimeout time.Duration, overrideSeconds *int) (time.Duration, error) {
	if defaultTimeout <= 0 {
		defaultTimeout = 10 * time.Minute
	}
	if overrideSeconds == nil {
		return defaultTimeout, nil
	}
	if *overrideSeconds <= 0 {
		return 0, fmt.Errorf("timeout_seconds must be positive")
	}
	return time.Duration(*overrideSeconds) * time.Second, nil
}

// status 返回指定 job 的快照。
func (r *backgroundRegistry) status(id string) (backgroundJob, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[id]
	if !ok {
		return backgroundJob{}, false
	}
	return *job, true
}

// list 返回所有后台 job 的快照。
func (r *backgroundRegistry) list() []backgroundJob {
	r.mu.Lock()
	defer r.mu.Unlock()
	jobs := make([]backgroundJob, 0, len(r.jobs))
	for _, job := range r.jobs {
		jobs = append(jobs, *job)
	}
	return jobs
}

// drainCompleted 取出并清空已完成 job 的通知队列。
func (r *backgroundRegistry) drainCompleted() []backgroundResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	results := append([]backgroundResult(nil), r.completed...)
	r.completed = nil
	return results
}

// formatBackgroundJob 格式化单个后台 job 的状态和输出。
func formatBackgroundJob(job backgroundJob) string {
	return fmt.Sprintf("Job: %s\nStatus: %s\nTimeout: %s\nCommand: %s\nError: %s\nOutput:\n%s",
		job.ID,
		job.Status,
		job.Timeout,
		job.Command,
		job.Error,
		truncate(job.Output, 2000),
	)
}
