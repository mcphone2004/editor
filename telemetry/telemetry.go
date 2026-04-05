// Package telemetry records structured editor usage events to a JSONL file
// that Claude (or any tool) can consume efficiently.
//
// Format: one JSON object per line (JSONL), flushed after each event.
// Each record has a "ts" (RFC3339Nano) and "event" field, plus per-event data.
//
// Example records:
//
//	{"ts":"...","event":"startup","pid":12345,"file":"main.go","size_bytes":1234,"lines":42}
//	{"ts":"...","event":"key","mode":"NORMAL","key":"dw","duration_us":12}
//	{"ts":"...","event":"mode_change","from":"NORMAL","to":"INSERT"}
//	{"ts":"...","event":"save","path":"main.go","lines":42,"duration_ms":3}
//	{"ts":"...","event":"vet_start","workdir":"/src/editor"}
//	{"ts":"...","event":"vet_end","workdir":"/src/editor","duration_ms":820,"exit_code":0,"output":""}
//	{"ts":"...","event":"lsp_request","method":"textDocument/hover","duration_ms":45,"ok":true}
//	{"ts":"...","event":"error","msg":"go vet failed","detail":"exit status 1"}
//	{"ts":"...","event":"session_end","keys_total":1523,"edits":87}
//
// The log path defaults to ~/.cache/editor/telemetry.jsonl and can be
// overridden with the EDITOR_TELEMETRY env variable.
// Set EDITOR_TELEMETRY=off to disable entirely.
//
// Set EDITOR_DEBUG=1 to also mirror every event to stderr (redirect with
// "~/editor file 2>debug.log" to capture logs across a crash or SIGKILL).
package telemetry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Telemetry is the interface through which all editor components emit events.
// Use Recorder for normal operation and wrap it with DebugRecorder when
// EDITOR_DEBUG=1 is set to also mirror events to stderr.
type Telemetry interface {
	// Startup logs the initial launch: PID, file opened, and file metadata.
	// Call as early as possible so a SIGKILL still leaves a trace.
	Startup(path string, sizeBytes int64, lines int)

	// VetStart logs that a go vet subprocess is about to be launched.
	// Call immediately before exec so the record is on disk before any kill.
	VetStart(workdir string)

	// VetEnd logs the outcome of a completed go vet run.
	VetEnd(workdir string, durationMS int64, exitCode int, output string)

	// Error logs an unexpected error with a human-readable message.
	Error(msg string, err error)

	// Key logs a single keystroke with its mode and processing time.
	Key(mode, key string, durationUS int64)

	// ModeChange logs a mode transition.
	ModeChange(from, to string)

	// Save logs a file-save event.
	Save(path string, lines int, durationMS int64)

	// LSPRequest logs an LSP round-trip.
	LSPRequest(method string, durationMS int64, ok bool)

	// Diagnostic logs the current diagnostic counts after an update.
	Diagnostic(source string, severity, count int)

	// CommandRun logs a : command execution.
	CommandRun(cmd string)

	// MotionFreq logs motion/operator usage for performance hints.
	MotionFreq(op string)

	// Close writes a session_end record and releases resources.
	Close()
}

// New returns a Recorder writing events to the telemetry JSONL file.
// If EDITOR_DEBUG=1, the recorder is wrapped in a DebugRecorder that
// also mirrors every event as JSON to stderr.
func New() Telemetry {
	r := newRecorder()
	if os.Getenv("EDITOR_DEBUG") == "1" {
		return NewDebugRecorder(r, os.Stderr)
	}
	return r
}

// Noop returns a Telemetry that silently discards all events. Useful in tests.
func Noop() Telemetry { return &Recorder{} }

// Recorder writes telemetry events to a JSONL file.
type Recorder struct {
	mu      sync.Mutex
	f       *os.File
	enc     *json.Encoder
	enabled bool

	// Session counters (read by Close to emit session_end).
	KeysTotal int
	Edits     int
}

func newRecorder() *Recorder {
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
		_ = os.MkdirAll(dir, 0o750)
		path = filepath.Join(dir, "telemetry.jsonl")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // known telemetry path
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

// Startup records the initial editor launch.
func (r *Recorder) Startup(path string, sizeBytes int64, lines int) {
	_ = r.write(map[string]any{
		"ts":         now(),
		"event":      "startup",
		"pid":        os.Getpid(),
		"file":       path,
		"size_bytes": sizeBytes,
		"lines":      lines,
	})
}

// VetStart records that a go vet subprocess is about to be launched.
func (r *Recorder) VetStart(workdir string) {
	_ = r.write(map[string]any{
		"ts":      now(),
		"event":   "vet_start",
		"workdir": workdir,
	})
}

// VetEnd records the outcome of a completed go vet run.
func (r *Recorder) VetEnd(workdir string, durationMS int64, exitCode int, output string) {
	const maxOut = 512
	if len(output) > maxOut {
		output = output[:maxOut] + "…(truncated)"
	}
	_ = r.write(map[string]any{
		"ts":          now(),
		"event":       "vet_end",
		"workdir":     workdir,
		"duration_ms": durationMS,
		"exit_code":   exitCode,
		"output":      output,
	})
}

// Error records an unexpected error.
func (r *Recorder) Error(msg string, err error) {
	detail := ""
	if err != nil {
		detail = err.Error()
	}
	_ = r.write(map[string]any{
		"ts":     now(),
		"event":  "error",
		"msg":    msg,
		"detail": detail,
	})
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
	_ = r.write(map[string]any{
		"ts":    now(),
		"event": "command",
		"cmd":   cmd,
	})
}

// MotionFreq records how often a motion/operator is used.
func (r *Recorder) MotionFreq(op string) {
	_ = r.write(map[string]any{
		"ts":    now(),
		"event": "motion",
		"op":    op,
	})
}

func (r *Recorder) write(v any) error {
	if !r.enabled {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.enc.Encode(v)
}

func now() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}
