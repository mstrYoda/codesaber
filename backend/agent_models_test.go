package backend

import (
	"codesaber/backend/acp"
	"context"
	"testing"
)

type fakeModelPrompter struct {
	fakePrompter
	modelState acp.ModelState
	called     bool
	onSet      func()
}

func (f *fakeModelPrompter) Models() acp.ModelState { return f.modelState }
func (f *fakeModelPrompter) SetModel(_ context.Context, id string) (acp.ModelState, error) {
	f.called = true
	if f.onSet != nil {
		f.onSet()
	}
	f.modelState.CurrentID = id
	return f.modelState, nil
}
func TestModelFacadeRejectsStaleSession(t *testing.T) {
	app, _, pid := newAgentTestApp(t)
	f := &fakeModelPrompter{modelState: acp.ModelState{SessionID: "current", CurrentID: "fast"}}
	app.agents[pid] = &agentSession{session: f}
	if _, err := app.ACPSetModel(pid, "old", "deep"); err == nil {
		t.Fatal("stale session accepted")
	}
	if f.called {
		t.Fatal("stale request reached provider")
	}
	if _, err := app.ACPSetModel("missing", "current", "deep"); err == nil {
		t.Fatal("missing project accepted")
	}
}
func TestModelFacadeRejectsReplacedSessionResult(t *testing.T) {
	app, _, pid := newAgentTestApp(t)
	old := &fakeModelPrompter{modelState: acp.ModelState{SessionID: "old", CurrentID: "fast"}}
	ag := &agentSession{session: old}
	app.agents[pid] = ag
	newer := &fakeModelPrompter{modelState: acp.ModelState{SessionID: "new", CurrentID: "fast"}}
	old.onSet = func() { ag.setSession(newer) }
	if _, err := app.ACPSetModel(pid, "old", "deep"); err == nil {
		t.Fatal("result from replaced session accepted")
	}
	if newer.called || newer.modelState.CurrentID != "fast" {
		t.Fatal("new session was changed")
	}
}
