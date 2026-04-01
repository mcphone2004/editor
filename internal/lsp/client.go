// Package lsp implements a minimal JSON-RPC 2.0 LSP client.
//
// It speaks the Language Server Protocol over stdin/stdout of a subprocess
// (e.g. gopls).  Call Start to launch the server, then use Call for
// request/response pairs and Notify for fire-and-forget notifications.
// Inbound server notifications are delivered on the Notifications channel.
package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// Client manages a connection to a language server subprocess.
type Client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader

	mu      sync.Mutex
	pending map[int64]chan *response

	nextID atomic.Int64

	// Notifications receives server-initiated messages (diagnostics, etc.).
	Notifications chan Notification

	done chan struct{}
}

type request struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("LSP error %d: %s", e.Code, e.Message)
}

// Notification is a server-initiated message (no id field).
type Notification struct {
	Method string
	Params json.RawMessage
}

// Start launches the language server at command+args and returns a Client.
func Start(command string, args ...string) (*Client, error) {
	cmd := exec.CommandContext(context.Background(), command, args...) //nolint:gosec // intentional subprocess launch for LSP
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c := &Client{
		cmd:           cmd,
		stdin:         stdin,
		stdout:        bufio.NewReader(stdoutPipe),
		pending:       make(map[int64]chan *response),
		Notifications: make(chan Notification, 64),
		done:          make(chan struct{}),
	}
	go c.readLoop()
	return c, nil
}

// Call sends a request with the given method+params and blocks until the
// server replies.  The result is JSON-unmarshalled into result (may be nil).
func (c *Client) Call(method string, params, result any) error {
	id := c.nextID.Add(1)
	ch := make(chan *response, 1)

	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()

	if err := c.send(&request{JSONRPC: "2.0", ID: id, Method: method, Params: params}); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return err
	}

	resp, ok := <-ch
	if !ok {
		return fmt.Errorf("lsp: connection closed waiting for %s response", method)
	}
	if resp.Error != nil {
		return resp.Error
	}
	if result != nil && len(resp.Result) > 0 {
		return json.Unmarshal(resp.Result, result)
	}
	return nil
}

// Notify sends a notification (no response expected).
func (c *Client) Notify(method string, params any) error {
	return c.send(&request{JSONRPC: "2.0", Method: method, Params: params})
}

// Close shuts down the language server gracefully.
func (c *Client) Close() error {
	close(c.done)
	_ = c.stdin.Close()
	return c.cmd.Wait()
}

func (c *Client) send(r *request) error {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := io.WriteString(c.stdin, header); err != nil {
		return err
	}
	_, err = c.stdin.Write(data)
	return err
}

func (c *Client) readLoop() {
	defer func() {
		// Unblock all waiting callers.
		c.mu.Lock()
		for _, ch := range c.pending {
			close(ch)
		}
		c.pending = nil
		c.mu.Unlock()
	}()

	for {
		select {
		case <-c.done:
			return
		default:
		}

		// Parse Content-Length header.
		contentLen := 0
		for {
			line, err := c.stdout.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				break
			}
			if strings.HasPrefix(line, "Content-Length:") {
				n, _ := strconv.Atoi(strings.TrimSpace(line[len("Content-Length:"):]))
				contentLen = n
			}
		}
		if contentLen == 0 {
			continue
		}

		body := make([]byte, contentLen)
		if _, err := io.ReadFull(c.stdout, body); err != nil {
			return
		}

		var msg struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      *int64          `json:"id"`
			Method  string          `json:"method"`
			Result  json.RawMessage `json:"result"`
			Error   *rpcError       `json:"error"`
			Params  json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(body, &msg); err != nil {
			continue
		}

		if msg.ID != nil {
			// Response to a pending call.
			resp := &response{
				JSONRPC: msg.JSONRPC,
				ID:      *msg.ID,
				Result:  msg.Result,
				Error:   msg.Error,
			}
			c.mu.Lock()
			ch, ok := c.pending[resp.ID]
			delete(c.pending, resp.ID)
			c.mu.Unlock()
			if ok {
				ch <- resp
			}
		} else if msg.Method != "" {
			// Server notification.
			select {
			case c.Notifications <- Notification{Method: msg.Method, Params: msg.Params}:
			default: // drop if consumer is slow
			}
		}
	}
}
