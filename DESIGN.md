# Editor Design Document

## Goal

A vim-like terminal editor intended as a daily driver for Go programming, with native LSP support via gopls, persistent undo, and structured telemetry so Claude can monitor usage and suggest improvements over time.

---

## Buffer layer

### Why two data structures?

| Structure | Used for | Trade-off |
|---|---|---|
| **Piece table** | Canonical document storage | O(pieces) edits, no text copied, natural undo |
| **Gap buffer** | Active insert-mode editing zone | O(1) insert/delete at cursor |

A pure piece table is correct but creates a new piece on every keystroke in insert mode. A gap buffer handles bursts of typing at O(1), then flushes a single piece to the table on mode exit. This mirrors how VS Code combines its piece tree with a batch flushing strategy.

### Piece table (`buffer/piece`)

- **Two backing arrays**: `original` (file content, never modified) and `add` (append-only new text).
- **Piece sequence**: `[]Piece{which, start, length}` — the document is the concatenation of the referenced spans.
- **Line cache**: `lineStarts []int` rebuilt lazily on first line access after a write. Dirty flag avoids redundant scans.
- **Snapshots**: `Snapshot{Pieces, AddLen}` captures the full document state cheaply (pieces slice + add buffer watermark). Restoring a snapshot never touches the original or add buffers — only the piece sequence is replaced.

### Gap buffer (`buffer/gap`)

- Standard rune-array gap buffer. Gap is sized at 64 runes initially and doubles as needed.
- Only active during insert mode on the "hot" line. `Buffer.ActivateGap(row, col)` loads the line; `Buffer.FlushGap()` writes it back as a single `Insert` into the piece table.

### Postgres undo (`buffer/piece/store.go`)

Each `FlushGap` + significant edit pushes a `Snapshot` row to `editor_undo_entries`. The schema is:

```
editor_files(id, path, opened_at)
editor_undo_entries(id, file_id, sequence, snapshot jsonb, created_at)
```

The `snapshot` column stores `{pieces: [...], add_len: N}`. Because the add buffer only grows, restoring a snapshot just truncates it to `add_len` — no data is ever lost.

**Why Postgres for undo?** SQLite would also work, but Postgres is already available in this environment and gives us free crash recovery, cross-session history, and the ability to query editing patterns with SQL.

Undo is gracefully degraded: if the Postgres connection fails, the editor still works — in-memory piece snapshots are still pushed locally, you just lose persistence across restarts.

---

## Editor engine (`editor`)

### Modal editing

Modes: `NORMAL → INSERT → NORMAL`, `NORMAL → VISUAL → NORMAL`, `NORMAL → COMMAND → NORMAL`.

State machine lives entirely in `Editor.HandleKey(key string)`. The `key` argument is a normalised string (`"a"`, `"<Esc>"`, `"<C-c>"`) so the engine has no bubbletea dependency.

### Motion / operator architecture

Motions are `func(e *Editor) (dst Pos, linewise bool)`. They return a destination; the engine applies the cursor move or operator range.

**Known limitation**: vim distinguishes "inclusive" motions (`e`, `$`, `f`) from "exclusive" ones (`w`, `b`). The current implementation treats all character motions as exclusive (matching `dw` behaviour). A future PR should add `inclusive bool` to the Motion return type. This is tracked in a TODO comment in `editor.go`.

Operators (`d`, `c`, `y`) dispatch through `applyOperatorRange` which calls the buffer's `DeleteRange` / `YankRange` directly.

### Count prefix

Digits are accumulated in `pendingCount` across all modes that support counts (normal, visual). `consumeCount()` parses and resets it atomically before each key is dispatched.

---

## LSP integration (`lsp`)

### Transport

`lsp.Client` speaks JSON-RPC 2.0 over subprocess stdin/stdout. The content-length framing loop runs in a dedicated goroutine. Pending calls are tracked in a `map[int64]chan *response`; the goroutine dispatches to the waiting channel or the `Notifications` channel for server-initiated messages.

### gopls session

`lsp.Session` wraps the client with gopls-specific lifecycle (initialize / initialized / shutdown) and the four methods the editor needs:

- `Definition` — go-to-definition (gd)
- `Hover` — type info under cursor (K)
- `Completion` — autocomplete list (C-n)
- `ParseDiagnostics` — decodes `textDocument/publishDiagnostics` notifications

Full-text sync (`textDocument/didChange` sends the entire file) is used for simplicity. Incremental sync is a future optimisation.

### go vet integration

On every save, `go vet ./...` is run in a `tea.Cmd` goroutine and its output is parsed into `[]editor.Diagnostic` using the standard `file:line:col: message` format. These are merged with gopls diagnostics and displayed in the gutter.

---

## UI (`ui`)

The bubbletea `Model` owns:
- The `Editor` (key dispatch, mode state)
- The `Buffer` (text storage)
- The `lsp.Session` (optional)
- Scroll offset, completion popup, hover popup

**Status signals via `StatusMsg`**: The editor engine communicates async operations back to the UI by setting `statusMsg` to sentinel strings (`"lsp:gd"`, `"lsp:hover"`, `"lsp:complete"`, `"quit"`, `"open:<path>"`). The UI reads these in `Update` and dispatches the appropriate `tea.Cmd`. This keeps the engine free of any UI/async dependency.

**Gap buffer lifecycle in the UI**: `handleKey` activates the gap buffer on entry to insert mode and calls `FlushGap` on exit. This is the correct place because the UI is the bridge between mode transitions and the buffer layer.

---

## Telemetry (`telemetry`)

Events are written as JSONL to `~/.cache/editor/telemetry.jsonl` (overridable via `EDITOR_TELEMETRY`, disabled with `EDITOR_TELEMETRY=off`).

Each line is a self-contained JSON object with:
- `"ts"` — RFC3339Nano timestamp (UTC)
- `"event"` — event type
- event-specific fields

### Why JSONL for Claude?

1. Each line is independently parseable — no need to buffer the whole file.
2. Claude can `tail -n 500 telemetry.jsonl` or `grep '"event":"key"'` to inspect recent activity.
3. The format is streamable and trivially imported into any analytics tool.

### Recorded events

| event | fields | purpose |
|---|---|---|
| `key` | mode, key, duration_us | hot key frequency, slow dispatch detection |
| `mode_change` | from, to | workflow patterns |
| `save` | path, lines, duration_ms | save frequency, file sizes |
| `lsp_request` | method, duration_ms, ok | LSP latency, error rates |
| `diagnostic` | source, severity, count | code quality over time |
| `command` | cmd | : command usage |
| `motion` | op | motion/operator frequency |
| `session_end` | keys_total, edits | session summary |

Claude can read `telemetry.jsonl`, aggregate motion frequencies, identify slow LSP calls, or spot patterns like "user always types `dd` 3 times in a row" (suggesting a missing `3dd` habit) and suggest improvements.

---

## Testing

- `go.uber.org/goleak` is wired into every package's `TestMain` to catch goroutine leaks.
- Tests cover all public methods of `gap.Buffer`, `piece.Table`, `buffer.Buffer`, and `editor.Editor`.
- The `editor` tests drive the engine through string key sequences (same path as the real UI) rather than calling internal methods directly, so they test realistic behaviour.

---

## Continuous improvement loop

1. **Telemetry** accumulates usage in `~/.cache/editor/telemetry.jsonl`.
2. Claude reads the log, identifies patterns (slow LSP, repeated inefficient motions, common error messages).
3. Claude opens this repo and implements targeted improvements — new motions, better completion ranking, undo stack optimisations, etc.
4. New behaviour is covered by tests before merging.
