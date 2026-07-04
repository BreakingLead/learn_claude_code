package agent

// session store 使用 append-only JSONL 保存会话快照。恢复时读取最后一条完整
// snapshot 事件；如果进程中断导致最后一行损坏，前面的完整事件仍可恢复。

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

type sessionStore struct {
	dir string
}

type sessionEvent struct {
	Type      string                   `json:"type"`
	SessionID string                   `json:"session_id"`
	CreatedAt string                   `json:"created_at"`
	Messages  []anthropic.MessageParam `json:"messages,omitempty"`
}

type sessionRecord struct {
	ID           string    `json:"id"`
	Path         string    `json:"path"`
	UpdatedAt    time.Time `json:"updated_at"`
	MessageCount int       `json:"message_count"`
	Preview      string    `json:"preview"`
}

func newSessionStore(dir string) *sessionStore {
	return &sessionStore{dir: dir}
}

func (s *sessionStore) newID(now time.Time) string {
	var suffix [3]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return fmt.Sprintf("sess_%s_%d", now.Format("20060102_150405"), now.UnixNano()&0xffff)
	}
	return fmt.Sprintf("sess_%s_%s", now.Format("20060102_150405"), hex.EncodeToString(suffix[:]))
}

func (s *sessionStore) saveSnapshot(sessionID string, messages []anthropic.MessageParam) error {
	if s == nil {
		return fmt.Errorf("session store is not initialized")
	}
	sessionID = safeSessionID(sessionID)
	if sessionID == "" {
		return fmt.Errorf("session id is required")
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(s.path(sessionID), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	event := sessionEvent{
		Type:      "snapshot",
		SessionID: sessionID,
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Messages:  messages,
	}
	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(event)
}

func (s *sessionStore) load(sessionID string) ([]anthropic.MessageParam, sessionRecord, error) {
	if s == nil {
		return nil, sessionRecord{}, fmt.Errorf("session store is not initialized")
	}
	sessionID = safeSessionID(sessionID)
	if sessionID == "" {
		return nil, sessionRecord{}, fmt.Errorf("session id is required")
	}
	path := s.path(sessionID)
	file, err := os.Open(path)
	if err != nil {
		return nil, sessionRecord{}, err
	}
	defer file.Close()
	var last sessionEvent
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event sessionEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		if event.Type == "snapshot" && event.SessionID == sessionID {
			last = event
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, sessionRecord{}, err
	}
	if last.SessionID == "" {
		return nil, sessionRecord{}, fmt.Errorf("session has no snapshot: %s", sessionID)
	}
	record := sessionRecord{ID: sessionID, Path: path, MessageCount: len(last.Messages), Preview: sessionPreview(last.Messages)}
	if stat, err := os.Stat(path); err == nil {
		record.UpdatedAt = stat.ModTime()
	}
	return last.Messages, record, nil
}

func (s *sessionStore) list() []sessionRecord {
	if s == nil {
		return nil
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil
	}
	var records []sessionRecord
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".jsonl")
		_, record, err := s.load(id)
		if err != nil {
			continue
		}
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].UpdatedAt.After(records[j].UpdatedAt)
	})
	return records
}

func (s *sessionStore) path(sessionID string) string {
	return filepath.Join(s.dir, safeSessionID(sessionID)+".jsonl")
}

func safeSessionID(id string) string {
	id = strings.TrimSpace(id)
	var b strings.Builder
	for _, r := range id {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func shortSessionID(id string) string {
	id = safeSessionID(id)
	if len(id) <= 18 {
		return id
	}
	return id[:18]
}

func sessionPreview(messages []anthropic.MessageParam) string {
	for i := len(messages) - 1; i >= 0; i-- {
		text := strings.TrimSpace(extractText(messages[i]))
		if text == "" {
			continue
		}
		runes := []rune(text)
		if len(runes) > 80 {
			return string(runes[:80])
		}
		return text
	}
	return "(empty session)"
}

func (rt *agentRuntime) saveCurrentSession(messages []anthropic.MessageParam) {
	if rt == nil || rt.sessions == nil || rt.sessionID == "" {
		return
	}
	if err := rt.sessions.saveSnapshot(rt.sessionID, messages); err != nil {
		rt.emitLine("[session] save failed: %v", err)
	}
}

func (rt *agentRuntime) startNewSession(now time.Time) string {
	if rt == nil || rt.sessions == nil {
		return ""
	}
	rt.sessionID = rt.sessions.newID(now)
	rt.promptCache = promptCache{}
	return rt.sessionID
}

func (rt *agentRuntime) resumeSession(sessionID string) ([]anthropic.MessageParam, sessionRecord, error) {
	if rt == nil || rt.sessions == nil {
		return nil, sessionRecord{}, fmt.Errorf("session store is not initialized")
	}
	messages, record, err := rt.sessions.load(safeSessionID(sessionID))
	if err != nil {
		return nil, sessionRecord{}, err
	}
	rt.sessionID = record.ID
	rt.promptCache = promptCache{}
	return messages, record, nil
}
