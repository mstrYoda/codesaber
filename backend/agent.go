package backend

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"codesaber/backend/acp"
	"codesaber/backend/agentstore"
)

// ACP agent event names (backend→UI).
const (
	EventACPMsg        = "acp.msg"        // {projectId, role, text, kind}
	EventACPTool       = "acp.tool"       // {projectId, toolCallId, title, kind, status, content}
	EventACPPermission = "acp.permission" // {projectId, requestId, options}
	EventACPState      = "acp.state"      // {projectId, state: idle|thinking|harness-down|no-harness}
	EventACPTranscript = "acp.transcript" // {projectId, sessionID, entries}
)

// Agent status values carried by acp.state.
const (
	AgentStateIdle        = "idle"
	AgentStateThinking    = "thinking"
	AgentStateHarnessDown = "harness-down"
	AgentStateNoHarness   = "no-harness"
)

// acpPermissionTimeout bounds how long a permission request blocks the agent
// before it is answered as cancelled.
const acpPermissionTimeout = 60 * time.Second

// acpMaxFileBytes caps agent fs reads: anything larger is rejected to keep
// context windows and memory bounded.
const acpMaxFileBytes = 10 << 20 // 10MB

// acpWriteDiffCaps bound the diff-review payload attached to fs-write
// permission events: cappedText is the per-side slice size used when the
// combined content exceeds totalCap.
const (
	acpWriteDiffTotalCap = 256 << 10 // 256KB
	acpWriteDiffCapped   = 64 << 10  // 64KB per side
)

// acpPrompter is the seam over acp.Session the facade drives (swap for fakes
// in tests).
type acpPrompter interface {
	Prompt(ctx context.Context, text string) (acp.PromptResponse, error)
	Close() error
	SetOnUpdate(func(acp.SessionUpdate))
}

// acpStderrRing extracts the child's stderr ring buffer for error diagnostics
// (*acp.Session backs one via its conn; fakes may not).
type acpStderrRing interface {
	Stderr() string
}

// acpStderrTailCap bounds the stderr tail appended to error surfaces.
const acpStderrTailCap = 800

// stderrTail returns up to acpStderrTailCap trailing characters of the child
// agent's stderr; empty when the prompter does not back a ring.
func stderrTail(s acpPrompter) string {
	ring, ok := s.(acpStderrRing)
	if !ok {
		return ""
	}
	out := ring.Stderr()
	if len(out) > acpStderrTailCap {
		out = out[len(out)-acpStderrTailCap:]
	}
	return out
}

// sessionPrompter adapts *acp.Session to acpPrompter.
type sessionPrompter struct{ s *acp.Session }

func (p *sessionPrompter) Prompt(ctx context.Context, text string) (acp.PromptResponse, error) {
	return p.s.Prompt(ctx, text)
}
func (p *sessionPrompter) Close() error { return p.s.Close() }
func (p *sessionPrompter) SetOnUpdate(f func(acp.SessionUpdate)) {
	p.s.OnUpdate = f
}
func (p *sessionPrompter) Stderr() string { return p.s.ConnStderr() }

// acpStartSession spawns an ACP session; swapped out in tests.
var acpStartSession = func(ctx context.Context, root string, profile acp.Info, handlers acp.ClientHandlers) (acpPrompter, error) {
	s, err := acp.StartSession(ctx, root, profile, handlers)
	if err != nil {
		return nil, err
	}
	return &sessionPrompter{s}, nil
}

// acpPermissionChoice is the user's answer to one pending permission request.
type acpPermissionChoice struct {
	optionID string
	cancel   bool
}

// agentSession is the per-project ACP harness state: the live session, the
// harness profile it was started with, pending permission requests and the
// in-flight prompt turn accumulator.
type agentSession struct {
	mu       sync.Mutex
	session  acpPrompter
	harness  acp.Info
	root     string
	chatID   string // persisted session record the harness writes to
	pending  map[string]chan acpPermissionChoice
	nextID   int
	starting bool // start in flight; guards double-spawn (double-click)

	turnMu    sync.Mutex
	turnText  strings.Builder
	turnSeen  map[string]bool
	titleless map[string]bool // tool ids recorded before any title arrived
}

