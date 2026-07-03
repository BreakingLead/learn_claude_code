package agent

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCronMatchesFiveFieldExpressions(t *testing.T) {
	mondayNine := time.Date(2026, 7, 6, 9, 0, 0, 0, time.Local)
	if !cronMatches("0 9 * * 1-5", mondayNine) {
		t.Fatal("expected weekday 9:00 cron to match")
	}
	if cronMatches("*/5 * * * *", mondayNine.Add(1*time.Minute)) {
		t.Fatal("expected */5 not to match minute 1")
	}
	if !cronMatches("0 9 1 * 1", mondayNine) {
		t.Fatal("expected constrained DOM/DOW to use OR semantics")
	}
}

func TestValidateCronRejectsInvalidExpressions(t *testing.T) {
	for _, expr := range []string{"* * *", "60 * * * *", "*/0 * * * *", "* * 0 * *"} {
		if err := validateCron(expr); err == nil {
			t.Fatalf("expected invalid cron %q to fail", expr)
		}
	}
}

func TestCronSchedulerQueuesOncePerMinute(t *testing.T) {
	scheduler := newCronScheduler("", nil, nil)
	job, err := scheduler.schedule("* * * * *", "run tests", true, false)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 3, 9, 30, 1, 0, time.Local)
	if queued := scheduler.checkDue(now); queued != 1 {
		t.Fatalf("expected first check to queue one job, got %d", queued)
	}
	if queued := scheduler.checkDue(now.Add(20 * time.Second)); queued != 0 {
		t.Fatalf("expected same minute not to queue again, got %d", queued)
	}
	jobs := scheduler.consumeQueue()
	if len(jobs) != 1 || jobs[0].ID != job.ID {
		t.Fatalf("unexpected queued jobs: %+v", jobs)
	}
}

func TestCronSchedulerRemovesOneShotJobAfterQueue(t *testing.T) {
	scheduler := newCronScheduler("", nil, nil)
	if _, err := scheduler.schedule("* * * * *", "once", false, false); err != nil {
		t.Fatal(err)
	}
	if queued := scheduler.checkDue(time.Date(2026, 7, 3, 9, 30, 0, 0, time.Local)); queued != 1 {
		t.Fatalf("expected one queued job, got %d", queued)
	}
	if jobs := scheduler.list(); len(jobs) != 0 {
		t.Fatalf("expected one-shot job to be removed, got %+v", jobs)
	}
}

func TestCronSchedulerPersistsDurableJobs(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".scheduled_tasks.json")
	scheduler := newCronScheduler(path, nil, nil)
	job, err := scheduler.schedule("*/5 * * * *", "check ci", true, true)
	if err != nil {
		t.Fatal(err)
	}

	loaded := newCronScheduler(path, nil, nil)
	if err := loaded.loadDurableJobs(); err != nil {
		t.Fatal(err)
	}
	jobs := loaded.list()
	if len(jobs) != 1 || jobs[0].ID != job.ID {
		t.Fatalf("unexpected loaded jobs: %+v", jobs)
	}
}

func TestScheduleCronToolDefaultsToDurableRecurring(t *testing.T) {
	rt := newAgentRuntime(testConfig(t.TempDir()), nil, nil)
	result := rt.runScheduleCron([]byte(`{"cron":"*/5 * * * *","prompt":"check ci"}`))
	if !strings.Contains(result, "Scheduled cron_") {
		t.Fatalf("unexpected schedule result: %s", result)
	}
	jobs := rt.cron.list()
	if len(jobs) != 1 || !jobs[0].Recurring || !jobs[0].Durable {
		t.Fatalf("expected durable recurring job, got %+v", jobs)
	}
}
