package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// harness wires a Client to two os.Pipe pairs standing in for a child
// language server, same pattern as backend/acp connection tests.
type harness struct {
	client *Client

	// toServerR is the read side of the client's writes (client stdin);
	// fromServerW is the write side feeding the client (client stdout).
	toServerR   *os.File
	fromServerW *os.File
}

type srvScript func(id any, method string, params map[string]any) []strip

type strip struct {
	Method string
	ID     any // non-nil response id / server request id
	Params any
	IsReq  bool // true → server→client request (param is params); false with ID → response (param is result)
}

func (h *harness) writeServer(msg strip) error {
	m := message{Jsonrpc: "2.0"}
	switch {
	case msg.ID != nil && msg.IsReq:
		m.ID = msg.ID
		m.Method = msg.Method
		m.Params = msg.Params
	case msg.ID != nil:
		// response: method must be absent
		m.ID = msg.ID
		m.Result = msg.Params
	default:
		m.Method = msg.Method
		m.Params = msg.Params
	}
	b, err := encodeMessage(m)
	if err != nil {
		return err
	}
	_, err = h.fromServerW.Write(b)
	return err
}

func newHarness(t *testing.T, onDiag OnDiagnostics) *harness {
	t.Helper()
	toServerR, clientW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	fromServerR, serverW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	c := newClientFromPipes(fromServerR, clientW, onDiag)
	t.Cleanup(func() {
		_ = c.Close()
		_ = toServerR.Close()
		_ = serverW.Close()
	})
	return &harness{client: c, toServerR: toServerR, fromServerW: serverW}
}

// serve runs a fake server goroutine: reads framed messages from the client
// and sends scripted strips in response.
func (h *harness) serve(t *testing.T, script srvScript) {
	t.Helper()
	go func() {
		br := &byteReader{r: bufio.NewReader(h.toServerR)}
		for {
			body, err := readMessage(br)
			if err != nil {
				return
			}
			msg, err := decodeMessage(body)
			if err != nil {
				return
			}
			var id any
			if msg.hasID() {
				_ = json.Unmarshal(msg.ID, &id)
			}
			var params map[string]any
			if len(msg.Params) > 0 {
				_ = json.Unmarshal(msg.Params, &params)
			}
			for _, s := range script(id, msg.Method, params) {
				if err := h.writeServer(s); err != nil {
					return
				}
			}
		}
	}()
}

// rawServer answers initialize so Start-like setups can proceed, and records
// every received message for assertions.
func (h *harness) serveRecording(t *testing.T, script srvScript) {
	t.Helper()
	h.serve(t, func(id any, method string, params map[string]any) []strip {
		return script(id, method, params)
	})
}

func resp(id any, method string, params map[string]any) []strip {
	return []strip{{Method: method, ID: id, Params: params}}
}

