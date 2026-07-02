package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// MODEL 是使用的模型名称，可通过环境变量覆盖
var MODEL = getEnvOr("MODEL", "deepseek-v4-flash")

// WORKDIR 是工作区根目录，所有文件操作不得逃逸此目录
var WORKDIR = mustGetwd()

// ── 辅助函数 ──────────────────────────────────────────

func getEnvOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	return wd
}

// safePath 将相对路径拼接到 WORKDIR 后 resolve，并校验不会逃逸工作区
func safePath(p string) (string, error) {
	abs := filepath.Join(WORKDIR, p)
	resolved, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		// 目录可能不存在（如 write_file 创建新文件），退化到 Clean
		resolved = filepath.Clean(abs)
	} else {
		resolved = filepath.Join(resolved, filepath.Base(abs))
	}
	rel, err := filepath.Rel(WORKDIR, resolved)
	if err != nil || len(rel) >= 2 && rel[:2] == ".." {
		return "", fmt.Errorf("路径逃逸工作区: %s", p)
	}
	return resolved, nil
}

// ── 终端颜色 ──────────────────────────────────────────

func colorBold(s string) string   { return fmt.Sprintf("\033[1m%s\033[0m", s) }
func colorCyan(s string) string   { return fmt.Sprintf("\033[36m%s\033[0m", s) }
func colorDim(s string) string    { return fmt.Sprintf("\033[2m%s\033[0m", s) }
func colorYellow(s string) string { return fmt.Sprintf("\033[33m%s\033[0m", s) }
func colorRed(s string) string    { return fmt.Sprintf("\033[31m%s\033[0m", s) }
