package lsp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// buildFakeLSP compiles the testdata lspfake binary into a temp dir.
func buildFakeLSP(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "lspfake.exe")
	cmd := exec.Command("go", "build", "-o", bin, "./testdata/lspfake")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build lspfake: %v: %s", err, out)
	}
	return bin
}

func TestProviderEnsureDefinitionFlow(t *testing.T) {
	fake := buildFakeLSP(t)

	m := NewManager(nil)
	OptionalBinaryPath = fake
	defer func() { OptionalBinaryPath = "" }()

	rootURI := toURI(t.TempDir())
	c, err := m.Ensure("proj-1", rootURI)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	uri := toURI("/x/main.go")
	locs, err := c.Definition(ctx, uri, 3, 7)
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	if len(locs) != 1 || locs[0].URI != "file:///x/other.go" || locs[0].Range.Start.Line != 4 {
		t.Fatalf("definition = %#v", locs)
	}
	h, err := c.Hover(ctx, uri, 0, 0)
	if err != nil || h == nil || string(h.Contents) != `"hover doc"` {
		t.Fatalf("hover = %v err %v", h, err)
	}
	syms, err := c.DocumentSymbols(ctx, uri)
	if err != nil || len(syms) != 1 || syms[0].Name != "Main" {
		t.Fatalf("symbols = %#v err %v", syms, err)
	}

	// second Ensure with same project returns the same client
	c2, err := m.Ensure("proj-1", rootURI)
	if err != nil || c2 != c {
		t.Fatalf("ensure idempotency: %v %v", c2 == c, err)
	}

	if err := m.Stop("proj-1"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if err := m.Stop("proj-1"); err != nil { // idempotent
		t.Fatalf("stop twice: %v", err)
	}
}

func TestProviderMissingBinary(t *testing.T) {
	OptionalBinaryPath = ""
	t.Setenv("PATH", "/nonexistent")
	m := NewManager(nil)
	_, err := m.Ensure("proj-2", toURI(t.TempDir()))
	if err == nil {
		t.Fatal("expected error for missing gopls")
	}
	if !contains(err.Error(), "install gopls") {
		t.Errorf("error %q lacks '(install gopls)' hint", err)
	}
}

func TestProviderRemoveStopsClient(t *testing.T) {
	fake := buildFakeLSP(t)
	m := NewManager(nil)
	OptionalBinaryPath = fake
	defer func() { OptionalBinaryPath = "" }()

	if _, err := m.Ensure("proj-3", toURI(t.TempDir())); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	m.Remove("proj-3")
	m.mu.Lock()
	_, present := m.clients["proj-3"]
	m.mu.Unlock()
	if present {
		t.Fatal("client still registered after Remove")
	}
	if err := m.Remove("missing"); err != nil {
		t.Fatalf("remove missing: %v", err)
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
