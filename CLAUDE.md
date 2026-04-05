# CLAUDE.md — editor

Vim-like terminal editor for Go, built with bubbletea. Intended as a daily driver that Claude can continuously improve via telemetry.

See **DESIGN.md** for full architectural rationale behind every layer.

---

## Common commands

```sh
make build          # compile ./editor binary
make test           # go test -race -v ./...
make lint           # golangci-lint run ./...
make vet            # go vet ./...
```

Run a single package's tests:
```sh
go test -race -v ./internal/editor/...
```

Postgres tests are skipped unless `EDITOR_TEST_DSN` is set:
```sh
EDITOR_TEST_DSN="host=localhost user=postgres dbname=editor sslmode=disable" \
  go test -race -v ./internal/buffer/piece/pgstore/...
```

Disable telemetry during development:
```sh
EDITOR_TELEMETRY=off ./editor myfile.go
```

---

## Package map

| Package | File(s) | Purpose |
|---|---|---|
| `main` | `main.go` | Entry point: wires telemetry, gopls, bubbletea |
| `internal/buffer` | `buffer.go` | `Buffer` facade — unified API over gap + piece table |
| `internal/buffer/gap` | `gap.go` | Gap buffer for O(1) insert-mode editing |
| `internal/buffer/piece` | `table.go`, `store.go` | Piece table (canonical document) + undo store interface |
| `internal/buffer/piece/pgstore` | `pgstore.go` | Postgres-backed undo store |
| `internal/buffer/piece/memstore` | `memstore.go` | In-memory undo store (fallback / tests) |
| `internal/editor` | `editor.go`, `motion.go`, `textobject.go` | Modal editing engine (Normal/Insert/Visual/Command) — no UI dependency |
| `internal/lsp` | `client.go`, `gopls.go` | JSON-RPC 2.0 LSP client + gopls session |
| `internal/ui` | `ui.go` | bubbletea Model — owns Editor, Buffer, lsp.Session |
| `internal/telemetry` | `telemetry.go` | JSONL event log at `~/.cache/editor/telemetry.jsonl` |
| `mocks/` | generated | mockery mocks for all interfaces |

---

## Constraints for AI changes

- **goleak in every `TestMain`**: all test packages must call `goleak.VerifyTestMain(m)`. Never remove it.
- **mockery for mocks**: regenerate mocks with `mockery --all` (or per-interface). Never write mocks by hand.
- **golangci-lint must pass**: `make lint` must be clean before any change is considered done.
- **Engine has no UI dependency**: `internal/editor` must not import `internal/ui` or bubbletea.
- **No direct bubbletea in buffer layer**: `internal/buffer` must not import bubbletea.
- **Postgres undo is optional**: buffer must work without a DSN; degraded-gracefully path must be preserved.

---

## Key interfaces

- `buffer.Buffer` (`internal/buffer/buffer.go`) — what the editor engine talks to
- `gap.Buffer` (`internal/buffer/gap/gap.go`) — gap buffer contract
- `piece.Table` (`internal/buffer/piece/table.go`) — piece table contract
- `piece.UndoStore` (`internal/buffer/piece/store.go`) — undo persistence contract
- `lsp.Session` (`internal/lsp/gopls.go`) — LSP operations the UI calls
- `telemetry.Telemetry` (`internal/telemetry/telemetry.go`) — event logging contract

---

## Status signals (UI ↔ engine)

The editor engine communicates async actions to the UI via `statusMsg` sentinel strings set on the `Editor` struct. The UI reads them in `Update` and fires the appropriate `tea.Cmd`:

| sentinel | meaning |
|---|---|
| `"lsp:gd"` | trigger go-to-definition |
| `"lsp:hover"` | trigger hover info |
| `"lsp:complete"` | trigger completion |
| `"quit"` | exit the program |
| `"open:<path>"` | open a new file |

---

## Telemetry

Usage events land in `~/.cache/editor/telemetry.jsonl` (one JSON object per line).

Quick inspection:
```sh
tail -n 200 ~/.cache/editor/telemetry.jsonl
grep '"event":"lsp_request"' ~/.cache/editor/telemetry.jsonl | jq '{method:.method, duration_ms:.duration_ms, ok:.ok}'
grep '"event":"key"' ~/.cache/editor/telemetry.jsonl | jq -r '.key' | sort | uniq -c | sort -rn | head -20
```

Use this data to identify slow LSP calls, inefficient motion habits, or underused features before proposing improvements.
