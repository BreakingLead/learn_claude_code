package agent

import (
	"testing"
	"time"
)

// TestRecoveryDelayCapsExponentialBackoff 验证恢复退避会指数增长并受上限约束。
func TestRecoveryDelayCapsExponentialBackoff(t *testing.T) {
	base := 100 * time.Millisecond
	maxDelay := 250 * time.Millisecond
	if got := recoveryDelay(1, base, maxDelay); got != base {
		t.Fatalf("attempt 1 delay = %s", got)
	}
	if got := recoveryDelay(2, base, maxDelay); got != 200*time.Millisecond {
		t.Fatalf("attempt 2 delay = %s", got)
	}
	if got := recoveryDelay(3, base, maxDelay); got != maxDelay {
		t.Fatalf("attempt 3 delay = %s", got)
	}
}
