package backend

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"codesaber/backend/acp"
	"codesaber/backend/agentstore"
)

// fakePrompter is a controllable acpPrompter: Prompt blocks until released
// (if gated) and records calls.
type fakePrompter struct {
	mu      sync.Mutex
	prompts []string
	closed  bool
	block   chan struct{}
	onUpd   func(acp.SessionUpdate)
}

func (f *fakePrompter) Prompt(ctx context.Context, text string) (acp.PromptResponse, error) {
	f.mu.Lock()
	f.prompts = append(f.prompts, text)
	f.mu.Unlock()
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return acp.PromptResponse{}, ctx.Err()
		}
	}
	return acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
}

func (f *fakePrompter) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *fakePrompter) SetOnUpdate(fn func(acp.SessionUpdate)) {
	f.mu.Lock()
	f.onUpd = fn
	f.mu.Unlock()
}

func (f *fakePrompter) emit(u acp.SessionUpdate) {
	f.mu.Lock()
	fn := f.onUpd
	f.mu.Unlock()
	if fn != nil {
		fn(u)
	}
}

// installFakeAgent swaps acpStartSession for a fake returning fp; restores on
// test cleanup. Tests start through acpStartProfile so fake sessions do not
// depend on an installed agent binary. ACPStart discovery is tested separately.
func installFakeAgent(t *testing.T, fp *fakePrompter) {
	t.Helper()
	prev := acpStartSession
	acpStartSession = func(ctx context.Context, root string, profile acp.Info, handlers acp.ClientHandlers) (acpPrompter, error) {
		return fp, nil
	}
	t.Cleanup(func() { acpStartSession = prev })
}

func newAgentTestApp(t *testing.T) (*App, *fakeSink, string) {
	t.Helper()
	app, sink := newTestApp(t)
	root := t.TempDir()
	if _, err := app.OpenProject(root); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	pid := app.reg.List()[0].ID
	return app, sink, pid
}

func TestACPStartEmitsTranscriptAndState(t *testing.T) {
	app, sink, pid := newAgentTestApp(t)
	installFakeAgent(t, &fakePrompter{})

	if err := app.acpStartProfile(pid, acp.Info{Name: "fake"}); err != nil {
		t.Fatalf("ACPStart: %v", err)
	}
	tr := sink.waitFor(t, EventACPTranscript, 2*time.Second)
	m := tr.payload.(map[string]any)
	if m["projectId"] != pid {
		t.Errorf("transcript projectId = %v, want %v", m["projectId"], pid)
	}
	if _, ok := m["entries"].([]agentstore.Entry); !ok {
		t.Errorf("transcript entries type = %T, want []agentstore.Entry", m["entries"])
	}
	if sid, _ := m["sessionID"].(string); sid == "" {
		t.Errorf("transcript payload missing sessionID: %#v", m)
	}
	st := sink.waitFor(t, EventACPState, 2*time.Second)
	sm := st.payload.(map[string]any)
	if sm["state"] != AgentStateIdle {
		t.Errorf("state = %v, want idle", sm["state"])
	}
}

// TestACPStartIdempotent verifies a double ACPStart does not double-spawn:
// the second call backs off and only the first fake session stays alive.
func TestACPStartIdempotent(t *testing.T) {
	app, _, pid := newAgentTestApp(t)
	fp1 := &fakePrompter{}
	var spawnCount int
	prev := acpStartSession
	acpStartSession = func(ctx context.Context, root string, profile acp.Info, handlers acp.ClientHandlers) (acpPrompter, error) {
		spawnCount++
		return fp1, nil
	}
	t.Cleanup(func() { acpStartSession = prev })

	if err := app.acpStartProfile(pid, acp.Info{Name: "fake"}); err != nil {
		t.Fatalf("first ACPStart: %v", err)
	}
	if err := app.acpStartProfile(pid, acp.Info{Name: "fake"}); err != nil {
		t.Fatalf("second ACPStart: %v", err)
	}
	if spawnCount != 1 {
		t.Fatalf("spawn count = %d, want 1 (double start must not re-spawn)", spawnCount)
	}
}

// TestACPStartRetryAfterFailure verifies a failed start clears its claim so a
// subsequent start can spawn again.
func TestACPStartRetryAfterFailure(t *testing.T) {
	app, _, pid := newAgentTestApp(t)
	var spawnCount int
	prev := acpStartSession
	acpStartSession = func(ctx context.Context, root string, profile acp.Info, handlers acp.ClientHandlers) (acpPrompter, error) {
		spawnCount++
		if spawnCount == 1 {
			return nil, errors.New("spawn failed")
		}
		return &fakePrompter{}, nil
	}
	t.Cleanup(func() { acpStartSession = prev })

	if err := app.acpStartProfile(pid, acp.Info{Name: "fake"}); err == nil {
		t.Fatal("want error from failed first start")
	}
	if err := app.acpStartProfile(pid, acp.Info{Name: "fake"}); err != nil {
		t.Fatalf("retry after failure: %v", err)
	}
	if spawnCount != 2 {
		t.Fatalf("spawn count = %d, want 2", spawnCount)
	}
}

