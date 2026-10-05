package terminal

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func testOpts(t *testing.T) SessionOpts {
	t.Helper()
	dir := t.TempDir()
	return SessionOpts{
		ID:    "test-session",
		Cwd:   dir,
		Shell: defaultShell(),
	}
}

func hasPty(t *testing.T) {
	if runtime.GOOS == "windows" {
		return
	}
	if _, err := os.Stat("/dev/ptmx"); err != nil {
		t.Skipf("pty not available: %v", err)
	}
}

// readUntil accumulates Data events until substr appears or deadline hits.
func readUntil(t *testing.T, data <-chan Event, substr string, deadline time.Duration) string {
	t.Helper()
	var acc []byte
	timer := time.NewTimer(deadline)
	defer timer.Stop()
	for {
		select {
		case ev, ok := <-data:
			if !ok {
				t.Fatalf("stream closed before marker %q", substr)
			}
			if ev.Exit {
				t.Fatalf("exited before marker %q", substr)
			}
			acc = append(acc, ev.Data...)
			if strings.Contains(string(acc), substr) {
				return string(acc)
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for %q; got %q", substr, string(acc))
		}
	}
}

func TestSessionEchoRoundtrip(t *testing.T) {
	hasPty(t)
	s, err := New(testOpts(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	data := s.Data()
	marker := "codesaber-pty-9"
	if err := s.Input([]byte("echo " + marker + "\r")); err != nil {
		t.Fatal(err)
	}
	readUntil(t, data, marker, 5*time.Second)
}

func TestCloseIdempotentAndExit(t *testing.T) {
	hasPty(t)
	s, err := New(testOpts(t))
	if err != nil {
		t.Fatal(err)
	}
	data := s.Data()
	// Drain output in the background so forwards never block Close.
	go func() {
		for range data {
		}
	}()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	// Second close must be safe (no panic, no race).
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestResizeNoPanic(t *testing.T) {
	hasPty(t)
	s, err := New(testOpts(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Resize(40, 120); err != nil {
		t.Fatal(err)
	}
	if err := s.Resize(0, 0); err == nil {
		t.Fatal("expected error for invalid size")
	}
}

func TestCommandExecutesInProjectAndExits(t *testing.T) {
	hasPty(t)
	opts := testOpts(t)
	opts.Cwd = filepath.Join(opts.Cwd, "a dir gün")
	if err := os.MkdirAll(opts.Cwd, 0700); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		opts.Shell = "cmd.exe"
	}
	s, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	exits := make(chan []Event, 1)
	go func() {
		var events []Event
		for ev := range s.Data() {
			if ev.Exit {
				events = append(events, ev)
			}
		}
		exits <- events
	}()
	// A file in the requested cwd proves command execution, unlike a marker
	// in terminal output, which may only be the input echoed by the console.
	if err := s.Input([]byte("echo executed>result.txt\rexit 7\r")); err != nil {
		t.Fatal(err)
	}
	select {
	case events := <-exits:
		if len(events) != 1 || events[0].Code != 7 {
			t.Fatalf("exit events = %#v, want one event with code 7", events)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("shell did not exit")
	}
	b, err := os.ReadFile(filepath.Join(opts.Cwd, "result.txt"))
	if err != nil || strings.TrimSpace(string(b)) != "executed" {
		t.Fatalf("command result = %q, %v", b, err)
	}
}

func TestInvalidShellAndSize(t *testing.T) {
	opts := testOpts(t)
	opts.Shell = filepath.Join(t.TempDir(), "missing-shell")
	if s, err := New(opts); err == nil {
		_ = s.Close()
		t.Fatal("missing shell accepted")
	}
	opts = testOpts(t)
	opts.Rows = 32768
	if s, err := New(opts); err == nil {
		_ = s.Close()
		t.Fatal("oversized terminal accepted")
	}
}
