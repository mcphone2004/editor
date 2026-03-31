// Package telemetry records structured editor usage events to a JSONL file
// that Claude (or any tool) can consume efficiently.
//
// Format: one JSON object per line (JSONL), flushed after each event.
// Each record has a "ts" (RFC3339Nano) and "event" field, plus per-event data.
//
// Example records:
//
//	{"ts":"2024-01-01T00:00:00Z","event":"key","mode":"NORMAL","key":"dw","duration_us":12}
//	{"ts":"2024-01-01T00:00:00Z","event":"mode_change","from":"NORMAL","to":"INSERT"}
//	{"ts":"2024-01-01T00:00:00Z","event":"save","path":"main.go","lines":42,"duration_ms":3}
//	{"ts":"2024-01-01T00:00:00Z","event":"lsp_request","method":"textDocument/hover","duration_ms":45,"ok":true}
//	{"ts":"2024-01-01T00:00:00Z","event":"diagnostic","severity":1,"source":"gopls","count":2}
//	{"ts":"2024-01-01T00:00:00Z","event":"session_end","keys_total":1523,"edits":87}
//
// The log path defaults to ~/.cache/editor/telemetry.jsonl and can be
// overridden with the EDITOR_TELEMETRY env variable.
// Set EDITOR_TELEMETRY=off to disable entirely.
package telemetry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Recorder writes telemetry events to a JSONL file.
type Recorder struct {
	mu      sync.Mutex
	f       *os.File
	enc     *json.Encoder
	enabled bool

	// Session counters.
	KeysTotal int
	Edits     int
}

// New opens (or creates) the telemetry log and returns a Recorder.
// Returns a no-op Recorder (enabled=false) on any error or when disabled.
func New() *Recorder {
	path := os.Getenv("EDITOR_TELEMETRY")
	if path == "off" {
		return &Recorder{}
	}
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return &Recorder{}
		}
		dir := filepath.Join(home, ".cache", "editor")
		_ = os.MkdirAll(dir, 0755)
		path = filepath.Join(dir, "telemetry.jsonl")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return &Recorder{}
	}
	return &Recorder{f: f, enc: json.NewEncoder(f), enabled: true}
}

// Close writes a session_end record and closes the file.
func (r *Recorder) Close() {
	if !r.enabled {
		return
	}
	_ = r.write(map[string]any{
		"ts":         now(),
		"event":      "session_end",
		"keys_total": r.KeysTotal,
		"edits":      r.Edits,
	})
	r.mu.Lock()
	defer r.mu.Unlock()
	_ = r.f.Close()
	r.enabled = false
}

// Key records a single keystroke with its current mode and processing time.
func (r *Recorder) Key(mode, key string, durationUS int64) {
	if !r.enabled {
		return
	}
	r.KeysTotal++
	_ = r.write(map[string]any{
		"ts":          now(),
		"event":       "key",
		"mode":        mode,
		"key":         key,
		"duration_us": durationUS,
	})
}

// ModeChange records a mode transition.
func (r *Recorder) ModeChange(from, to string) {
	if !r.enabled {
		return
	}
	_ = r.write(map[string]any{
		"ts":    now(),
		"event": "mode_change",
		"from":  from,
		"to":    to,
	})
}

// Save records a file-save event.
func (r *Recorder) Save(path string, lines int, durationMS int64) {
	if !r.enabled {
		return
	}
	r.Edits++
	_ = r.write(map[string]any{
		"ts":          now(),
		"event":       "save",
		"path":        path,
		"lines":       lines,
		"duration_ms": durationMS,
	})
}

// LSPRequest records an LSP round-trip.
func (r *Recorder) LSPRequest(method string, durationMS int64, ok bool) {
	if !r.enabled {
		return
	}
	_ = r.write(map[string]any{
		"ts":          now(),
		"event":       "lsp_request",
		"method":      method,
		"duration_ms": durationMS,
		"ok":          ok,
	})
}

// Diagnostic records the current diagnostic counts after an update.
func (r *Recorder) Diagnostic(source string, severity, count int) {
	if !r.enabled {
		return
	}
	_ = r.write(map[string]any{
		"ts":       now(),
		"event":    "diagnostic",
		"source":   source,
		"severity": severity,
		"count":    count,
	})
}

// CommandRun records a : command.
func (r *Recorder) CommandRun(cmd string) {
	if !r.enabled {
		return
	}
	_ = r.write(map[string]any{
		"ts":    now(),
		"event": "command",
		"cmd":   cmd,
	})
}

// MotionFreq records how often a motion/operator is used (for perf hints).
func (r *Recorder) MotionFreq(op string) {
	if !r.enabled {
		return
	}
	_ = r.write(map[string]any{
		"ts":    now(),
		"event": "motion",
		"op":    op,
	})
}

func (r *Recorder) write(v any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.enc.Encode(v)
}

func now() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}
