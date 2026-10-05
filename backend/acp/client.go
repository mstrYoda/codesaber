package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

// ClientInfo is the implementation identity sent in initialize params.
const ClientInfo = `{"name":"codesaber","version":"0.1.0"}`

// Session is one ACP conversation lifecycle: spawn -> initialize ->
// session/new -> prompt turns with streamed updates.
type Session struct {
	conn          *Conn
	harness       Info
	id            string
	modelsMu      sync.RWMutex
	models        ModelState
	modelConfigID string

	// OnUpdate receives decoded session/update notifications delivered while
	// a prompt turn is open. Nil is allowed.
	OnUpdate func(SessionUpdate)

	// promptMu guards concurrent prompt turns via promptOpen.
	promptMu   sync.Mutex
	promptOpen bool
}

// StartSession spawns the agent process for profile, runs initialize and
// session/new, and returns the ready Session. The agent->client handlers
// (fs access, permission routing) are invoked on the spawned conn.
func StartSession(ctx context.Context, root string, profile Info, handlers ClientHandlers) (*Session, error) {
	conn, err := Spawn(profile, handlers)
	if err != nil {
		return nil, err
	}
	s, err := initializeAndNew(ctx, conn, profile, root, handlers)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return s, nil
}

// initializeAndNew runs the initialize + session/new handshake on an
// established conn.
func initializeAndNew(ctx context.Context, conn *Conn, profile Info, root string, handlers ClientHandlers) (*Session, error) {
	var clientInfo struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal([]byte(ClientInfo), &clientInfo); err != nil {
		return nil, fmt.Errorf("acp: client info: %w", err)
	}
	initParams := map[string]any{
		"protocolVersion": ProtocolVersion,
		"clientInfo":      clientInfo,
		"clientCapabilities": map[string]any{
			"fs": map[string]any{
				"readTextFile":  handlers.ReadTextFile != nil,
				"writeTextFile": handlers.WriteTextFile != nil,
			},
		},
	}
	if _, err := conn.Request(ctx, MethodInitialize, initParams); err != nil {
		return nil, err
	}
	res, err := conn.Request(ctx, MethodSessionNew, map[string]any{
		"cwd":        root,
		"mcpServers": []any{},
	})
	if err != nil {
		return nil, err
	}
	m, _ := res.(map[string]any)
	if m == nil {
		return nil, errors.New("acp: session/new returned non-object result")
	}
	id, _ := m["sessionId"].(string)
	if id == "" {
		return nil, fmt.Errorf("acp: session/new missing sessionId in %v", res)
	}
	s := &Session{conn: conn, harness: profile, id: id}
	s.readModels(m)
	conn.observerMu.Lock()
	conn.observeUpdate = s.observeModels
	conn.observerMu.Unlock()
	return s, nil
}

// startSessionOnConn is the test seam: drive the handshake on a pre-built conn.
func startSessionOnConn(ctx context.Context, conn *Conn, root string, handlers ClientHandlers) (*Session, error) {
	return initializeAndNew(ctx, conn, Info{}, root, handlers)
}

// ID returns the agent-assigned session id.
func (s *Session) ID() string { return s.id }

// ConnStderr returns the child agent's most recent stderr output (ring buffer)
// for error diagnostics; empty when backed by a pipe-less test conn.
func (s *Session) ConnStderr() string {
	if s.conn == nil {
		return ""
	}
	return s.conn.Stderr()
}

// Prompt sends one turn and blocks until the agent answers with a stop
// reason. session/update notifications are delivered to OnUpdate LIVE as they
// are drained, not batched after the response: the drain goroutine calls
// OnUpdate directly as each frame arrives. Ordering per-turn is kept by
// delivering every update from the ONE drain goroutine; notifications already
// buffered (or racing the response) are drained after the response and
// delivered in order before Prompt returns.
func (s *Session) Prompt(ctx context.Context, text string) (PromptResponse, error) {
	s.promptMu.Lock()
	if s.promptOpen {
		s.promptMu.Unlock()
		return PromptResponse{}, errors.New("acp: prompt already in flight")
	}
	s.promptOpen = true
	s.promptMu.Unlock()
	defer func() {
		s.promptMu.Lock()
		s.promptOpen = false
		s.promptMu.Unlock()
	}()

	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case f := <-s.conn.Notify:
				if f.Method != MethodSessionUpdate || s.OnUpdate == nil {
					continue
				}
				// deliver live, in arrival order, from this single goroutine
				if upd, ok := decodeSessionUpdate(f.Params); ok {
					s.OnUpdate(upd)
				}
			case <-done:
				return
			}
		}
	}()

	res, err := s.conn.Request(ctx, MethodSessionPrompt, NewPromptParams(s.id, []PromptItem{{Type: BlockTypeText, Text: text}}))
	close(done)
	wg.Wait()
	// Trailing notifications already queued when the goroutine exited: drain
	// them here (post-response) and deliver in order, on the caller's
	// goroutine — no other consumer may deliver concurrently because the
	// drain goroutine has terminated and prompt turns are serialized.
	for {
		select {
		case f := <-s.conn.Notify:
			if f.Method != MethodSessionUpdate || s.OnUpdate == nil {
				continue
			}
			if upd, ok := decodeSessionUpdate(f.Params); ok {
				s.OnUpdate(upd)
			}
		default:
			return decodePromptResponse(res, err)
		}
	}
}

// Cancel sends the session/cancel notification for an in-flight turn.
func (s *Session) Cancel() error {
	return s.conn.NotifySend(MethodSessionCancel, map[string]any{"sessionId": s.id})
}

// Close terminates the agent process.
func (s *Session) Close() error { return s.conn.Close() }

// decodePromptResponse interprets the session/prompt result. A nil err with a
// nil res means the request itself failed (err carries the reason).
func decodePromptResponse(res any, err error) (PromptResponse, error) {
	if err != nil {
		return PromptResponse{}, err
	}
	m, _ := res.(map[string]any)
	if m == nil {
		return PromptResponse{}, errors.New("acp: session/prompt returned non-object result")
	}
	stop, _ := m["stopReason"].(string)
	if stop == "" {
		return PromptResponse{}, fmt.Errorf("acp: session/prompt missing stopReason in %v", res)
	}
	return PromptResponse{StopReason: stop}, nil
}

// decodeSessionUpdate tolerates both wire forms: nested
// {sessionId, update: SessionUpdate} (v1 spec) and the flat shape with the
// sessionUpdate discriminator.
func decodeSessionUpdate(params any) (SessionUpdate, bool) {
	if params == nil {
		return SessionUpdate{}, false
	}
	switch m := params.(type) {
	case map[string]any:
		if nested, ok := m["update"]; ok {
			params = nested
		}
	case SessionUpdate:
		return m, true
	}
	b, err := json.Marshal(params)
	if err != nil {
		return SessionUpdate{}, false
	}
	var su SessionUpdate
	if err := json.Unmarshal(b, &su); err != nil {
		return SessionUpdate{}, false
	}
	return su, true
}
