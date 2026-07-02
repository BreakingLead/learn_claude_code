package agent

import (
	"fmt"
	"os"
)

// ── 辅助函数 ──────────────────────────────────────────

func getEnvOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// ── 终端颜色 ──────────────────────────────────────────

func colorBold(s string) string   { return fmt.Sprintf("\033[1m%s\033[0m", s) }
func colorCyan(s string) string   { return fmt.Sprintf("\033[36m%s\033[0m", s) }
func colorDim(s string) string    { return fmt.Sprintf("\033[2m%s\033[0m", s) }
func colorYellow(s string) string { return fmt.Sprintf("\033[33m%s\033[0m", s) }
func colorRed(s string) string    { return fmt.Sprintf("\033[31m%s\033[0m", s) }