func (ag *agentSession) get() acpPrompter {
	ag.mu.Lock()
	defer ag.mu.Unlock()
	return ag.session
}

func (ag *agentSession) setSession(s acpPrompter) {
	ag.mu.Lock()
	ag.session = s
	ag.mu.Unlock()
}

// claimStart claims the right to spawn under a.mu (must be held). It returns
// the agent stub and whether the caller should proceed: ok=false means a start
// is already in flight or a session is running (double-click back-off);
// otherwise a fresh stub is created, or a dead stub left by a failed start is
// reused. Failed starts clear the flag via finishStart, so retry works.
func (a *App) claimStart(projectID string, profile acp.Info, root string) (*agentSession, bool) {
	if cur, ok := a.agents[projectID]; ok {
		cur.mu.Lock()
		defer cur.mu.Unlock()
		if cur.starting || cur.session != nil {
			return cur, false
		}
		// dead stub from a failed previous start: reuse it
		cur.starting = true
		cur.harness = profile
		cur.root = root
		return cur, true
	}
	ag := &agentSession{root: root, harness: profile, pending: map[string]chan acpPermissionChoice{}, starting: true}
	a.agents[projectID] = ag
	return ag, true
}

// finishStart clears the starting flag after a spawn attempt.
func (ag *agentSession) finishStart() {
	ag.mu.Lock()
	ag.starting = false
	ag.mu.Unlock()
}

func (ag *agentSession) appendTurn(text string) {
	ag.turnMu.Lock()
	ag.turnText.WriteString(text)
	ag.turnMu.Unlock()
}

// noteTool records a tool call id and title. It reports whether the entry
// should be persisted: on first sight of the id (persist if the title is
// already known), or later when a title first arrives for a previously
// titleless call (first-title-later pattern — the tool entry is not lost just
// because the initial tool_call carried no title).
func (ag *agentSession) noteTool(id, title string) bool {
	ag.turnMu.Lock()
	defer ag.turnMu.Unlock()
	if ag.turnSeen == nil {
		ag.turnSeen = map[string]bool{}
	}
	if ag.turnSeen[id] {
		// already recorded; only re-persist if we skipped a titleless call and
		// the title just arrived (once — then the titleless marker is cleared)
		if title != "" && ag.titleless[id] {
			delete(ag.titleless, id)
			return true
		}
		return false
	}
	ag.turnSeen[id] = true
	if title == "" {
		if ag.titleless == nil {
			ag.titleless = map[string]bool{}
		}
		ag.titleless[id] = true
		return false
	}
	return true
}

func (ag *agentSession) resetTurn() {
	ag.turnMu.Lock()
	ag.turnText.Reset()
	ag.turnSeen = map[string]bool{}
	ag.titleless = map[string]bool{}
	ag.turnMu.Unlock()
}

func (ag *agentSession) takeTurnText() string {
	ag.turnMu.Lock()
	defer ag.turnMu.Unlock()
	s := ag.turnText.String()
	ag.turnText.Reset()
	return s
}

// newChatSessionID mints a persisted-transcript session id. The nanosecond
// timestamp orders sessions lexically; the random suffix rules out collisions
// between two ids minted within the same nanosecond.
func newChatSessionID() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failure is essentially impossible; fall back to the
		// fixed digits of the timestamp itself.
		copy(b[:], fmt.Sprintf("%08x", time.Now().UnixNano()))
	}
	return fmt.Sprintf("ses-%d-%s", time.Now().UnixNano(), hex.EncodeToString(b[:]))
}

// currentChatID returns the persisted session record id under ag.mu.
func (ag *agentSession) currentChatID() string {
	ag.mu.Lock()
	defer ag.mu.Unlock()
	return ag.chatID
}

// activeSessionID returns the persisted session record the project's harness
// is currently writing to ("" when none).
func (a *App) activeSessionID(projectID string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	ag := a.agents[projectID]
	if ag == nil {
		return ""
	}
	return ag.currentChatID()
}

