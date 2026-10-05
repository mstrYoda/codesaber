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

// Opt in: this starts the installed language server, not the fake test server.
func TestRealGoplsWindowsPaths(t *testing.T) {
	if os.Getenv("CODESABER_TEST_GOPLS") != "1" {
		t.Skip("set CODESABER_TEST_GOPLS=1 for installed gopls integration")
	}
	binary, err := exec.LookPath("gopls")
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "Go Deneme Çalışması")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	source := "package main\n\nfunc greet() string { return \"merhaba\" }\nfunc main() { _ = greet() }\n"
	path := filepath.Join(root, "main.go")
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/gui-test\n\ngo 1.25\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	client, err := Start(binary, ToURI(root))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	diags := make(chan []Diagnostic, 32)
	client.SetDiagnosticsHandler(func(uri string, entries []Diagnostic) {
		if uri == ToURI(path) {
			select {
			case diags <- entries:
			default:
			}
		}
	})
	if err := client.DidOpen(ToURI(path), 1, source); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	definitions, err := client.Definition(ctx, ToURI(path), 3, 19)
	if err != nil || len(definitions) != 1 || definitions[0].URI != ToURI(path) || definitions[0].Range.Start.Line != 2 {
		t.Fatalf("definition=%+v err=%v", definitions, err)
	}
	hover, err := client.Hover(ctx, ToURI(path), 3, 19)
	if err != nil || hover == nil || !strings.Contains(string(hover.Contents), "greet") {
		t.Fatalf("hover=%+v err=%v", hover, err)
	}
	symbols, err := client.DocumentSymbols(ctx, ToURI(path))
	if err != nil || len(symbols) < 2 {
		t.Fatalf("symbols=%+v err=%v", symbols, err)
	}
	broken := strings.Replace(source, "_ = greet()", "_ = missingName", 1)
	if err := client.DidChange(ToURI(path), 2, broken); err != nil {
		t.Fatal(err)
	}
	found := false
	for !found {
		select {
		case entries := <-diags:
			for _, d := range entries {
				if strings.Contains(d.Message, "missingName") {
					found = true
				}
			}
		case <-ctx.Done():
			t.Fatal("real gopls did not report undefined identifier")
		}
	}
	if err := client.DidChange(ToURI(path), 3, source); err != nil {
		t.Fatal(err)
	}
	for {
		select {
		case entries := <-diags:
			if len(entries) == 0 {
				return
			}
		case <-ctx.Done():
			t.Fatal("real gopls did not clear fixed diagnostics")
		}
	}
}
