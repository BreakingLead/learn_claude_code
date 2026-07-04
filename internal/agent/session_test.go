package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

func TestSessionStoreSaveLoadSnapshot(t *testing.T) {
	store := newSessionStore(filepath.Join(t.TempDir(), "sessions"))
	messages := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock("hello")),
		anthropic.NewAssistantMessage(anthropic.NewTextBlock("world")),
	}

	if err := store.saveSnapshot("sess_test", messages); err != nil {
		t.Fatal(err)
	}

	loaded, record, err := store.load("sess_test")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(loaded))
	}
	if record.ID != "sess_test" || record.MessageCount != 2 || record.Preview != "world" {
		t.Fatalf("unexpected record: %+v", record)
	}
}

func TestSessionStoreIgnoresCorruptedTrailingLine(t *testing.T) {
	store := newSessionStore(filepath.Join(t.TempDir(), "sessions"))
	messages := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock("recover me")),
	}
	if err := store.saveSnapshot("sess_corrupt", messages); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(store.path("sess_corrupt"), os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("{broken json\n"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	loaded, record, err := store.load("sess_corrupt")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || record.Preview != "recover me" {
		t.Fatalf("expected previous complete snapshot, loaded=%d record=%+v", len(loaded), record)
	}
}

func TestSessionStoreListSortsAndIncludesEmptySessions(t *testing.T) {
	store := newSessionStore(filepath.Join(t.TempDir(), "sessions"))
	if err := store.saveSnapshot("sess_old", nil); err != nil {
		t.Fatal(err)
	}
	if err := store.saveSnapshot("sess_new", []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("latest"))}); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	newTime := oldTime.Add(time.Hour)
	if err := os.Chtimes(store.path("sess_old"), oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(store.path("sess_new"), newTime, newTime); err != nil {
		t.Fatal(err)
	}

	records := store.list()
	if len(records) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(records))
	}
	if records[0].ID != "sess_new" || records[1].ID != "sess_old" {
		t.Fatalf("unexpected order: %+v", records)
	}
	if records[1].Preview != "(empty session)" {
		t.Fatalf("expected empty preview, got %q", records[1].Preview)
	}
}

func TestSessionStoreSanitizesSessionID(t *testing.T) {
	store := newSessionStore(filepath.Join(t.TempDir(), "sessions"))
	if err := store.saveSnapshot("../sess_bad", []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("safe"))}); err != nil {
		t.Fatal(err)
	}
	loaded, record, err := store.load("../sess_bad")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || record.ID != "sess_bad" {
		t.Fatalf("unexpected sanitized load: loaded=%d record=%+v", len(loaded), record)
	}
	if _, err := os.Stat(filepath.Join(store.dir, "sess_bad.jsonl")); err != nil {
		t.Fatal(err)
	}
}