// chatIDForEmit resolves the transcript id to show: the agent's active record
// or, when no harness is running, the project's latest session.
func (a *App) chatIDForEmit(projectID string) string {
	if id := a.activeSessionID(projectID); id != "" {
		return id
	}
	if meta, err := a.chats.LatestSession(projectID); err == nil {
		return meta.ID
	}
	return ""
}

// ACPHarnesses lists known agent profiles with PATH availability.
func (a *App) ACPHarnesses() []acp.Info {
	return acp.DefaultProfiles()
}

// ACPStart resolves the harness profile and spawns one ACP session for the
// project (one session/connection MVP). After start the persisted transcript
// is emitted (acp.transcript) followed by an idle state.
func (a *App) ACPStart(projectID, harnessName string) error {
	profile, err := acp.Resolve(harnessName)
	if err != nil {
		return err
	}
	return a.acpStartProfile(projectID, *profile)
}

func (a *App) acpStartProfile(projectID string, profile acp.Info) error {
	root, err := a.resolveRoot(projectID)
	if err != nil {
		return err
	}

	// Idempotent start: claim under a.mu so a double-click (concurrent
	// ACPStart) cannot double-spawn; the loser re-shows the transcript.
	a.mu.Lock()
	ag, claimed := a.claimStart(projectID, profile, root)
	a.mu.Unlock()
	if !claimed {
		// already running or a start is in flight: idempotent, just re-show
		// the transcript
		a.emitTranscript(projectID, a.chatIDForEmit(projectID))
		return nil
	}
	// clear the flag on every exit path; failed start leaves a dead stub the
	// next claimStart reuses
	defer ag.finishStart()
	return a.acpSpawnOnAgent(projectID, ag, profile)
}

// ACPSendPrompt runs one synchronous prompt turn: the user entry is persisted
// and emitted up front, session/update notifications stream out as acp.msg /
// acp.tool events while the turn is open, and the assembled agent reply is
// persisted once the turn completes.
func (a *App) ACPSendPrompt(projectID, text string) error {
	ag := a.agentFor(projectID)
	if ag == nil {
		return errors.New("acp: no agent session for project; start a harness first")
	}
	s := ag.get()
	if s == nil {
		return errors.New("acp: agent session is down; start a harness first")
	}
	// persisted session id: spawn always mints before any append
	chatID := ag.currentChatID()

	if err := a.chats.Append(chatID, projectID, agentstore.Entry{Role: "user", Kind: agentstore.KindText, Text: text}); err != nil {
		return fmt.Errorf("acp: persist user entry: %w", err)
	}
	a.sink.Emit(EventACPMsg, map[string]any{"projectId": projectID, "role": "user", "text": text, "kind": "text"})
	a.emitState(projectID, AgentStateThinking)

	ag.resetTurn()
	_, err := s.Prompt(context.Background(), text)
	if reply := ag.takeTurnText(); reply != "" {
		_ = a.chats.Append(chatID, projectID, agentstore.Entry{Role: "agent", Kind: agentstore.KindText, Text: reply})
	}
	if err != nil {
		// surface the failure in the chat; the conn is typically dead after
		// this (agent crashed / stream closed). Include the stderr tail for
		// diagnostics.
		msg := err.Error()
		if tail := stderrTail(s); tail != "" {
			msg += "\n[agent stderr] " + tail
		}
		a.sink.Emit(EventACPMsg, map[string]any{"projectId": projectID, "role": "agent", "text": msg, "kind": "error"})
		a.emitState(projectID, AgentStateIdle)
		return nil
	}
	a.emitState(projectID, AgentStateIdle)
	return nil
}

// ACPRespondPermission resolves a pending permission request: optionId picks
// one of the offered options; cancel=true answers {"outcome":"cancelled"}.
func (a *App) ACPRespondPermission(projectID, requestID, optionID string, cancel bool) error {
	ag := a.agentFor(projectID)
	if ag == nil {
		return errors.New("acp: no agent session for project")
	}
	ag.mu.Lock()
	ch, ok := ag.pending[requestID]
	ag.mu.Unlock()
	if !ok {
		return fmt.Errorf("acp: no pending permission %q", requestID)
	}
	ch <- acpPermissionChoice{optionID: optionID, cancel: cancel}
	return nil
}

