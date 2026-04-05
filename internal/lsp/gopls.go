// gopls.go — gopls-specific LSP integration.
//
// This file handles the LSP lifecycle (initialize / initialized / shutdown),
// text-document sync, and the Go-specific requests the editor needs:
//   - textDocument/publishDiagnostics  (inbound notification)
//   - textDocument/definition          (go-to-definition)
//   - textDocument/hover               (type info / docs)
//   - textDocument/completion          (autocomplete)
package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// --- LSP protocol types (subset) ---

// Position represents a zero-based line/character offset in a text document.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range represents a span in a text document defined by start and end positions.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Location represents a file URI and a range within that file.
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// TextDocumentIdentifier identifies a text document by URI.
type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

// VersionedTextDocumentIdentifier identifies a text document with a version number.
type VersionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
}

// TextDocumentPositionParams are parameters for a text-document position request.
type TextDocumentPositionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

// Diagnostic severity constants.
const (
	SeverityError   = 1
	SeverityWarning = 2
	SeverityInfo    = 3
	SeverityHint    = 4
)

// DiagnosticMsg is a single LSP diagnostic message.
type DiagnosticMsg struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity"`
	Message  string `json:"message"`
	Source   string `json:"source"`
}

// PublishDiagnosticsParams are the params for a textDocument/publishDiagnostics notification.
type PublishDiagnosticsParams struct {
	URI         string          `json:"uri"`
	Diagnostics []DiagnosticMsg `json:"diagnostics"`
}

// HoverResult is the result of a textDocument/hover request.
type HoverResult struct {
	Contents MarkupContent `json:"contents"`
	Range    *Range        `json:"range,omitempty"`
}

// MarkupContent represents human-readable content with a kind (plaintext or markdown).
type MarkupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// CompletionItem is a single completion suggestion.
type CompletionItem struct {
	Label         string `json:"label"`
	Kind          int    `json:"kind"`
	Detail        string `json:"detail"`
	Documentation string `json:"documentation"`
	InsertText    string `json:"insertText"`
}

// CompletionList is a list of completion items.
type CompletionList struct {
	IsIncomplete bool             `json:"isIncomplete"`
	Items        []CompletionItem `json:"items"`
}

// --- Session ---

// Session wraps a Client with gopls-specific methods.
type Session struct {
	client  *Client
	rootURI string
	version map[string]int // uri → document version
}

// StartGopls launches gopls and performs the LSP handshake.
// rootDir should be the workspace root (e.g. the module root).
// ctx controls the timeout for the initialize handshake; use context.WithTimeout.
func StartGopls(ctx context.Context, rootDir string) (*Session, error) {
	c, err := Start(ctx, "gopls", "serve")
	if err != nil {
		return nil, fmt.Errorf("gopls: start: %w", err)
	}
	abs, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, err
	}
	rootURI := "file://" + abs

	s := &Session{client: c, rootURI: rootURI, version: make(map[string]int)}

	if err := s.initialize(ctx); err != nil {
		_ = c.Close(ctx)
		return nil, err
	}
	return s, nil
}

func (s *Session) initialize(ctx context.Context) error {
	params := map[string]any{
		"processId": os.Getpid(),
		"rootUri":   s.rootURI,
		"capabilities": map[string]any{
			"textDocument": map[string]any{
				"publishDiagnostics": map[string]any{"relatedInformation": true},
				"hover":              map[string]any{"contentFormat": []string{"markdown", "plaintext"}},
				"completion": map[string]any{
					"completionItem": map[string]any{"snippetSupport": false},
				},
				"definition": map[string]any{},
			},
		},
		"initializationOptions": map[string]any{
			"analyses": map[string]any{
				"unusedparams": true,
				"shadow":       true,
			},
			// staticcheck disabled: runs full project-wide analysis on startup, OOM risk.
			"staticcheck": false,
		},
	}
	var result json.RawMessage
	if err := s.client.Call(ctx, "initialize", params, &result); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	return s.client.Notify(ctx, "initialized", map[string]any{})
}

// DidOpen notifies gopls that a file has been opened.
func (s *Session) DidOpen(ctx context.Context, path, text string) error {
	uri := pathToURI(path)
	s.version[uri] = 1
	return s.client.Notify(ctx, "textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": "go",
			"version":    1,
			"text":       text,
		},
	})
}