func TestACPStartUnknownHarness(t *testing.T) {
	app, _, pid := newAgentTestApp(t)
	if err := app.ACPStart(pid, "does-not-exist"); err == nil {
		t.Fatal("want error for unknown harness")
	}
}

func TestACPStartUnavailableHarness(t *testing.T) {
	app, _, pid := newAgentTestApp(t)
	installFakeAgent(t, &fakePrompter{})
	t.Setenv("PATH", t.TempDir())
	if err := app.ACPStart(pid, "opencode"); err == nil || !strings.Contains(err.Error(), "not found on PATH") {
		t.Fatalf("ACPStart = %v, want unavailable harness error", err)
	}
	if ag := app.agentFor(pid); ag != nil {
		t.Fatal("unavailable harness must not create an agent session")
	}
}

func TestACPSendPromptStreamsAndPersists(t *testing.T) {
	app, sink, pid := newAgentTestApp(t)
	fp := &fakePrompter{}
	installFakeAgent(t, fp)

	if err := app.acpStartProfile(pid, acp.Info{Name: "fake"}); err != nil {
		t.Fatalf("ACPStart: %v", err)
	}
	sink.snapshot() // drain start events

	fp.block = make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- app.ACPSendPrompt(pid, "hello") }()
	time.Sleep(50 * time.Millisecond)
	fp.emit(acp.SessionUpdate{
		SessionUpdate: acp.UpdateAgentMessageChunk,
		Content:       &acp.ContentBlock{Type: acp.BlockTypeText, Text: "hi "},
	})
	fp.emit(acp.SessionUpdate{
		SessionUpdate: acp.UpdateAgentMessageChunk,
		Content:       &acp.ContentBlock{Type: acp.BlockTypeText, Text: "there"},
	})
	fp.emit(acp.SessionUpdate{
		SessionUpdate: acp.UpdateToolCall,
		ToolCallID:    "t-1",
		Title:         "read file",
		Kind:          acp.ToolKindRead,
		Status:        acp.ToolStatusInProgress,
	})
	close(fp.block) // let the prompt turn complete
	<-done

	var msgs, tools int
	var lastUser, chunks string
	var toolPayload map[string]any
	for _, ev := range sink.snapshot() {
		switch ev.name {
		case EventACPMsg:
			msgs++
			m := ev.payload.(map[string]any)
			if m["role"] == "user" && m["text"] == "hello" {
				lastUser = "ok"
			}
			if m["role"] == "agent" && m["kind"] == "chunk" {
				chunks += m["text"].(string)
			}
		case EventACPTool:
			tools++
			toolPayload = ev.payload.(map[string]any)
		case EventACPState:
			m := ev.payload.(map[string]any)
			if m["state"] != AgentStateThinking && m["state"] != AgentStateIdle {
				t.Errorf("unexpected state %v", m["state"])
			}
		}
	}
	if msgs != 3 || lastUser != "ok" {
		t.Errorf("msgs=%d lastUser=%q, want 3 user-ok", msgs, lastUser)
	}
	if chunks != "hi there" {
		t.Errorf("chunks = %q, want %q", chunks, "hi there")
	}
	if tools != 1 || toolPayload["toolCallId"] != "t-1" || toolPayload["status"] != acp.ToolStatusInProgress {
		t.Errorf("tool payload = %#v, want t-1 in_progress", toolPayload)
	}

	// transcript persisted: user entry + assembled agent reply
	transcript, err := app.ACPLoadTranscript(pid)
	if err != nil {
		t.Fatalf("LoadTranscript: %v", err)
	}
	entries := transcript.Entries
	var roles []string
	for _, e := range entries {
		roles = append(roles, e.Role)
	}
	// session-started note + user entry + persisted tool call + agent reply
	if len(roles) != 4 || roles[0] != "system" || roles[1] != "user" || roles[3] != "agent" {
		t.Fatalf("transcript roles = %#v, want [system user agent agent]", roles)
	}
	if entries[3].Text != "hi there" {
		t.Errorf("agent entry text = %q", entries[3].Text)
	}
}

func TestACPSendPromptWithoutSession(t *testing.T) {
	app, _, pid := newAgentTestApp(t)
	if err := app.ACPSendPrompt(pid, "x"); err == nil {
		t.Fatal("want error without running harness")
	}
}