// ACPNewSession closes the current harness connection and spawns a fresh
// session with the same harness. The transcript is kept; a divider note entry
// marks the boundary.
//
// Get + close + spawn are serialized under a.mu together with claimStart, so a
// concurrent ACPStart / ACPStop cannot interleave: the closed session is never
// resurrected by a stale-closed send, and only one spawn is in flight.
func (a *App) ACPNewSession(projectID string) error {
	a.mu.Lock()
	ag, ok := a.agents[projectID]
	if !ok || ag == nil {
		a.mu.Unlock()
		return errors.New("acp: no running harness for project")
	}
	ag.mu.Lock()
	s := ag.session
	if s == nil {
		ag.mu.Unlock()
		a.mu.Unlock()
		return errors.New("acp: no running harness for project")
	}
	harness := ag.harness
	ag.session = nil
	ag.mu.Unlock()
	// old conn is closed while a.mu is held; the new session claims the stub
	// directly (bypassing the starting-flag back-off, since we hold the slot)
	_ = s.Close()
	// divider note lands in the session it ends; the respawn below mints a
	// fresh record
	ag.mu.Lock()
	oldChatID := ag.chatID
	ag.chatID = ""
	ag.mu.Unlock()
	if oldChatID != "" {
		_ = a.chats.Append(oldChatID, projectID, agentstore.Entry{Role: "system", Kind: agentstore.KindText, Text: "— session ended —"})
	}
	ag.mu.Lock()
	ag.starting = true
	ag.mu.Unlock()
	a.mu.Unlock()

	defer ag.finishStart()
	return a.acpSpawnOnAgent(projectID, ag, harness)
}

// acpSpawnOnAgent spawns a session for an existing (claimed) agentSession
// stub; shared by acpStartProfile and ACPNewSession.
func (a *App) acpSpawnOnAgent(projectID string, ag *agentSession, profile acp.Info) error {
	root, err := a.resolveRoot(projectID)
	if err != nil {
		return err
	}
	ag.mu.Lock()
	ag.root = root
	ag.harness = profile
	ag.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	s, err := acpStartSession(ctx, root, profile, acp.ClientHandlers{
		ReadTextFile: func(_, path string) (string, error) {
			p, cerr := containedPath(root, path)
			if cerr != nil {
				return "", cerr
			}
			info, serr := os.Stat(p)
			if serr != nil {
				return "", fmt.Errorf("acp: stat %s: %w", path, serr)
			}
			if info.Size() > acpMaxFileBytes {
				return "", fmt.Errorf("acp: read %s: file exceeds %d byte cap", path, acpMaxFileBytes)
			}
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				return "", fmt.Errorf("acp: read %s: %w", path, rerr)
			}
			return string(b), nil
		},
		// WriteTextFile is gated: every agent fs/write_text_file surfaces a
		// synchronous permission card to the user (same routing as
		// session/request_permission) before any byte is written. Deny, or no
		// answer within acpPermissionTimeout, rejects the write.
		WriteTextFile: func(_, path, content string) error {
			p, cerr := containedPath(root, path)
			if cerr != nil {
				return cerr
			}
			choice, perr := a.acpFsWritePermission(projectID, ag, path, content)
			if perr != nil {
				return perr
			}
			if !choice {
				return fmt.Errorf("acp: write to %s denied by user", path)
			}
			if derr := os.MkdirAll(filepath.Dir(p), 0o755); derr != nil {
				return fmt.Errorf("acp: mkdir %s: %w", filepath.Dir(p), derr)
			}
			if werr := os.WriteFile(p, []byte(content), 0o644); werr != nil {
				return fmt.Errorf("acp: write %s: %w", path, werr)
			}
			// accepted diff: surface a tool card note (one card per target path)
			toolID := "fsWrite:" + path
			title := "diff applied ✓ — " + path
			if ag.noteTool(toolID, title) {
				chatID := ag.currentChatID()
				_ = a.chats.Append(chatID, projectID, agentstore.Entry{
					Role: "agent", Kind: agentstore.KindTool, Text: title, ToolID: toolID,
				})
			}
			a.sink.Emit(EventACPTool, map[string]any{
				"projectId": projectID, "toolCallId": toolID, "title": title,
				"kind": "edit", "status": "completed", "content": "",
			})
			return nil
		},
		RequestPermission: func(params map[string]any) (any, error) {
			return a.acpRequestPermission(projectID, ag, params)
		},
	})
	if err != nil {
		ag.setSession(nil)
		a.emitState(projectID, AgentStateHarnessDown)
		return acp.ExplainStartError(err)
	}
	s.SetOnUpdate(a.acpUpdateHandler(projectID, ag))
	ag.setSession(s)
	// persisted session record: fresh per harness session
	ag.mu.Lock()
	if ag.chatID == "" {
		ag.chatID = newChatSessionID()
	}
	chatID := ag.chatID
	ag.mu.Unlock()
	_ = a.chats.Append(chatID, projectID, agentstore.Entry{Role: "system", Kind: agentstore.KindText, Text: "— session started —"})
	a.emitTranscript(projectID, chatID)
	a.emitState(projectID, AgentStateIdle)
	return nil
}

