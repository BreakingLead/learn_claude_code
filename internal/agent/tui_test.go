package agent

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestIsLeakedMouseReportKeyDetectsSGRFragments(t *testing.T) {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[<65;75;21M")}
	if !isLeakedMouseReportKey(msg) {
		t.Fatal("expected leaked SGR mouse report to be filtered")
	}

	normal := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[<not mouse>")}
	if isLeakedMouseReportKey(normal) {
		t.Fatal("expected normal text to remain editable")
	}
}

func TestStripSGRMouseReportsRemovesEmbeddedReports(t *testing.T) {
	input := "hello[<65;75;21M world\x1b[<0;33;17m!"
	got := stripSGRMouseReports(input)
	if got != "hello world!" {
		t.Fatalf("unexpected cleaned input: %q", got)
	}
}

func TestStripSGRMouseReportsKeepsIncompleteText(t *testing.T) {
	input := "keep [<65;75;21 without terminator"
	got := stripSGRMouseReports(input)
	if got != input {
		t.Fatalf("expected incomplete report to remain unchanged, got %q", got)
	}
}