func TestInitializeHandshake(t *testing.T) {
	rx := make(chan strip, 16)
	h := newHarness(t, nil)

	h.serve(t, func(id any, method string, params map[string]any) []strip {
		if method == "initialized" {
			rx <- strip{Method: "initialized"}
			return nil
		}
		rx <- strip{Method: method, ID: id, Params: params}
		return resp(id, "initialize", map[string]any{
			"capabilities": map[string]any{"textDocumentSync": 1},
		})
	})

	// Drive the handshake directly against the pipe harness client (the same
	// sequence Start performs, sans subprocess spawn).
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	initParams := map[string]any{
		"rootUri": "file:///proj/root",
		"capabilities": map[string]any{
			"textDocument": map[string]any{
				"hover":      map[string]any{"contentFormat": []string{"markdown"}},
				"definition": map[string]any{"linkSupport": false},
			},
		},
	}
	if _, err := h.client.rawRequest(ctx, "initialize", "initialize", initParams); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if err := h.client.NotifySend("initialized", map[string]any{}); err != nil {
		t.Fatalf("initialized notify: %v", err)
	}

	select {
	case got := <-rx:
		if got.Method != "initialize" {
			t.Fatalf("first request = %s, want initialize", got.Method)
		}
		p := got.Params.(map[string]any)
		if p["rootUri"] != "file:///proj/root" {
			t.Errorf("initialize.rootUri = %v, want file:///proj/root", p["rootUri"])
		}
		caps, _ := p["capabilities"].(map[string]any)
		td, _ := caps["textDocument"].(map[string]any)
		if td == nil {
			t.Fatalf("initialize.capabilities.textDocument missing: %#v", p)
		}
		def, _ := td["definition"].(map[string]any)
		if def["linkSupport"] != false {
			t.Errorf("definition.linkSupport = %v, want false", def["linkSupport"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for initialize request")
	}
	select {
	case g := <-rx:
		if g.Method != "initialized" {
			t.Errorf("expected initialized notification next, got %s", g.Method)
		}
	case <-time.After(2 * time.Second):
		t.Error("initialized notification not observed")
	}
}

func TestWireFramingContentLength(t *testing.T) {
	h := newHarness(t, nil)
	done := make(chan error, 1)
	go func() {
		br := &byteReader{r: bufio.NewReader(h.toServerR)}
		raw, err := readMessage(br)
		if err != nil {
			done <- err
			return
		}
		done <- fmt.Errorf("framed ok: %s", raw)
	}()
	b, _ := encodeMessage(notification("test/x", map[string]any{"a": 1}))
	if _, err := h.client.stdin.Write(b); err != nil {
		t.Fatalf("write: %v", err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected framing result error carrying body")
		}
		if !strings.HasPrefix(err.Error(), "framed ok:") {
			t.Fatalf("framing read failed: %v", err)
		}
		body := strings.TrimPrefix(err.Error(), "framed ok: ")
		if len(body) < 10 || body[0] != '{' {
			t.Errorf("body not raw JSON as framed: %q", body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for framed message")
	}
}

func TestDidOpenFraming(t *testing.T) {
	h := newHarness(t, nil)
	rx := make(chan string, 8)
	go func() {
		br := &byteReader{r: bufio.NewReader(h.toServerR)}
		for {
			body, err := readMessage(br)
			if err != nil {
				return
			}
			rx <- string(body)
		}
	}()
	h.client.SetDiagnosticsHandler(func(string, []Diagnostic) {})

	if err := h.client.DidOpen(toURI("/p/main.go"), 1, "package main\n"); err != nil {
		t.Fatalf("didOpen: %v", err)
	}

	select {
	case body := <-rx:
		if !strings.Contains(body, "textDocument/didOpen") {
			t.Errorf("didOpen body missing method: %s", body)
		}
		if !strings.Contains(body, "package main") {
			t.Errorf("didOpen body missing text: %s", body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for didOpen")
	}
}

func TestDefinitionResultVariants(t *testing.T) {
	single := map[string]any{"uri": "file:///p/a.go", "range": map[string]any{
		"start": map[string]any{"line": 1, "character": 2},
		"end":   map[string]any{"line": 1, "character": 8},
	}}
	cases := []struct {
		name    string
		result  string
		wantLen int
	}{
		{"null", `null`, 0},
		{"empty array", `[]`, 0},
		{"singleton", string(mustMarshal(single)), 1},
		{"array", string(mustMarshal([]map[string]any{single, single})), 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, nil)
			h.serve(t, func(id any, method string, params map[string]any) []strip {
				if method == "initialize" {
					return resp(id, "initialize", map[string]any{})
				}
				return []strip{{Method: method, ID: id, Params: json.RawMessage(tc.result)}}
			})
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			locs, err := h.client.Definition(ctx, "file:///p/main.go", 3, 7)
			if err != nil {
				t.Fatalf("definition: %v", err)
			}
			if len(locs) != tc.wantLen {
				t.Fatalf("definition len = %d, want %d (%v)", len(locs), tc.wantLen, locs)
			}
			if tc.wantLen == 1 && locs[0].URI != "file:///p/a.go" {
				t.Errorf("uri = %v, want file:///p/a.go", locs[0].URI)
			}
			if tc.wantLen == 1 && locs[0].Range.Start.Line != 1 {
				t.Errorf("line = %d, want 1", locs[0].Range.Start.Line)
			}
		})
	}
}

func TestPublishDiagnosticsMapping(t *testing.T) {
	type diagRev struct {
		URI   string
		Diags []Diagnostic
	}
	rev := make(chan diagRev, 4)
	h := newHarness(t, func(uri string, diags []Diagnostic) {
		rev <- diagRev{uri, diags}
	})

	pub := strip{Method: "textDocument/publishDiagnostics", Params: map[string]any{
		"uri": "file:///p/main.go",
		"diagnostics": []map[string]any{
			{
				"range":    map[string]any{"start": map[string]any{"line": 0, "character": 0}, "end": map[string]any{"line": 0, "character": 1}},
				"severity": 1,
				"message":  "boom",
				"source":   "gopls",
			},
		},
	}}
	if err := h.writeServer(pub); err != nil {
		t.Fatalf("writeServer: %v", err)
	}
	select {
	case got := <-rev:
		if got.URI != "file:///p/main.go" {
			t.Errorf("uri = %v", got.URI)
		}
		if len(got.Diags) != 1 || got.Diags[0].Message != "boom" || got.Diags[0].Severity != 1 {
			t.Errorf("diags = %#v", got.Diags)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for diagnostics")
	}
}

func TestMalformedHeaderFailsPending(t *testing.T) {
	h := newHarness(t, nil)
	// start a request whose response will never arrive
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// write garbage that breaks framing; the read loop should fail pending
	// requests (and exit) without hanging.
	go func() {
		time.Sleep(50 * time.Millisecond)
		_, _ = h.fromServerW.Write([]byte("Garbage-Length: abc\r\n\r\nxyz"))
	}()

	done := make(chan error, 1)
	go func() {
		_, err := h.client.rawRequest(ctx, "hover", "textDocument/hover", positionParams("file:///p/main.go", 0, 0))
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected framing error to fail pending request")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("malformed header caused a hang")
	}
}

func TestServerRequestAnsweredMethodNotFound(t *testing.T) {
	h := newHarness(t, nil)
	gotResp := make(chan string, 1)
	go func() {
		br := &byteReader{r: bufio.NewReader(h.toServerR)}
		for {
			body, err := readMessage(br)
			if err != nil {
				return
			}
			if strings.Contains(string(body), "workspace/echo") {
				gotResp <- string(body)
			}
		}
	}()
	if err := h.writeServer(strip{Method: "workspace/echo", ID: 7.0, Params: map[string]any{}, IsReq: true}); err != nil {
		t.Fatalf("writeServer: %v", err)
	}
	select {
	case body := <-gotResp:
		if !strings.Contains(body, `-32601`) {
			t.Errorf("expected method-not-found error code in %s", body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for method-not-found response")
	}
}

func TestShutdownExitSequenceOnClose(t *testing.T) {
	h := newHarness(t, nil)
	saw := make(chan string, 4)
	h.serve(t, func(id any, method string, params map[string]any) []strip {
		switch method {
		case "shutdown":
			saw <- "shutdown"
			return []strip{{Method: method, ID: id, Params: "null"}}
		case "exit":
			saw <- "exit"
			return nil
		default:
			return resp(id, method, map[string]any{})
		}
	})
	ctxI, cancelI := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelI()
	if _, err := h.client.rawRequest(ctxI, "initialize", "initialize", map[string]any{}); err != nil {
		t.Fatalf("initialize roundtrip: %v", err)
	}
	if err := h.client.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	deadline := time.After(2 * time.Second)
	var sawShutdown, sawExit bool
	for !sawShutdown || !sawExit {
		select {
		case m := <-saw:
			if m == "shutdown" {
				sawShutdown = true
			}
			if m == "exit" {
				sawExit = true
			}
		case <-deadline:
			t.Fatalf("close sequence incomplete: shutdown=%v exit=%v", sawShutdown, sawExit)
		}
	}
}

func TestURIHelpers(t *testing.T) {
	original := filepath.Join(t.TempDir(), "a dir", "gün.go")
	uri := toURI(original)
	if !hasPrefix(uri, "file:///") {
		t.Errorf("toURI = %q, want file:/// scheme", uri)
	}
	back, err := fromURI(uri)
	if err != nil {
		t.Fatalf("fromURI: %v", err)
	}
	if back != original {
		t.Errorf("fromURI round-trip = %q", back)
	}
	expected, _ := filepath.Abs("/w/main.go")
	p, err := fromURI(toURI("/w/main.go"))
	if err != nil || p != expected {
		t.Errorf("fromURI = %q err %v, want /w/main.go", p, err)
	}
	if _, err := fromURI("http://web/ex"); err != nil || p == "" {
		_ = err // non-file uri: permitted to pass through or error; no crash
	}
}

func hasPrefix(s, p string) bool { return len(s) >= len(p) && s[:len(p)] == p }

func mustMarshal(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func TestWindowsURIPaths(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows path semantics")
	}
	cases := []struct{ path, uri string }{
		{`C:\Users\dev\a dir\gün.go`, "file:///C:/Users/dev/a%20dir/g%C3%BCn.go"},
		{`\\server\share\a dir\main.go`, "file://server/share/a%20dir/main.go"},
	}
	for _, tc := range cases {
		if got := toURI(tc.path); got != tc.uri {
			t.Errorf("toURI(%q) = %q, want %q", tc.path, got, tc.uri)
		}
		got, err := fromURI(tc.uri)
		if err != nil || got != tc.path {
			t.Errorf("fromURI(%q) = %q, %v", tc.uri, got, err)
		}
	}
	got, err := fromURI("file://localhost/C:/Users/dev/main.go")
	if err != nil || got != `C:\Users\dev\main.go` {
		t.Fatalf("localhost URI = %q, %v", got, err)
	}
}