// ACPStop closes the harness connection (terminating the child process) and
// reports harness-down.
func (a *App) ACPStop(projectID string) error {
	a.mu.Lock()
	ag, ok := a.agents[projectID]
	delete(a.agents, projectID)
	a.mu.Unlock()
	if !ok || ag == nil {
		return nil
	}
	if s := ag.get(); s != nil {
		_ = s.Close()
	}
	a.emitState(projectID, AgentStateHarnessDown)
	return nil
}

// ACPLoadTranscript returns the persisted entries for the project's active
// (or latest) session.
func (a *App) ACPLoadTranscript(projectID string) ([]agentstore.Entry, error) {
	chatID := a.chatIDForEmit(projectID)
	if chatID == "" {
		return []agentstore.Entry{}, nil
	}
	return a.chats.Read(chatID)
}

// ACPSessions lists the project's persisted agent sessions (newest first).
func (a *App) ACPSessions(projectID string) ([]agentstore.SessionMeta, error) {
	return a.chats.ListSessions(projectID)
}

// ACPOpenSession switches the UI/harness context to a persisted session:
// with a running harness it re-points the transcript and re-emits it; without
// one it just emits the stored transcript (no auto-start).
func (a *App) ACPOpenSession(projectID, sessionID string) error {
	sessions, err := a.chats.ListSessions(projectID)
	if err != nil {
		return fmt.Errorf("acp: list sessions: %w", err)
	}
	found := false
	for _, s := range sessions {
		if s.ID == sessionID {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("acp: no session %q for project", sessionID)
	}
	a.mu.Lock()
	ag := a.agents[projectID]
	if ag != nil {
		ag.mu.Lock()
		ag.chatID = sessionID
		ag.mu.Unlock()
	}
	a.mu.Unlock()
	a.emitTranscript(projectID, sessionID)
	return nil
}

// ACPDeleteSession removes a persisted session. Refuses while a running
// harness is writing to it; afterwards a dead stub's dangling chatID is
// cleared so the next spawn mints a fresh record.
func (a *App) ACPDeleteSession(projectID, sessionID string) error {
	if a.activeSessionID(projectID) == sessionID {
		if ag := a.agentFor(projectID); ag != nil && ag.get() != nil {
			return errors.New("acp: cannot delete the active session while the harness is running")
		}
	}
	if err := a.chats.DeleteSession(sessionID); err != nil {
		return fmt.Errorf("acp: delete session: %w", err)
	}
	if ag := a.agentFor(projectID); ag != nil && ag.currentChatID() == sessionID {
		ag.mu.Lock()
		ag.chatID = ""
		ag.mu.Unlock()
	}
	return nil
}

// ACPRenameSession sets a session's display title.
func (a *App) ACPRenameSession(sessionID, title string) error {
	return a.chats.RenameSession(sessionID, title)
}

// ACPClearTranscript deletes every entry of the active/latest session. Like
// ACPDeleteSession it refuses while a running harness is pointed at that
// session, since deleting the record would strand the harness's writes.
func (a *App) ACPClearTranscript(projectID string) error {
	chatID := a.chatIDForEmit(projectID)
	if chatID == "" {
		return nil
	}
	if a.activeSessionID(projectID) == chatID {
		if ag := a.agentFor(projectID); ag != nil && ag.get() != nil {
			return errors.New("acp: cannot clear the active session while the harness is running")
		}
	}
	if err := a.chats.DeleteSession(chatID); err != nil {
		return fmt.Errorf("acp: clear transcript: %w", err)
	}
	a.emitTranscript(projectID, chatID)
	return nil
}

func (a *App) agentFor(projectID string) *agentSession {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.agents[projectID]
}

// acpRequestPermission registers a pending request, surfaces it to the UI and
// blocks for the user's choice (or the timeout, which answers cancelled).
func (a *App) acpRequestPermission(projectID string, ag *agentSession, params map[string]any) (any, error) {
	ag.mu.Lock()
	ag.nextID++
	id := fmt.Sprintf("perm-%d", ag.nextID)
	ch := make(chan acpPermissionChoice, 1)
	ag.pending[id] = ch
	ag.mu.Unlock()
	defer func() {
		ag.mu.Lock()
		delete(ag.pending, id)
		ag.mu.Unlock()
	}()

	a.sink.Emit(EventACPPermission, map[string]any{
		"projectId": projectID,
		"requestId": id,
		"options":   params["options"],
	})

	select {
	case c := <-ch:
		if c.cancel {
			return nil, nil
		}
		return c.optionID, nil
	case <-time.After(acpPermissionTimeout):
		return nil, nil
	}
}

// acpFsWritePermission gates an agent fs/write_text_file request: it registers
// a pending permission under the same routing as session/request_permission,
// emits acp.permission with purpose=fs-write plus the target path and a
// diff-review payload (old on-disk text vs requested new text, byte-capped),
// and blocks for the user's answer (or the timeout, which denies). Returns
// true only for an explicit allow.
func (a *App) acpFsWritePermission(projectID string, ag *agentSession, path, content string) (bool, error) {
	ag.mu.Lock()
	ag.nextID++
	id := fmt.Sprintf("perm-%d", ag.nextID)
	ch := make(chan acpPermissionChoice, 1)
	ag.pending[id] = ch
	ag.mu.Unlock()
	defer func() {
		ag.mu.Lock()
		delete(ag.pending, id)
		ag.mu.Unlock()
	}()

	oldText := ""
	isNew := true
	ag.mu.Lock()
	root := ag.root
	ag.mu.Unlock()
	if p, cerr := containedPath(root, path); cerr == nil {
		if b, err := os.ReadFile(p); err == nil {
			isNew = false
			oldText = string(b)
		}
	}
	truncated := false
	if len(oldText)+len(content) > acpWriteDiffTotalCap {
		oldText = capDiffText(oldText, acpWriteDiffCapped)
		content = capDiffText(content, acpWriteDiffCapped)
		truncated = true
	}

	a.sink.Emit(EventACPPermission, map[string]any{
		"projectId": projectID,
		"requestId": id,
		"purpose":   "fs-write",
		"path":      path,
		"oldText":   oldText,
		"newText":   content,
		"isNew":     isNew,
		"truncated": truncated,
		"options": []any{
			map[string]any{"optionId": "allow", "name": "Allow", "kind": "allow_once"},
			map[string]any{"optionId": "deny", "name": "Deny", "kind": "reject_once"},
		},
	})

	select {
	case c := <-ch:
		if c.cancel {
			return false, nil
		}
		return c.optionID == "allow", nil
	case <-time.After(acpPermissionTimeout):
		return false, nil
	}
}

// capDiffText slices s to at most max bytes, snapping to the last newline so
// the preview does not end on a partial line.
func capDiffText(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := s[:max]
	if i := strings.LastIndexByte(cut, '\n'); i > 0 {
		cut = cut[:i+1]
	}
	return cut
}

// acpUpdateHandler translates session/update notifications into UI events and
// transcript persistence for the open prompt turn.
func (a *App) acpUpdateHandler(projectID string, ag *agentSession) func(acp.SessionUpdate) {
	return func(u acp.SessionUpdate) {
		switch u.SessionUpdate {
		case acp.UpdateAgentMessageChunk:
			if u.Content == nil || u.Content.Text == "" {
				return
			}
			ag.appendTurn(u.Content.Text)
			a.sink.Emit(EventACPMsg, map[string]any{
				"projectId": projectID, "role": "agent",
				"text": u.Content.Text, "kind": "chunk",
			})
		case acp.UpdateToolCall, acp.UpdateToolCallUpdate:
			id, title := u.ToolCallID, u.Title
			if u.ToolCall != nil {
				if id == "" {
					id = u.ToolCall.ID
				}
				if title == "" {
					title = u.ToolCall.Title
				}
			}
			if id == "" {
				return
			}
			if ag.noteTool(id, title) {
				chatID := ag.currentChatID()
				_ = a.chats.Append(chatID, projectID, agentstore.Entry{
					Role: "agent", Kind: agentstore.KindTool, Text: title, ToolID: id,
				})
			}
			a.sink.Emit(EventACPTool, map[string]any{
				"projectId": projectID, "toolCallId": id, "title": title,
				"kind": u.Kind, "status": u.Status, "content": toolContentText(u),
			})
		}
	}
}

func (a *App) emitTranscript(projectID, chatID string) {
	entries, err := a.chats.Read(chatID)
	if err != nil {
		entries = []agentstore.Entry{}
	}
	a.sink.Emit(EventACPTranscript, map[string]any{"projectId": projectID, "sessionID": chatID, "entries": entries})
}

func (a *App) emitState(projectID, state string) {
	a.sink.Emit(EventACPState, map[string]any{"projectId": projectID, "state": state})
}

// containedPath resolves path for agent fs access, refusing anything outside
// the project root. Both the root and the path are symlink-resolved
// (EvalSymlinks) BEFORE the containment check, so a symlink inside the root
// pointing outside it is rejected. For not-yet-existing paths (write targets)
// the deepest existing ancestor is resolved and the remaining tail is joined
// lexically; a missing path under a resolvable ancestor is accepted.
func containedPath(root, path string) (string, error) {
	if root == "" {
		return "", errors.New("acp: no project root for fs access")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("acp: root %q: %w", root, err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("acp: path %q: %w", path, err)
	}
	// NOTE: no lexical check here — root and path may address the same tree
	// through different aliases (macOS /var vs /private/var), and filepath.Abs
	// already cleans `..`; the resolved containment below is authoritative.
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", fmt.Errorf("acp: root %q: %w", root, err)
	}
	// Resolve symlinks on the deepest existing ancestor; missing tails (write
	// targets) are joined back lexically.
	targetReal, err := evalExisting(abs)
	if err != nil {
		return "", fmt.Errorf("acp: resolve %q: %w", path, err)
	}
	relReal, err := filepath.Rel(rootReal, targetReal)
	if err != nil || relReal == ".." || strings.HasPrefix(relReal, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("acp: path %q resolves outside the project root", path)
	}
	return targetReal, nil
}

// evalExisting symlink-resolves the longest prefix of p that exists and joins
// the unresolved remainder (which contains no symlink components by
// definition, since those components do not exist yet).
func evalExisting(p string) (string, error) {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved, nil
	}
	dir := filepath.Dir(p)
	if dir == p {
		return "", errors.New("acp: reached filesystem root without resolution")
	}
	base := filepath.Base(p)
	parent, err := evalExisting(dir)
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, base), nil
}

// toolContentText joins the text content blocks of a tool call update.
func toolContentText(u acp.SessionUpdate) string {
	blocks := u.ContentItems
	if u.ToolCall != nil && len(u.ToolCall.Content) > 0 {
		blocks = append(append([]acp.ContentBlock(nil), blocks...), u.ToolCall.Content...)
	}
	var sb strings.Builder
	for _, b := range blocks {
		if b.Type == acp.BlockTypeText && b.Text != "" {
			if sb.Len() > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(b.Text)
		}
	}
	return sb.String()
}
