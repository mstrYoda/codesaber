package acp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Explicit opt-in: creates a real local agent session, but sends no prompt.
func TestRealAgentHandshake(t *testing.T) {
	name := os.Getenv("CODESABER_TEST_AGENT")
	if name == "" {
		t.Skip("set CODESABER_TEST_AGENT to an installed ACP profile")
	}
	p, err := Resolve(name)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "ACP Deneme Çalışması")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	s, err := StartSession(ctx, root, *p, ClientHandlers{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.conn.Close()
	if s.ID() == "" {
		t.Fatal("empty session ID")
	}
}
