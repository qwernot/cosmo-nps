package main

import (
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

type logEntry struct {
	ID      int64     `json:"id"`
	Time    time.Time `json:"time"`
	Stream  string    `json:"stream"`
	Message string    `json:"message"`
}

type logBuffer struct {
	mu      sync.RWMutex
	nextID  int64
	max     int
	entries []logEntry
	partial map[string]string
}

func newLogBuffer(max int) *logBuffer {
	if max <= 0 {
		max = 1000
	}
	return &logBuffer{
		max:     max,
		partial: map[string]string{},
	}
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.addChunk("process", string(p))
	return len(p), nil
}

func (b *logBuffer) addChunk(stream, chunk string) {
	if chunk == "" {
		return
	}
	text := b.partial[stream] + strings.ReplaceAll(chunk, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	b.partial[stream] = lines[len(lines)-1]
	for _, line := range lines[:len(lines)-1] {
		b.addLine(stream, line)
	}
}

func (b *logBuffer) addLine(stream, line string) {
	line = strings.TrimSpace(ansiPattern.ReplaceAllString(line, ""))
	if line == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextID++
	b.entries = append(b.entries, logEntry{
		ID:      b.nextID,
		Time:    time.Now().UTC(),
		Stream:  stream,
		Message: line,
	})
	if len(b.entries) > b.max {
		copy(b.entries, b.entries[len(b.entries)-b.max:])
		b.entries = b.entries[:b.max]
	}
}

func (b *logBuffer) list(limit int, query string) []logEntry {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if limit <= 0 || limit > b.max {
		limit = b.max
	}
	query = strings.ToLower(strings.TrimSpace(query))
	out := make([]logEntry, 0, min(limit, len(b.entries)))
	for i := len(b.entries) - 1; i >= 0 && len(out) < limit; i-- {
		entry := b.entries[i]
		if query != "" && !strings.Contains(strings.ToLower(entry.Message), query) && !strings.Contains(strings.ToLower(entry.Stream), query) {
			continue
		}
		out = append(out, entry)
	}
	slices.Reverse(out)
	return out
}

func (b *logBuffer) clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.entries = nil
	b.partial = map[string]string{}
}
