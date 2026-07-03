package agent

// 模块说明：
// cron scheduler 负责“什么时候触发”，agent runner 只负责“如何执行”。
// 调度线程把到点的任务放入队列；TUI queue processor 在 agent 空闲时启动一轮执行；
// agentLoop 开始时消费队列，把任务注入为 `[Scheduled] ...` 用户消息。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

type cronJob struct {
	ID        string `json:"id"`
	Cron      string `json:"cron"`
	Prompt    string `json:"prompt"`
	Recurring bool   `json:"recurring"`
	Durable   bool   `json:"durable"`
}

type cronScheduler struct {
	mu        sync.Mutex
	jobs      map[string]cronJob
	queue     []cronJob
	lastFired map[string]string
	path      string
	started   bool
	emit      func(format string, args ...any)
	notify    func(count int)
}

func newCronScheduler(path string, emit func(format string, args ...any), notify func(count int)) *cronScheduler {
	return &cronScheduler{
		jobs:      map[string]cronJob{},
		lastFired: map[string]string{},
		path:      path,
		emit:      emit,
		notify:    notify,
	}
}

func (s *cronScheduler) start() {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return
	}
	s.started = true
	s.mu.Unlock()

	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for now := range ticker.C {
			if queued := s.checkDue(now); queued > 0 && s.notify != nil {
				s.notify(queued)
			}
		}
	}()
}

func (s *cronScheduler) schedule(cronExpr string, prompt string, recurring bool, durable bool) (cronJob, error) {
	cronExpr = strings.TrimSpace(cronExpr)
	prompt = strings.TrimSpace(prompt)
	if err := validateCron(cronExpr); err != nil {
		return cronJob{}, err
	}
	if prompt == "" {
		return cronJob{}, fmt.Errorf("prompt is required")
	}
	job := cronJob{
		ID:        fmt.Sprintf("cron_%d", time.Now().UnixNano()),
		Cron:      cronExpr,
		Prompt:    prompt,
		Recurring: recurring,
		Durable:   durable,
	}

	s.mu.Lock()
	s.jobs[job.ID] = job
	err := s.saveDurableJobsLocked()
	s.mu.Unlock()
	if err != nil {
		return cronJob{}, err
	}
	return job, nil
}

func (s *cronScheduler) cancel(id string) bool {
	id = strings.TrimSpace(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.jobs[id]; !ok {
		return false
	}
	delete(s.jobs, id)
	delete(s.lastFired, id)
	_ = s.saveDurableJobsLocked()
	return true
}

func (s *cronScheduler) list() []cronJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	jobs := make([]cronJob, 0, len(s.jobs))
	for _, job := range s.jobs {
		jobs = append(jobs, job)
	}
	return jobs
}

func (s *cronScheduler) hasQueue() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.queue) > 0
}

func (s *cronScheduler) queueCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.queue)
}

func (s *cronScheduler) consumeQueue() []cronJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	jobs := append([]cronJob(nil), s.queue...)
	s.queue = nil
	return jobs
}

func (s *cronScheduler) checkDue(now time.Time) int {
	marker := now.Format("2006-01-02 15:04")
	var queued int
	var saveNeeded bool

	s.mu.Lock()
	defer s.mu.Unlock()
	for id, job := range s.jobs {
		if !cronMatches(job.Cron, now) || s.lastFired[id] == marker {
			continue
		}
		s.queue = append(s.queue, job)
		s.lastFired[id] = marker
		queued++
		if !job.Recurring {
			delete(s.jobs, id)
			saveNeeded = true
		}
	}
	if saveNeeded {
		_ = s.saveDurableJobsLocked()
	}
	return queued
}

func (s *cronScheduler) loadDurableJobs() error {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var jobs []cronJob
	if err := json.Unmarshal(raw, &jobs); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, job := range jobs {
		if !job.Durable || validateCron(job.Cron) != nil || strings.TrimSpace(job.Prompt) == "" {
			continue
		}
		s.jobs[job.ID] = job
	}
	return nil
}

func (s *cronScheduler) saveDurableJobsLocked() error {
	if s.path == "" {
		return nil
	}
	var durable []cronJob
	for _, job := range s.jobs {
		if job.Durable {
			durable = append(durable, job)
		}
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(durable, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, raw, 0o644)
}

func cronMatches(cronExpr string, dt time.Time) bool {
	fields := strings.Fields(cronExpr)
	if len(fields) != 5 {
		return false
	}
	minute, hour, dom, month, dow := fields[0], fields[1], fields[2], fields[3], fields[4]
	if !cronFieldMatches(minute, dt.Minute(), 0, 59) ||
		!cronFieldMatches(hour, dt.Hour(), 0, 23) ||
		!cronFieldMatches(month, int(dt.Month()), 1, 12) {
		return false
	}

	domOK := cronFieldMatches(dom, dt.Day(), 1, 31)
	dowOK := cronFieldMatches(dow, int(dt.Weekday()), 0, 6)
	domAny := dom == "*"
	dowAny := dow == "*"
	switch {
	case domAny && dowAny:
		return true
	case domAny:
		return dowOK
	case dowAny:
		return domOK
	default:
		return domOK || dowOK
	}
}

func validateCron(cronExpr string) error {
	fields := strings.Fields(cronExpr)
	if len(fields) != 5 {
		return fmt.Errorf("cron must have 5 fields")
	}
	ranges := [][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 6}}
	for i, field := range fields {
		if err := validateCronField(field, ranges[i][0], ranges[i][1]); err != nil {
			return fmt.Errorf("field %d: %w", i+1, err)
		}
	}
	return nil
}

