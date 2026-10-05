// Package lsp implements a lean LSP-over-stdio client and a gopls provider.
// Framing is the standard LSP wire format: "Content-Length: N\r\n\r\n" +
// UTF-8 JSON body, in both directions.
package lsp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Basic protocol structures. Positions are (line, character) pairs with
// UTF-16 code unit characters — which matches CodeMirror's native offsets,
// so the frontend can pass offsets straight through.
type Position struct {
	Line      int32 `json:"line"`
	Character int32 `json:"character"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int8   `json:"severity,omitempty"`
	Message  string `json:"message"`
	Source   string `json:"source,omitempty"`
}

type Hover struct {
	Contents json.RawMessage `json:"contents"`
}

type DocumentSymbol struct {
	Name           string            `json:"name"`
	Kind           int32             `json:"kind"`
	Range          Range             `json:"range"`
	SelectionRange Range             `json:"selectionRange"`
	Children       []*DocumentSymbol `json:"children,omitempty"`
}

// SymbolInformation is the flat symbol shape some servers return from
// textDocument/documentSymbol. It is converted into DocumentSymbol by the
// client so callers only see the hierarchical shape.
type SymbolInformation struct {
	Name     string   `json:"name"`
	Kind     int32    `json:"kind"`
	Location Location `json:"location"`
}

// toURI converts a filesystem path to a file:// URI.
func toURI(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	abs = filepath.ToSlash(abs)
	u := url.URL{Scheme: "file", Path: abs}
	if runtime.GOOS == "windows" {
		if strings.HasPrefix(abs, "//") {
			host, path, _ := strings.Cut(strings.TrimPrefix(abs, "//"), "/")
			u.Host, u.Path = host, "/"+path
		} else {
			u.Path = "/" + abs
		}
	}
	return u.String()
}

// ToURI is the exported filesystem-path → file://-URI converter used by
// facade layers outside the package.
func ToURI(path string) string { return toURI(path) }

// FromURI is the exported file://-URI → filesystem-path converter used by
// facade layers outside the package. Non-file URIs are returned unchanged.
func FromURI(uri string) (string, error) { return fromURI(uri) }

// fromURI converts a file:// URI back to a filesystem path. Non-file URIs
// are returned unchanged.
func fromURI(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", fmt.Errorf("lsp: parse uri %q: %w", uri, err)
	}
	if u.Scheme != "file" {
		return uri, nil
	}
	if u.Path == "" && u.Host != "" && u.Host != "localhost" {
		return "", fmt.Errorf("lsp: uri %q has no path", uri)
	}
	path := u.Path
	if runtime.GOOS == "windows" {
		if u.Host != "" && !strings.EqualFold(u.Host, "localhost") {
			path = "//" + u.Host + path
		} else if len(path) >= 3 && path[0] == '/' && path[2] == ':' {
			path = path[1:]
		}
	}
	return filepath.FromSlash(path), nil
}

// message is the generic JSON-RPC envelope for marshaling.
type message struct {
	Jsonrpc string    `json:"jsonrpc"`
	ID      any       `json:"id,omitempty"` // nil → notification
	Method  string    `json:"method,omitempty"`
	Params  any       `json:"params,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int64  `json:"code"`
	Message string `json:"message"`
}

// notification builds a request-less frame.
func notification(method string, params any) message {
	return message{Jsonrpc: "2.0", Method: method, Params: params}
}

// request builds a numbered request frame.
func request(id uint64, method string, params any) message {
	idv := id // pointer so 0 is not dropped by omitempty
	return message{Jsonrpc: "2.0", ID: &idv, Method: method, Params: params}
}

// encodeMessage marshals v and prepends the LSP Content-Length header.
func encodeMessage(v any) ([]byte, error) {
	body, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "Content-Length: %d\r\n\r\n", len(body))
	buf.Write(body)
	return buf.Bytes(), nil
}

// decodeMessage parses one LSP-framed message from raw body bytes.
func decodeMessage(body []byte) (wireMessage, error) {
	var m wireMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return wireMessage{}, fmt.Errorf("lsp: decode message: %w", err)
	}
	return m, nil
}

// wireMessage is the generic JSON-RPC envelope for reading, keeping raw
// params and results so result shapes can be branch-decoded.
type wireMessage struct {
	Jsonrpc string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int64  `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (m wireMessage) hasID() bool    { return len(m.ID) > 0 && string(m.ID) != "null" }
func (m wireMessage) isResp() bool   { return m.Method == "" && m.hasID() }
func (m wireMessage) isSrvReq() bool { return m.Method != "" && m.hasID() }

// readMessage reads a single Length-prefixed message from r.
func readMessage(r *byteReader) ([]byte, error) {
	contentLength := -1
	for {
		line, err := r.readLine()
		if err != nil {
			return nil, err
		}
		if len(bytes.TrimSpace(line)) == 0 {
			break // blank line terminates headers
		}
		colon := bytes.IndexByte(line, ':')
		if colon < 0 {
			return nil, fmt.Errorf("lsp: malformed header %q", string(line))
		}
		key := strings.ToLower(strings.TrimSpace(string(line[:colon])))
		val := strings.TrimSpace(string(line[colon+1:]))
		if key == "content-length" {
			contentLength, err = strconv.Atoi(val)
			if err != nil || contentLength < 0 {
				return nil, fmt.Errorf("lsp: bad content-length %q", val)
			}
		}
	}
	if contentLength < 0 {
		return nil, fmt.Errorf("lsp: missing content-length header")
	}
	return r.readN(contentLength)
}
