package lsp

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// OptionalBinaryPath overrides the language-server binary lookup when set
// (tests inject a fake server binary here); when empty, the default is
// resolved via exec.LookPath("gopls").
var OptionalBinaryPath string

// defaultBinaryPath is the binary Options wiring in codesaber.json (Phase 6)
// will make configurable. gopls must be on PATH (or in GOPATH/bin) otherwise.
const defaultBinaryPath = "gopls"

// goplsBinaryPath resolves the server binary: optional override, then PATH,
// then GOPATH/bin (gopls is commonly installed there but the GUI process may
// not inherit it in PATH).
func goplsBinaryPath() (string, error) {
	if OptionalBinaryPath != "" {
		return OptionalBinaryPath, nil
	}
	bin, err := exec.LookPath(defaultBinaryPath)
	if err == nil {
		return bin, nil
	}
	if out, gerr := exec.Command("go", "env", "GOPATH").Output(); gerr == nil {
		cand := filepath.Join(strings.TrimSpace(string(out)), "bin", defaultBinaryPath)
		if abs, aerr := filepath.Abs(cand); aerr == nil {
			if resolved, serr := exec.LookPath(abs); serr == nil {
				return resolved, nil
			}
		}
	}
	return "", fmt.Errorf("lsp: %q not found (install gopls): %w", defaultBinaryPath, err)
}

// Manager owns one language-server client per project; clients are spawned
// lazily on first use and cleaned up on Stop/RemoveProject.
type Manager struct {
	mu      sync.Mutex
	clients map[string]*Client

	// onDiagnostics receives publishDiagnostics deliveries with the owning
	// project id (nil-safe).
	onDiagnostics func(projectID, uri string, diags []Diagnostic)
}

// NewManager builds a Manager. onDiagnostics may be nil.
func NewManager(onDiagnostics func(projectID, uri string, diags []Diagnostic)) *Manager {
	return &Manager{clients: make(map[string]*Client), onDiagnostics: onDiagnostics}
}

// Ensure returns the running client for projectID, spawning one rooted at
// rootURI if none exists. Errors are wrapped with a hint when the binary
// cannot be found.
func (m *Manager) Ensure(projectID, rootURI string) (*Client, error) {
	bin, err := goplsBinaryPath()
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	if c, ok := m.clients[projectID]; ok {
		m.mu.Unlock()
		return c, nil
	}
	m.mu.Unlock()

	c, err := Start(bin, rootURI)
	if err != nil {
		return nil, fmt.Errorf("lsp: start %s: %w", bin, err)
	}
	if m.onDiagnostics != nil {
		c.SetDiagnosticsHandler(func(uri string, diags []Diagnostic) {
			m.onDiagnostics(projectID, uri, diags)
		})
	}
	m.mu.Lock()
	// A racing Ensure may have registered its client meanwhile; ours loses.
	if old, ok := m.clients[projectID]; ok {
		m.mu.Unlock()
		go func() { _ = c.Close() }()
		return old, nil
	}
	m.clients[projectID] = c
	m.mu.Unlock()
	return c, nil
}

// Client returns the running client for projectID or nil.
func (m *Manager) Client(projectID string) *Client {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.clients[projectID]
}

// Stop terminates the client for projectID, if any.
func (m *Manager) Stop(projectID string) error {
	m.mu.Lock()
	c, ok := m.clients[projectID]
	if ok {
		delete(m.clients, projectID)
	}
	m.mu.Unlock()
	if !ok {
		return nil
	}
	return c.Close()
}

// Remove (used when a project is removed from the workspace) stops any
// running client for the project; always succeeds.
func (m *Manager) Remove(projectID string) error {
	return m.Stop(projectID)
}

// RunProject runs fn with the project's client, ensuring it is running.
func (m *Manager) RunProject(ctx context.Context, projectID, rootURI string, fn func(*Client) error) error {
	c, err := m.Ensure(projectID, rootURI)
	if err != nil {
		return err
	}
	return fn(c)
}
