package backend

import (
	"reflect"
	"testing"
	"time"

	"codesaber/backend/acp"
	"codesaber/backend/agentstore"
)

func seedTranscript(t *testing.T, app *App, projectID, sessionID string, when time.Time) {
	t.Helper()
	if err := app.chats.Append(sessionID, projectID, agentstore.Entry{
		Role: "user", Kind: agentstore.KindText, Text: sessionID, When: when,
	}); err != nil {
		t.Fatalf("seed transcript: %v", err)
	}
}

// Opening an older chat without a harness only changes the displayed
// transcript. Clear must use that ID, not resolve the project's latest chat.
func TestACPClearTranscriptTargetsOpenedOfflineSession(t *testing.T) {
	app, _, pid := newAgentTestApp(t)
	now := time.Now()
	seedTranscript(t, app, pid, "older", now.Add(-time.Hour))
	seedTranscript(t, app, pid, "latest", now)
	seedTranscript(t, app, "other-project", "other", now)
	latest, err := app.chats.LatestSession(pid)
	if err != nil || latest.ID != "latest" {
		t.Fatalf("latest session = %#v, %v", latest, err)
	}
	before, err := app.chats.Read("latest")
	if err != nil {
		t.Fatal(err)
	}
	if err := app.ACPOpenSession(pid, "older"); err != nil {
		t.Fatalf("open older: %v", err)
	}
	if err := app.ACPClearTranscript(pid, "older"); err != nil {
		t.Fatalf("clear older: %v", err)
	}
	remaining, err := app.ACPSessions(pid)
	if err != nil || len(remaining) != 1 || remaining[0].ID != "latest" {
		t.Fatalf("remaining sessions = %#v, %v", remaining, err)
	}
	after, err := app.chats.Read("latest")
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("latest transcript changed: %#v, %v", after, err)
	}
	entries, err := app.chats.Read("older")
	if err != nil || len(entries) != 0 {
		t.Fatalf("cleared transcript = %#v, %v", entries, err)
	}
	entries, err = app.chats.Read("other")
	if err != nil || len(entries) != 1 || entries[0].Text != "other" {
		t.Fatalf("other project's transcript changed: %#v, %v", entries, err)
	}
}

func TestACPClearTranscriptRejectsInvalidTarget(t *testing.T) {
	for _, target := range []string{"", "missing", "foreign"} {
		t.Run(target, func(t *testing.T) {
			app, _, pid := newAgentTestApp(t)
			seedTranscript(t, app, pid, "own", time.Now())
			seedTranscript(t, app, "other-project", "foreign", time.Now())
			if err := app.ACPClearTranscript(pid, target); err == nil {
				t.Fatal("want error for invalid target")
			}
			for _, id := range []string{"own", "foreign"} {
				entries, err := app.chats.Read(id)
				if err != nil || len(entries) != 1 || entries[0].Text != id {
					t.Fatalf("transcript %q changed: %#v, %v", id, entries, err)
				}
			}
		})
	}
}

func TestACPClearTranscriptRefusesWhileStarting(t *testing.T) {
	app, _, pid := newAgentTestApp(t)
	seedTranscript(t, app, pid, "starting", time.Now())
	app.agents[pid] = &agentSession{chatID: "starting", starting: true}
	if err := app.ACPClearTranscript(pid, "starting"); err == nil {
		t.Fatal("want error clearing a session while its harness starts")
	}
	entries, err := app.chats.Read("starting")
	if err != nil || len(entries) != 1 {
		t.Fatalf("starting transcript changed: %#v, %v", entries, err)
	}
}

func TestACPClearTranscriptAllowsAnotherSessionWhileRunning(t *testing.T) {
	app, _, pid := newAgentTestApp(t)
	installFakeAgent(t, &fakePrompter{})
	seedTranscript(t, app, pid, "older", time.Now().Add(-time.Hour))
	if err := app.acpStartProfile(pid, acp.Info{Name: "fake"}); err != nil {
		t.Fatalf("start: %v", err)
	}
	activeID := app.activeSessionID(pid)
	if err := app.ACPClearTranscript(pid, "older"); err != nil {
		t.Fatalf("clear non-active session: %v", err)
	}
	if err := app.ACPSendPrompt(pid, "still running"); err != nil {
		t.Fatalf("prompt after clear: %v", err)
	}
	remaining, err := app.ACPSessions(pid)
	if err != nil || len(remaining) != 1 || remaining[0].ID != activeID {
		t.Fatalf("remaining sessions = %#v, %v", remaining, err)
	}
	entries, err := app.chats.Read(activeID)
	if err != nil || len(entries) != 2 || entries[1].Text != "still running" {
		t.Fatalf("running transcript = %#v, %v", entries, err)
	}
}

func TestACPClearTranscriptClearsDeadStub(t *testing.T) {
	app, _, pid := newAgentTestApp(t)
	installFakeAgent(t, &fakePrompter{})
	seedTranscript(t, app, pid, "dead", time.Now())
	app.agents[pid] = &agentSession{chatID: "dead"}
	if err := app.ACPClearTranscript(pid, "dead"); err != nil {
		t.Fatalf("clear dead session: %v", err)
	}
	if id := app.activeSessionID(pid); id != "" {
		t.Fatalf("active ID after clear = %q", id)
	}
	if err := app.acpStartProfile(pid, acp.Info{Name: "fake"}); err != nil {
		t.Fatalf("start after clear: %v", err)
	}
	if id := app.activeSessionID(pid); id == "" || id == "dead" {
		t.Fatalf("new harness reused cleared session: %q", id)
	}
}

func TestACPLoadTranscriptIncludesSessionID(t *testing.T) {
	app, _, pid := newAgentTestApp(t)
	transcript, err := app.ACPLoadTranscript(pid)
	if err != nil || transcript.SessionID != "" || transcript.Entries == nil || len(transcript.Entries) != 0 {
		t.Fatalf("empty transcript = %#v, %v", transcript, err)
	}
	now := time.Now()
	seedTranscript(t, app, pid, "older", now.Add(-time.Hour))
	seedTranscript(t, app, pid, "latest", now)
	transcript, err = app.ACPLoadTranscript(pid)
	if err != nil || transcript.SessionID != "latest" || len(transcript.Entries) != 1 || transcript.Entries[0].Text != "latest" {
		t.Fatalf("latest transcript = %#v, %v", transcript, err)
	}
	// A harness may point at a session other than the newest record.
	app.agents[pid] = &agentSession{chatID: "older"}
	transcript, err = app.ACPLoadTranscript(pid)
	if err != nil || transcript.SessionID != "older" || len(transcript.Entries) != 1 || transcript.Entries[0].Text != "older" {
		t.Fatalf("active transcript = %#v, %v", transcript, err)
	}
}