func validateCronField(field string, minValue int, maxValue int) error {
	if field == "" {
		return fmt.Errorf("empty field")
	}
	for _, part := range strings.Split(field, ",") {
		if part == "" {
			return fmt.Errorf("empty list item")
		}
		if strings.HasPrefix(part, "*/") {
			step, err := strconv.Atoi(strings.TrimPrefix(part, "*/"))
			if err != nil || step <= 0 {
				return fmt.Errorf("invalid step %q", part)
			}
			continue
		}
		if strings.Contains(part, "-") {
			bounds := strings.Split(part, "-")
			if len(bounds) != 2 {
				return fmt.Errorf("invalid range %q", part)
			}
			start, err1 := strconv.Atoi(bounds[0])
			end, err2 := strconv.Atoi(bounds[1])
			if err1 != nil || err2 != nil || start > end || start < minValue || end > maxValue {
				return fmt.Errorf("invalid range %q", part)
			}
			continue
		}
		if part == "*" {
			continue
		}
		value, err := strconv.Atoi(part)
		if err != nil || value < minValue || value > maxValue {
			return fmt.Errorf("invalid value %q", part)
		}
	}
	return nil
}

func cronFieldMatches(field string, value int, minValue int, maxValue int) bool {
	for _, part := range strings.Split(field, ",") {
		if cronFieldPartMatches(part, value, minValue, maxValue) {
			return true
		}
	}
	return false
}

func cronFieldPartMatches(part string, value int, minValue int, maxValue int) bool {
	if part == "*" {
		return true
	}
	if strings.HasPrefix(part, "*/") {
		step, err := strconv.Atoi(strings.TrimPrefix(part, "*/"))
		return err == nil && step > 0 && (value-minValue)%step == 0
	}
	if strings.Contains(part, "-") {
		bounds := strings.Split(part, "-")
		if len(bounds) != 2 {
			return false
		}
		start, err1 := strconv.Atoi(bounds[0])
		end, err2 := strconv.Atoi(bounds[1])
		return err1 == nil && err2 == nil && value >= start && value <= end
	}
	n, err := strconv.Atoi(part)
	return err == nil && value >= minValue && value <= maxValue && value == n
}

func (rt *agentRuntime) runScheduleCron(raw json.RawMessage) string {
	var input struct {
		Cron      string `json:"cron"`
		Prompt    string `json:"prompt"`
		Recurring *bool  `json:"recurring"`
		Durable   *bool  `json:"durable"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	recurring := true
	if input.Recurring != nil {
		recurring = *input.Recurring
	}
	durable := true
	if input.Durable != nil {
		durable = *input.Durable
	}
	job, err := rt.cron.schedule(input.Cron, input.Prompt, recurring, durable)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return fmt.Sprintf("Scheduled %s: %s -> %s", job.ID, job.Cron, job.Prompt)
}

func (rt *agentRuntime) runListCrons(raw json.RawMessage) string {
	jobs := rt.cron.list()
	if len(jobs) == 0 {
		return "(no scheduled cron jobs)"
	}
	var lines []string
	for _, job := range jobs {
		lines = append(lines, fmt.Sprintf("- %s recurring=%t durable=%t cron=%q prompt=%q", job.ID, job.Recurring, job.Durable, job.Cron, job.Prompt))
	}
	return strings.Join(lines, "\n")
}

func (rt *agentRuntime) runCancelCron(raw json.RawMessage) string {
	var input struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	if !rt.cron.cancel(input.ID) {
		return fmt.Sprintf("Error: cron job not found: %s", input.ID)
	}
	return fmt.Sprintf("Cancelled cron job %s", input.ID)
}

func (rt *agentRuntime) injectScheduledCronMessages(messages *[]anthropic.MessageParam) {
	if rt == nil || rt.cron == nil {
		return
	}
	jobs := rt.cron.consumeQueue()
	if len(jobs) == 0 {
		return
	}
	for _, job := range jobs {
		*messages = append(*messages, anthropic.NewUserMessage(
			anthropic.NewTextBlock(fmt.Sprintf("[Scheduled] %s", job.Prompt)),
		))
	}
	rt.emitLine("[cron] injected %d scheduled task(s)", len(jobs))
}