// DidChange notifies gopls of a full-text update (we use full sync).
func (s *Session) DidChange(ctx context.Context, path, text string) error {
	uri := pathToURI(path)
	s.version[uri]++
	return s.client.Notify(ctx, "textDocument/didChange", map[string]any{
		"textDocument": VersionedTextDocumentIdentifier{URI: uri, Version: s.version[uri]},
		"contentChanges": []map[string]any{
			{"text": text},
		},
	})
}

// DidSave notifies gopls that a file was saved.
func (s *Session) DidSave(ctx context.Context, path string) error {
	uri := pathToURI(path)
	return s.client.Notify(ctx, "textDocument/didSave", map[string]any{
		"textDocument": TextDocumentIdentifier{URI: uri},
	})
}

// Definition requests go-to-definition for (path, line, char).
// Returns a list of target locations.
func (s *Session) Definition(ctx context.Context, path string, line, char int) ([]Location, error) {
	params := TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: pathToURI(path)},
		Position:     Position{Line: line, Character: char},
	}
	var result json.RawMessage
	if err := s.client.Call(ctx, "textDocument/definition", params, &result); err != nil {
		return nil, err
	}
	// The result may be a Location, []Location, or null.
	if len(result) == 0 || string(result) == "null" {
		return nil, nil
	}
	// Try array first.
	var locs []Location
	if err := json.Unmarshal(result, &locs); err == nil {
		return locs, nil
	}
	var loc Location
	if err := json.Unmarshal(result, &loc); err != nil {
		return nil, err
	}
	return []Location{loc}, nil
}

// Hover requests hover information for (path, line, char).
// Returns the markdown/plaintext content, or ("", nil) if nothing to show.
func (s *Session) Hover(ctx context.Context, path string, line, char int) (string, error) {
	params := TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: pathToURI(path)},
		Position:     Position{Line: line, Character: char},
	}
	var result json.RawMessage
	if err := s.client.Call(ctx, "textDocument/hover", params, &result); err != nil {
		return "", err
	}
	if len(result) == 0 || string(result) == "null" {
		return "", nil
	}
	var h HoverResult
	if err := json.Unmarshal(result, &h); err != nil {
		return "", err
	}
	return h.Contents.Value, nil
}

// Completion requests completion items at (path, line, char).
func (s *Session) Completion(ctx context.Context, path string, line, char int) ([]CompletionItem, error) {
	params := map[string]any{
		"textDocument": TextDocumentIdentifier{URI: pathToURI(path)},
		"position":     Position{Line: line, Character: char},
		"context":      map[string]any{"triggerKind": 1},
	}
	var list CompletionList
	if err := s.client.Call(ctx, "textDocument/completion", params, &list); err != nil {
		// Fall back to raw array.
		var items []CompletionItem
		if err2 := s.client.Call(ctx, "textDocument/completion", params, &items); err2 != nil {
			return nil, err
		}
		return items, nil
	}
	return list.Items, nil
}

// Shutdown sends shutdown + exit to gopls.
func (s *Session) Shutdown(ctx context.Context) {
	_ = s.client.Call(ctx, "shutdown", nil, nil)
	_ = s.client.Notify(ctx, "exit", nil)
	_ = s.client.Close(ctx)
}

// Notifications returns the raw notification channel from the underlying client.
func (s *Session) Notifications() <-chan Notification {
	return s.client.Notifications
}

// Exited returns a channel that is closed when the gopls process exits,
// whether due to a crash, explicit shutdown, or any other reason.
func (s *Session) Exited() <-chan struct{} {
	return s.client.exited
}

// ParseDiagnostics decodes a publishDiagnostics notification.
func ParseDiagnostics(n Notification) (PublishDiagnosticsParams, error) {
	var p PublishDiagnosticsParams
	err := json.Unmarshal(n.Params, &p)
	return p, err
}

// URIToPath converts a file:// URI to an OS path.
func URIToPath(uri string) string {
	if len(uri) > 7 && uri[:7] == "file://" {
		return uri[7:]
	}
	return uri
}

func pathToURI(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "file://" + path
	}
	return "file://" + abs
}
