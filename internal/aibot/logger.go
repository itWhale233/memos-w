package aibot

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/pkg/errors"

	"github.com/usememos/memos/internal/profile"
)

type LogLevel string

const (
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
)

type LogEntry struct {
	Time       time.Time      `json:"time"`
	Level      LogLevel       `json:"level"`
	Stage      string         `json:"stage"`
	Status     string         `json:"status"`
	EventType  string         `json:"event_type,omitempty"`
	Memo       string         `json:"memo,omitempty"`
	Target     string         `json:"target,omitempty"`
	ProviderID string         `json:"provider_id,omitempty"`
	Model      string         `json:"model,omitempty"`
	Message    string         `json:"message"`
	Detail     map[string]any `json:"detail,omitempty"`
}

type Logger struct {
	profile *profile.Profile
	mu      sync.Mutex
}

func NewLogger(profile *profile.Profile) *Logger {
	return &Logger{profile: profile}
}

func (l *Logger) Log(_ context.Context, entry LogEntry) error {
	if entry.Time.IsZero() {
		entry.Time = time.Now()
	}
	bytes, err := json.Marshal(entry)
	if err != nil {
		return errors.Wrap(err, "failed to marshal AI assistant log entry")
	}
	path := l.logPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return errors.Wrap(err, "failed to create AI assistant log directory")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return errors.Wrap(err, "failed to open AI assistant log file")
	}
	defer file.Close()
	if _, err := file.Write(append(bytes, '\n')); err != nil {
		return errors.Wrap(err, "failed to append AI assistant log entry")
	}
	return nil
}

func (l *Logger) ReadRecent(limit int) ([]LogEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	bytes, err := os.ReadFile(l.logPath())
	if err != nil {
		if os.IsNotExist(err) {
			return []LogEntry{}, nil
		}
		return nil, errors.Wrap(err, "failed to read AI assistant log file")
	}
	lines := splitNonEmptyLines(string(bytes))
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	entries := make([]LogEntry, 0, len(lines))
	for _, line := range lines {
		var entry LogEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func (l *Logger) logPath() string {
	return filepath.Join(l.profile.Data, "logs", "ai-assistant.log")
}

func splitNonEmptyLines(content string) []string {
	lines := []string{}
	start := 0
	for i, ch := range content {
		if ch != '\n' {
			continue
		}
		if i > start {
			lines = append(lines, content[start:i])
		}
		start = i + 1
	}
	if start < len(content) {
		lines = append(lines, content[start:])
	}
	return lines
}
