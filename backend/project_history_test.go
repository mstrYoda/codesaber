package backend

import (
	"path/filepath"
	"testing"

	"codesaber/backend/agentstore"
)

func TestChatHistorySurvivesAppRestart(t *testing.T) {
	root := t.TempDir()
	data := t.TempDir()
	previous := chatStoreFactory
	chatStoreFactory = func() (*agentstore.Store, error) {
		return agentstore.OpenStoreAt(filepath.Join(data, "chats"))
	}
	t.Cleanup(func() { chatStoreFactory = previous })
	recents := filepath.Join(data, "recents.json")
	first := NewWith(&fakeSink{}, recents)
	p, err := first.OpenProject(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.chats.Append("saved-session", p.ID, agentstore.Entry{
		Role: "agent", Kind: agentstore.KindText, Text: "saved reply",
	}); err != nil {
		t.Fatal(err)
	}
	first.CloseWatcher(p.ID)
	if err := first.chats.Close(); err != nil {
		t.Fatal(err)
	}

	second := NewWith(&fakeSink{}, recents)
	t.Cleanup(func() {
		for _, project := range second.reg.List() {
			second.CloseWatcher(project.ID)
		}
		second.chats.Close()
	})
	reopened, err := second.OpenProject(root)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.ID != p.ID {
		t.Fatalf("project identity changed: %s -> %s", p.ID, reopened.ID)
	}
	sessions, err := second.ACPSessions(reopened.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ID != "saved-session" || sessions[0].MessageCount != 1 {
		t.Fatalf("saved transcript is missing after restart: %+v", sessions)
	}
	// Reopening within the same process must re-enable watcher events too.
	second.CloseWatcher(reopened.ID)
	if err := second.reg.Remove(reopened.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := second.OpenProject(root); err != nil {
		t.Fatal(err)
	}
	if second.isClosed(reopened.ID) {
		t.Fatal("reopened project remains marked closed")
	}
}
