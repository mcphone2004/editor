package telemetry

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// DebugRecorder wraps any Telemetry and mirrors every event as a JSON line to
// an io.Writer (typically os.Stderr) before delegating to the inner recorder.
//
// Activate it by running the editor with EDITOR_DEBUG=1:
//
//	EDITOR_DEBUG=1 ~/editor myfile.go 2>debug.log
//
// Because events are written to stderr before the inner recorder flushes them
// to disk, the debug log is useful for diagnosing crashes or SIGKILL events
// where the JSONL file may not have been fully flushed.
type DebugRecorder struct {
	inner Telemetry
	w     io.Writer
}

// NewDebugRecorder returns a DebugRecorder that writes debug output to w and
// delegates all events to inner.
func NewDebugRecorder(inner Telemetry, w io.Writer) *DebugRecorder {
	return &DebugRecorder{inner: inner, w: w}
}

func (d *DebugRecorder) log(fields map[string]any) {
	b, err := json.Marshal(fields)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[telemetry] marshal error: %v\n", err)
		return
	}
	_, _ = fmt.Fprintf(d.w, "%s\n", b)
}

// Startup implements Telemetry.
func (d *DebugRecorder) Startup(path string, sizeBytes int64, lines int) {
	d.log(map[string]any{"event": "startup", "pid": os.Getpid(), "file": path, "size_bytes": sizeBytes, "lines": lines})
	d.inner.Startup(path, sizeBytes, lines)
}

// VetStart implements Telemetry.
func (d *DebugRecorder) VetStart(workdir string) {
	d.log(map[string]any{"event": "vet_start", "workdir": workdir})
	d.inner.VetStart(workdir)
}

// VetEnd implements Telemetry.
func (d *DebugRecorder) VetEnd(workdir string, durationMS int64, exitCode int, output string) {
	d.log(map[string]any{"event": "vet_end", "workdir": workdir, "duration_ms": durationMS, "exit_code": exitCode, "output": output})
	d.inner.VetEnd(workdir, durationMS, exitCode, output)
}

// Error implements Telemetry.
func (d *DebugRecorder) Error(msg string, err error) {
	detail := ""
	if err != nil {
		detail = err.Error()
	}
	d.log(map[string]any{"event": "error", "msg": msg, "detail": detail})
	d.inner.Error(msg, err)
}

// Key implements Telemetry.
func (d *DebugRecorder) Key(mode, key string, durationUS int64) {
	d.log(map[string]any{"event": "key", "mode": mode, "key": key, "duration_us": durationUS})
	d.inner.Key(mode, key, durationUS)
}

// ModeChange implements Telemetry.
func (d *DebugRecorder) ModeChange(from, to string) {
	d.log(map[string]any{"event": "mode_change", "from": from, "to": to})
	d.inner.ModeChange(from, to)
}

// Save implements Telemetry.
func (d *DebugRecorder) Save(path string, lines int, durationMS int64) {
	d.log(map[string]any{"event": "save", "path": path, "lines": lines, "duration_ms": durationMS})
	d.inner.Save(path, lines, durationMS)
}

// LSPRequest implements Telemetry.
func (d *DebugRecorder) LSPRequest(method string, durationMS int64, ok bool) {
	d.log(map[string]any{"event": "lsp_request", "method": method, "duration_ms": durationMS, "ok": ok})
	d.inner.LSPRequest(method, durationMS, ok)
}

// Diagnostic implements Telemetry.
func (d *DebugRecorder) Diagnostic(source string, severity, count int) {
	d.log(map[string]any{"event": "diagnostic", "source": source, "severity": severity, "count": count})
	d.inner.Diagnostic(source, severity, count)
}

// CommandRun implements Telemetry.
func (d *DebugRecorder) CommandRun(cmd string) {
	d.log(map[string]any{"event": "command", "cmd": cmd})
	d.inner.CommandRun(cmd)
}

// MotionFreq implements Telemetry.
func (d *DebugRecorder) MotionFreq(op string) {
	d.log(map[string]any{"event": "motion", "op": op})
	d.inner.MotionFreq(op)
}

// Close implements Telemetry.
func (d *DebugRecorder) Close() {
	d.log(map[string]any{"event": "session_end"})
	d.inner.Close()
}