func TestACPPermissionRespondSelection(t *testing.T) {
	app, sink, pid := newAgentTestApp(t)
	fp := &fakePrompter{}
	installFakeAgent(t, fp)

	if err := app.acpStartProfile(pid, acp.Info{Name: "fake"}); err != nil {
		t.Fatalf("ACPStart: %v", err)
	}

	opts := []any{map[string]any{"optionId": "allow-once", "name": "Allow", "kind": "allow_once"}}
	var handlerErr error
	resCh := make(chan any, 1)
	go func() {
		// reach into the agent session's permission handler through the app
		ag := app.agentFor(pid)
		res, err := app.acpRequestPermission(pid, ag, map[string]any{"options": opts})
		handlerErr = err
		resCh <- res
	}()
	ev := sink.waitFor(t, EventACPPermission, 2*time.Second)
	m := ev.payload.(map[string]any)
	reqID, _ := m["requestId"].(string)
	if reqID == "" {
		t.Fatalf("permission event missing requestId: %#v", m)
	}
	if err := app.ACPRespondPermission(pid, reqID, "allow-once", false); err != nil {
		t.Fatalf("ACPRespondPermission: %v", err)
	}
	select {
	case res := <-resCh:
		if res != "allow-once" {
			t.Fatalf("permission result = %v, want allow-once", res)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for permission resolution")
	}
	if handlerErr != nil {
		t.Fatalf("handler err: %v", handlerErr)
	}
}

func TestACPPermissionRespondCancel(t *testing.T) {
	app, sink, pid := newAgentTestApp(t)
	fp := &fakePrompter{}
	installFakeAgent(t, fp)
	if err := app.acpStartProfile(pid, acp.Info{Name: "fake"}); err != nil {
		t.Fatalf("ACPStart: %v", err)
	}

	resCh := make(chan any, 1)
	go func() {
		ag := app.agentFor(pid)
		res, _ := app.acpRequestPermission(pid, ag, map[string]any{"options": []any{}})
		resCh <- res
	}()
	ev := sink.waitFor(t, EventACPPermission, 2*time.Second)
	reqID := ev.payload.(map[string]any)["requestId"].(string)
	if err := app.ACPRespondPermission(pid, reqID, "", true); err != nil {
		t.Fatalf("ACPRespondPermission: %v", err)
	}
	select {
	case res := <-resCh:
		if res != nil {
			t.Fatalf("cancelled result = %v, want nil", res)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestACPRespondPermissionUnknownRequest(t *testing.T) {
	app, _, pid := newAgentTestApp(t)
	fp := &fakePrompter{}
	installFakeAgent(t, fp)
	if err := app.acpStartProfile(pid, acp.Info{Name: "fake"}); err != nil {
		t.Fatalf("ACPStart: %v", err)
	}
	if err := app.ACPRespondPermission(pid, "nope", "allow", false); err == nil {
		t.Fatal("want error for unknown request id")
	}
}

func TestACPNewSessionClosesOldAndKeepsTranscript(t *testing.T) {
	app, sink, pid := newAgentTestApp(t)
	fp1 := &fakePrompter{}
	installFakeAgent(t, fp1)
	if err := app.acpStartProfile(pid, acp.Info{Name: "fake"}); err != nil {
		t.Fatalf("ACPStart: %v", err)
	}
	if err := app.ACPSendPrompt(pid, "first turn"); err != nil {
		t.Fatalf("ACPSendPrompt: %v", err)
	}
	_ = sink
	oldID := app.activeSessionID(pid)

	fp2 := &fakePrompter{}
	installFakeAgent(t, fp2)
	if err := app.ACPNewSession(pid); err != nil {
		t.Fatalf("ACPNewSession: %v", err)
	}
	if !fp1.closed {
		t.Error("old session was not closed")
	}
	if fp2.closed {
		t.Error("new session should be running")
	}
	// the previous record is kept: its tail is the session-ended divider
	oldEntries, err := app.chats.Read(oldID)
	if err != nil {
		t.Fatalf("read old session: %v", err)
	}
	if len(oldEntries) == 0 || oldEntries[len(oldEntries)-1].Text != "— session ended —" {
		t.Fatalf("old session tail = %#v, want divider entry", oldEntries)
	}
	// the fresh record becomes the active transcript
	transcript, err := app.ACPLoadTranscript(pid)
	if err != nil {
		t.Fatalf("LoadTranscript: %v", err)
	}
	entries := transcript.Entries
	if len(entries) != 1 || entries[0].Text != "— session started —" {
		t.Fatalf("transcript tail = %#v, want session-started entry", entries)
	}
}

func TestACPNewSessionWithoutHarness(t *testing.T) {
	app, _, pid := newAgentTestApp(t)
	if err := app.ACPNewSession(pid); err == nil {
		t.Fatal("want error without harness")
	}
}

func TestACPStopClosesSession(t *testing.T) {
	app, sink, pid := newAgentTestApp(t)
	fp := &fakePrompter{}
	installFakeAgent(t, fp)
	if err := app.acpStartProfile(pid, acp.Info{Name: "fake"}); err != nil {
		t.Fatalf("ACPStart: %v", err)
	}
	if err := app.ACPStop(pid); err != nil {
		t.Fatalf("ACPStop: %v", err)
	}
	if !fp.closed {
		t.Error("session not closed by ACPStop")
	}
	st := sink.snapshot()
	last := st[len(st)-1]
	if last.name != EventACPState || last.payload.(map[string]any)["state"] != AgentStateHarnessDown {
		t.Fatalf("last event = %v %#v, want harness-down", last.name, last.payload)
	}
	// stopping again is a no-op
	if err := app.ACPStop(pid); err != nil {
		t.Fatalf("second ACPStop: %v", err)
	}
}

func TestACPStartSpawnFailureReportsHarnessDown(t *testing.T) {
	app, sink, pid := newAgentTestApp(t)
	prev := acpStartSession
	acpStartSession = func(ctx context.Context, root string, profile acp.Info, handlers acp.ClientHandlers) (acpPrompter, error) {
		return nil, errors.New("spawn failed")
	}
	t.Cleanup(func() { acpStartSession = prev })

	if err := app.acpStartProfile(pid, acp.Info{Name: "fake"}); err == nil {
		t.Fatal("want error from failed spawn")
	}
	st := sink.waitFor(t, EventACPState, 2*time.Second)
	if st.payload.(map[string]any)["state"] != AgentStateHarnessDown {
		t.Fatalf("state = %#v, want harness-down", st.payload)
	}
}

func TestContainedPath(t *testing.T) {
	root := t.TempDir()
	rootReal, err := filepath.EvalSymlinks(root) // macOS /var -> /private/var
	if err != nil {
		t.Fatal(err)
	}
	p, err := containedPath(root, filepath.Join(root, "a", "b.txt"))
	if err != nil || p != filepath.Join(rootReal, "a", "b.txt") {
		t.Fatalf("containedPath inside = %v, %v", p, err)
	}
	if _, err := containedPath(root, "/etc/passwd"); err == nil {
		t.Error("want error for path outside root")
	}
	if _, err := containedPath(root, filepath.Join(root, "..", "escape")); err == nil {
		t.Error("want error for traversal")
	}
	if _, err := containedPath("", "x"); err == nil {
		t.Error("want error for empty root")
	}
}

// TestContainedPathSymlinkEscape verifies symlink-blind traversal is rejected:
// a link inside the root pointing outside must not pass containment.
func TestContainedPathSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := containedPath(root, filepath.Join(root, "link", "secret.txt")); err == nil {
		t.Error("want error for symlink escape")
	}
	// existing file reached through the link is also rejected
	victim := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(victim, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := containedPath(root, filepath.Join(root, "link", "secret.txt")); err == nil {
		t.Error("want error for symlink escape to existing file")
	}
	// root itself addressed through a symlink still resolves inside
	rootLink := filepath.Join(t.TempDir(), "rootlink")
	if err := os.Symlink(root, rootLink); err != nil {
		t.Fatal(err)
	}
	if _, err := containedPath(rootLink, filepath.Join(rootLink, "ok.txt")); err != nil {
		t.Errorf("root addressed via symlink rejected: %v", err)
	}
}

// TestContainedPathMissingTailAcceptsWriteTargets verifies not-yet-existing
// write targets resolve via their deepest existing ancestor.
func TestContainedPathMissingTailAcceptsWriteTargets(t *testing.T) {
	root := t.TempDir()
	rootReal, err := filepath.EvalSymlinks(root) // macOS /var -> /private/var
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(rootReal, "newdir", "new.txt")
	p, err := containedPath(root, target)
	if err != nil {
		t.Fatalf("write target rejected: %v", err)
	}
	if p != target {
		t.Fatalf("resolved %q, want %q", p, target)
	}
}

// TestACPReadRejectsOversizedFile verifies the 10MB read cap.
func TestACPReadRejectsOversizedFile(t *testing.T) {
	app, _, pid := newAgentTestApp(t)
	fp := &fakePrompter{}
	installFakeAgent(t, fp)
	root, err := app.resolveRoot(pid)
	if err != nil {
		t.Fatalf("resolveRoot: %v", err)
	}
	big := filepath.Join(root, "big.txt")
	if err := os.WriteFile(big, make([]byte, acpMaxFileBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}

	// capture the read handler registered at start
	var readHandler func(sessionID, path string) (string, error)
	prev := acpStartSession
	acpStartSession = func(ctx context.Context, _ string, _ acp.Info, handlers acp.ClientHandlers) (acpPrompter, error) {
		readHandler = handlers.ReadTextFile
		return fp, nil
	}
	t.Cleanup(func() { acpStartSession = prev })
	if err := app.acpStartProfile(pid, acp.Info{Name: "fake"}); err != nil {
		t.Fatalf("ACPStart: %v", err)
	}
	if readHandler == nil {
		t.Fatal("read handler not captured")
	}
	if _, err := readHandler("s", big); err == nil {
		t.Fatal("want error for oversized read")
	}
	small := filepath.Join(root, "small.txt")
	if err := os.WriteFile(small, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if c, err := readHandler("s", small); err != nil || c != "ok" {
		t.Fatalf("small read = %q, %v", c, err)
	}
}

// TestACPWriteGatedByPermission verifies fs/write_text_file only writes after
// an explicit allow through the permission flow, and rejects otherwise.
func TestACPWriteGatedByPermission(t *testing.T) {
	app, sink, pid := newAgentTestApp(t)
	fp := &fakePrompter{}
	installFakeAgent(t, fp)
	root, err := app.resolveRoot(pid)
	if err != nil {
		t.Fatalf("resolveRoot: %v", err)
	}

	var writeHandler func(sessionID, path, content string) error
	prev := acpStartSession
	acpStartSession = func(ctx context.Context, _ string, _ acp.Info, handlers acp.ClientHandlers) (acpPrompter, error) {
		writeHandler = handlers.WriteTextFile
		return fp, nil
	}
	t.Cleanup(func() { acpStartSession = prev })
	if err := app.acpStartProfile(pid, acp.Info{Name: "fake"}); err != nil {
		t.Fatalf("ACPStart: %v", err)
	}
	if writeHandler == nil {
		t.Fatal("write handler not captured")
	}
	target := filepath.Join(root, "gated.txt")

	// deny: write must fail and no file appear
	done := make(chan error, 1)
	go func() { done <- writeHandler("s", target, "x") }()
	ev := sink.waitFor(t, EventACPPermission, 2*time.Second)
	m := ev.payload.(map[string]any)
	if m["purpose"] != "fs-write" || m["path"] != target {
		t.Fatalf("permission payload = %#v, want purpose=fs-write path=%s", m, target)
	}
	reqID := m["requestId"].(string)
	if err := app.ACPRespondPermission(pid, reqID, "deny", false); err != nil {
		t.Fatalf("respond deny: %v", err)
	}
	if err := <-done; err == nil {
		t.Fatal("want error for denied write")
	}
	if _, serr := os.Stat(target); !os.IsNotExist(serr) {
		t.Fatalf("denied write created file: %v", serr)
	}

	// allow: write succeeds
	go func() { done <- writeHandler("s", target, "hello") }()
	// skip past the drained perm-1 event; the second write re-raises a card
	// under a fresh request id
	var seen string
	deadline := time.Now().Add(2 * time.Second)
	for seen != "perm-2" {
		if time.Now().After(deadline) {
			t.Fatal("second permission event never arrived")
		}
		time.Sleep(10 * time.Millisecond)
		for _, ev := range sink.snapshot() {
			if ev.name != EventACPPermission {
				continue
			}
			m := ev.payload.(map[string]any)
			if id, _ := m["requestId"].(string); id != "" {
				seen = id
			}
		}
	}
	if err := app.ACPRespondPermission(pid, seen, "allow", false); err != nil {
		t.Fatalf("respond allow: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("allowed write: %v", err)
	}
	b, rerr := os.ReadFile(target)
	if rerr != nil || string(b) != "hello" {
		t.Fatalf("file = %q, %v", b, rerr)
	}

	// symlink escape blocked even with permission
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "esc")); err != nil {
		t.Fatal(err)
	}
	if err := writeHandler("s", filepath.Join(root, "esc", "f.txt"), "x"); err == nil {
		t.Fatal("want error for symlink escape write")
	}
}

// waitForPermission returns the latest fs-write permission event whose isNew
// and truncated flags match want (waitFor returns the FIRST match, which
// would race with the prior card when the sink still holds earlier events).
func waitForPermission(t *testing.T, sink *fakeSink, isNew, truncated bool) fakeEvent {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		evs := sink.snapshot()
		for i := len(evs) - 1; i >= 0; i-- {
			ev := evs[i]
			if ev.name != EventACPPermission {
				continue
			}
			m := ev.payload.(map[string]any)
			if m["isNew"] == isNew && m["truncated"] == truncated {
				return ev
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for permission event isNew=%v truncated=%v", isNew, truncated)
	return fakeEvent{}
}

// TestACPWritePermissionDiffPayload verifies the fs-write permission event
// carries the diff-review fields: old on-disk text, requested new text,
// isNew, and the truncated flag once the combined size exceeds the cap.
func TestACPWritePermissionDiffPayload(t *testing.T) {
	app, sink, pid := newAgentTestApp(t)
	fp := &fakePrompter{}
	installFakeAgent(t, fp)
	root, err := app.resolveRoot(pid)
	if err != nil {
		t.Fatalf("resolveRoot: %v", err)
	}

	var writeHandler func(sessionID, path, content string) error
	prev := acpStartSession
	acpStartSession = func(ctx context.Context, root string, profile acp.Info, handlers acp.ClientHandlers) (acpPrompter, error) {
		writeHandler = handlers.WriteTextFile
		return fp, nil
	}
	t.Cleanup(func() { acpStartSession = prev })
	if err := app.acpStartProfile(pid, acp.Info{Name: "fake"}); err != nil {
		t.Fatalf("ACPStart: %v", err)
	}

	// new file: isNew + empty oldText
	target := filepath.Join(root, "fresh.txt")
	done := make(chan error, 1)
	go func() { done <- writeHandler("s", target, "line1\nline2") }()
	ev := waitForPermission(t, sink, true, false)
	m := ev.payload.(map[string]any)
	if m["isNew"] != true || m["oldText"] != "" || m["newText"] != "line1\nline2" || m["truncated"] != false {
		t.Fatalf("new-file payload = %#v, want isNew oldText='' newText=full truncated=false", m)
	}
	reqID := m["requestId"].(string)
	if err := app.ACPRespondPermission(pid, reqID, "deny", false); err != nil {
		t.Fatalf("respond: %v", err)
	}
	<-done

	// existing file: oldText from disk, isNew=false
	if err := os.WriteFile(target, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	go func() { done <- writeHandler("s", target, "new\n") }()
	ev2 := waitForPermission(t, sink, false, false)
	m2 := ev2.payload.(map[string]any)
	if m2["isNew"] != false || m2["oldText"] != "old\n" || m2["newText"] != "new\n" {
		t.Fatalf("edit payload = %#v, want isNew=false oldText='old\\n' newText='new\\n'", m2)
	}
	reqID = m2["requestId"].(string)
	if err := app.ACPRespondPermission(pid, reqID, "deny", false); err != nil {
		t.Fatalf("respond: %v", err)
	}
	<-done

	// oversized: truncated=true and both sides capped to the 64KB budget
	big := strings.Repeat("x", 200<<10) + "\n"
	go func() { done <- writeHandler("s", target, big+big) }()
	ev3 := waitForPermission(t, sink, false, true)
	m3 := ev3.payload.(map[string]any)
	if m3["truncated"] != true {
		t.Fatalf("oversized payload truncated = %v, want true", m3["truncated"])
	}
	if ot, _ := m3["oldText"].(string); len(ot) > 64<<10 {
		t.Fatalf("capped oldText too long: %d", len(ot))
	}
	if nt, _ := m3["newText"].(string); len(nt) > 64<<10 {
		t.Fatalf("capped newText too long: %d", len(nt))
	}
	reqID = m3["requestId"].(string)
	if err := app.ACPRespondPermission(pid, reqID, "deny", false); err != nil {
		t.Fatalf("respond: %v", err)
	}
	<-done
}

// TestNoteToolFirstTitleLater verifies the first-title-later pattern: a
// tool_call with no title is not persisted until the title arrives, then
// persisted exactly once.
func TestNoteToolFirstTitleLater(t *testing.T) {
	ag := &agentSession{}
	if ag.noteTool("t-1", "") {
		t.Fatal("titleless first sight must not persist")
	}
	if ag.noteTool("t-1", "") {
		t.Fatal("repeat titleless sight must not persist")
	}
	if !ag.noteTool("t-1", "read file") {
		t.Fatal("title arrival after titleless record must persist")
	}
	if ag.noteTool("t-1", "read file") {
		t.Fatal("already-persisted tool must not re-persist")
	}
	// titled first sight persists immediately, once
	if !ag.noteTool("t-2", "edit file") {
		t.Fatal("titled first sight must persist")
	}
	if ag.noteTool("t-2", "edit file") {
		t.Fatal("repeat titled sight must not re-persist")
	}
	ag.resetTurn()
	if !ag.noteTool("t-1", "again") {
		t.Fatal("resetTurn must clear the seen set")
	}
}

func TestToolContentText(t *testing.T) {
	u := acp.SessionUpdate{
		ContentItems: []acp.ContentBlock{{Type: acp.BlockTypeText, Text: "line1"}},
		ToolCall: &acp.ToolCall{
			Content: []acp.ContentBlock{{Type: acp.BlockTypeText, Text: "line2"}},
		},
	}
	if got := toolContentText(u); got != "line1\nline2" {
		t.Fatalf("toolContentText = %q", got)
	}
	if got := toolContentText(acp.SessionUpdate{}); got != "" {
		t.Fatalf("empty toolContentText = %q", got)
	}
}

// TestACPPromptErrorSurfacedInChat checks a Prompt failure lands as an error
// chat entry instead of a failed RPC.
func TestACPPromptErrorSurfacedInChat(t *testing.T) {
	app, sink, pid := newAgentTestApp(t)
	fp := &fakePrompter{}
	installFakeAgent(t, fp)
	if err := app.acpStartProfile(pid, acp.Info{Name: "fake"}); err != nil {
		t.Fatalf("ACPStart: %v", err)
	}

	// make the fake fail
	boom := &boomPrompter{}
	app.agentFor(pid).setSession(boom)
	sink.snapshot()

	if err := app.ACPSendPrompt(pid, "go"); err != nil {
		t.Fatalf("ACPSendPrompt returned error: %v (want chat-surfaced)", err)
	}
	var sawErr bool
	for _, ev := range sink.snapshot() {
		if ev.name == EventACPMsg {
			m := ev.payload.(map[string]any)
			if m["kind"] == "error" {
				sawErr = true
			}
		}
	}
	if !sawErr {
		t.Fatal("no error chat event emitted")
	}
}

// TestStderrTailCapsAndSkips verifies the stderr tail helper: capped length,
// passthrough under the cap, and empty for prompters without a ring.
func TestStderrTailCapsAndSkips(t *testing.T) {
	if got := stderrTail(&fakePrompter{}); got != "" {
		t.Errorf("stderrTail(fake) = %q, want empty", got)
	}
	big := &bigStderrPrompter{out: strings.Repeat("x", acpStderrTailCap+100)}
	if got := stderrTail(big); len(got) != acpStderrTailCap {
		t.Errorf("stderrTail(big) len = %d, want %d", len(got), acpStderrTailCap)
	}
	small := &bigStderrPrompter{out: "boom"}
	if got := stderrTail(small); got != "boom" {
		t.Errorf("stderrTail(small) = %q, want boom", got)
	}
}

type bigStderrPrompter struct{ out string }

func (b *bigStderrPrompter) Prompt(ctx context.Context, text string) (acp.PromptResponse, error) {
	return acp.PromptResponse{}, nil
}
func (b *bigStderrPrompter) Close() error                        { return nil }
func (b *bigStderrPrompter) SetOnUpdate(func(acp.SessionUpdate)) {}
func (b *bigStderrPrompter) Stderr() string                      { return b.out }

type boomPrompter struct{}

func (b *boomPrompter) Prompt(ctx context.Context, text string) (acp.PromptResponse, error) {
	return acp.PromptResponse{}, errors.New("agent exploded")
}
func (b *boomPrompter) Close() error                        { return nil }
func (b *boomPrompter) SetOnUpdate(func(acp.SessionUpdate)) {}

// TestACPStartUserEntryPersisted verifies json round-trip of agentstore.Entry
// through the transcript path (guards the emit payload shape).
func TestACPTranscriptEntryJSONRoundtrip(t *testing.T) {
	e := agentstore.Entry{Role: "agent", Kind: agentstore.KindTool, Text: "t", ToolID: "x", When: time.Now()}
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back agentstore.Entry
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.ToolID != "x" || back.Kind != agentstore.KindTool {
		t.Fatalf("roundtrip mismatch: %#v", back)
	}
}

// ensure os import used (keep imports honest if tests evolve)
var _ = os.Getenv

// TestACPSessionsCRUD verifies the session-record RPCs: a prompt turn creates
// one titled record, rename retitles it, ACPNewSession adds a second record
// and ACPDeleteSession removes a non-active one.
func TestACPSessionsCRUD(t *testing.T) {
	app, _, pid := newAgentTestApp(t)
	installFakeAgent(t, &fakePrompter{})

	if err := app.acpStartProfile(pid, acp.Info{Name: "fake"}); err != nil {
		t.Fatalf("ACPStart: %v", err)
	}
	if err := app.ACPSendPrompt(pid, "fix the login bug"); err != nil {
		t.Fatalf("ACPSendPrompt: %v", err)
	}
	sessions, err := app.ACPSessions(pid)
	if err != nil {
		t.Fatalf("ACPSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(sessions))
	}
	if sessions[0].Title != "fix the login bug" {
		t.Fatalf("title = %q", sessions[0].Title)
	}
	sid := sessions[0].ID

	if err := app.ACPRenameSession(sid, "Login fix"); err != nil {
		t.Fatalf("ACPRenameSession: %v", err)
	}
	sessions, _ = app.ACPSessions(pid)
	if sessions[0].Title != "Login fix" {
		t.Fatalf("title after rename = %q", sessions[0].Title)
	}

	// new session creates a second record
	if err := app.ACPNewSession(pid); err != nil {
		t.Fatalf("ACPNewSession: %v", err)
	}
	sessions, _ = app.ACPSessions(pid)
	if len(sessions) != 2 {
		t.Fatalf("sessions = %d, want 2", len(sessions))
	}

	// delete the non-active one
	other := sessions[0].ID
	if other == app.activeSessionID(pid) {
		other = sessions[1].ID
	}
	if err := app.ACPDeleteSession(pid, other); err != nil {
		t.Fatalf("ACPDeleteSession: %v", err)
	}
	sessions, _ = app.ACPSessions(pid)
	if len(sessions) != 1 {
		t.Fatalf("sessions after delete = %d", len(sessions))
	}
}

// TestACPOpenSessionSwitchesTranscript verifies ACPOpenSession re-points the
// active session record so ACPLoadTranscript returns the opened session's
// entries.
func TestACPOpenSessionSwitchesTranscript(t *testing.T) {
	app, _, pid := newAgentTestApp(t)
	installFakeAgent(t, &fakePrompter{})
	if err := app.acpStartProfile(pid, acp.Info{Name: "fake"}); err != nil {
		t.Fatalf("ACPStart: %v", err)
	}
	if err := app.ACPSendPrompt(pid, "first session prompt"); err != nil {
		t.Fatalf("prompt 1: %v", err)
	}
	if err := app.ACPNewSession(pid); err != nil {
		t.Fatalf("ACPNewSession: %v", err)
	}
	if err := app.ACPSendPrompt(pid, "second session prompt"); err != nil {
		t.Fatalf("prompt 2: %v", err)
	}
	sessions, _ := app.ACPSessions(pid)
	var firstID string
	for _, s := range sessions {
		if s.Title == "first session prompt" {
			firstID = s.ID
		}
	}
	if firstID == "" {
		t.Fatal("first session not found")
	}
	if err := app.ACPOpenSession(pid, firstID); err != nil {
		t.Fatalf("ACPOpenSession: %v", err)
	}
	transcript, err := app.ACPLoadTranscript(pid)
	if err != nil {
		t.Fatalf("LoadTranscript: %v", err)
	}
	entries := transcript.Entries
	found := false
	for _, e := range entries {
		if e.Text == "first session prompt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("opened transcript lacks first-session prompt: %#v", entries)
	}
}

// TestACPClearTranscriptRefusesWhileRunning verifies ACPClearTranscript
// refuses while a running harness points at the active session and clears
// the record after ACPStop.
func TestACPClearTranscriptRefusesWhileRunning(t *testing.T) {
	app, _, pid := newAgentTestApp(t)
	installFakeAgent(t, &fakePrompter{})
	if err := app.acpStartProfile(pid, acp.Info{Name: "fake"}); err != nil {
		t.Fatalf("ACPStart: %v", err)
	}
	if err := app.ACPSendPrompt(pid, "clear me"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	sessions, _ := app.ACPSessions(pid)
	sid := sessions[0].ID
	if err := app.ACPClearTranscript(pid, sid); err == nil {
		t.Fatal("want error clearing the active session while the harness runs")
	}
	if err := app.ACPStop(pid); err != nil {
		t.Fatalf("ACPStop: %v", err)
	}
	if err := app.ACPClearTranscript(pid, sid); err != nil {
		t.Fatalf("clear after stop: %v", err)
	}
	entries, err := app.chats.Read(sid)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries after clear = %#v, want empty", entries)
	}
	// the dangling chatID is cleared, so the next spawn mints fresh
}

// TestACPDeleteSessionRefusesActiveWhileRunning verifies deleting the active
// session is refused while its harness runs and allowed after ACPStop.
func TestACPDeleteSessionRefusesActiveWhileRunning(t *testing.T) {
	app, _, pid := newAgentTestApp(t)
	installFakeAgent(t, &fakePrompter{})
	if err := app.acpStartProfile(pid, acp.Info{Name: "fake"}); err != nil {
		t.Fatalf("ACPStart: %v", err)
	}
	if err := app.ACPSendPrompt(pid, "active session"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	sessions, _ := app.ACPSessions(pid)
	sid := sessions[0].ID
	if err := app.ACPDeleteSession(pid, sid); err == nil {
		t.Fatal("want error deleting the active, running session")
	}
	// after stop it is allowed
	if err := app.ACPStop(pid); err != nil {
		t.Fatalf("ACPStop: %v", err)
	}
	if err := app.ACPDeleteSession(pid, sid); err != nil {
		t.Fatalf("delete after stop: %v", err)
	}
}
