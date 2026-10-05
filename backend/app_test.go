package backend

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"codesaber/backend/agentstore"
	"codesaber/backend/git"
	"codesaber/backend/lsp"
	"codesaber/backend/settings"
)

type fakeSink struct {
	mu     sync.Mutex
	events []fakeEvent
}

type fakeEvent struct {
	name    string
	payload any
}

// Emit may be called from multiple goroutines (e.g. the fswatch forwarder),
// so the fake serializes access to its event log.
func (f *fakeSink) Emit(name string, payload any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, fakeEvent{name: name, payload: payload})
}

func (f *fakeSink) snapshot() []fakeEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeEvent(nil), f.events...)
}

func (f *fakeSink) waitFor(t *testing.T, name string, timeout time.Duration) fakeEvent {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, ev := range f.snapshot() {
			if ev.name == name {
				return ev
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for event %q", name)
	return fakeEvent{}
}

func newTestApp(t *testing.T) (*App, *fakeSink) {
	t.Helper()
	sink := &fakeSink{}
	// Isolate the chat store: the real user DB must never be touched by
	// tests, so every test app gets its own temp-dir store.
	dir := t.TempDir()
	prevFactory := chatStoreFactory
	chatStoreFactory = func() (*agentstore.Store, error) {
		return agentstore.OpenStoreAt(dir)
	}
	t.Cleanup(func() { chatStoreFactory = prevFactory })
	app := NewWith(sink, filepath.Join(t.TempDir(), "recents.json"))
	t.Cleanup(func() {
		for _, p := range app.reg.List() {
			app.CloseWatcher(p.ID)
		}
		app.chats.Close()
	})
	return app, sink
}

func TestListTree_DepthAndDotfiles(t *testing.T) {
	root := t.TempDir()
	mkfile := func(rel string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mkfile("src/main.go")          // depth 2, inside dir
	mkfile("src/deep/deepest.txt") // depth 3 → beyond guard
	mkfile(".env")                 // allowed dotfile depth 1
	mkfile(".gitignore")           // allowed dotfile depth 1
	mkfile("src/.hidden.go")       // skipped dotfile at depth 2
	if err := os.Mkdir(filepath.Join(root, ".github"), 0o755); err != nil {
		t.Fatal(err)
	}

	app, _ := newTestApp(t)
	entries, err := app.ListTree(root)
	if err != nil {
		t.Fatal(err)
	}

	got := ""
	for _, e := range entries {
		got += filepath.ToSlash(e.Path) + "\n"
	}
	wantSubstrings := []string{"/src\n", "src/main.go", "/.env", "/.gitignore"}
	for _, want := range wantSubstrings {
		if !containsPrefix(got, want) {
			t.Fatalf("ListTree missing %q in:\n%s", want, got)
		}
	}
	if containsPrefix(got, "hidden.go") {
		t.Fatalf("dotfile .hidden.go should be skipped in:\n%s", got)
	}
	if containsPrefix(got, "deep/") {
		t.Fatalf("depth 3 should not be walked in:\n%s", got)
	}

	// dirs-first: src (dir) must precede .env (file)
	srcIdx := indexPrefix(got, "/src\n")
	envIdx := indexPrefix(got, "/.env")
	if srcIdx == -1 || envIdx == -1 || srcIdx > envIdx {
		t.Fatalf("dirs should come first:\n%s", got)
	}
}

func containsPrefix(list, want string) bool { return indexPrefix(list, want) != -1 }

func indexPrefix(list, want string) int {
	for i := 0; i+len(want) <= len(list); i++ {
		if list[i:i+len(want)] == want {
			return i
		}
	}
	return -1
}

func TestIndexFiles_SkipsAndCaps(t *testing.T) {
	root := t.TempDir()
	mkfile := func(rel string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mkfile("main.go")
	mkfile("src/deep/deeper/best.go") // full recursion, no depth limit
	mkfile("node_modules/pkg/index.js")
	mkfile("dist/bundle.js")
	mkfile(".git/HEAD")
	mkfile(".hidden")

	app, _ := newTestApp(t)
	files, err := app.IndexFiles(root)
	if err != nil {
		t.Fatal(err)
	}

	got := ""
	for _, f := range files {
		got += filepath.ToSlash(f) + "\n"
	}
	if !containsPrefix(got, "/main.go\n") || !containsPrefix(got, "src/deep/deeper/best.go") {
		t.Fatalf("IndexFiles missing expected files:\n%s", got)
	}
	if containsPrefix(got, "node_modules") || containsPrefix(got, "dist/") {
		t.Fatalf("IndexFiles should skip node_modules/dist:\n%s", got)
	}
	if containsPrefix(got, ".git") || containsPrefix(got, ".hidden") {
		t.Fatalf("IndexFiles should skip dotfiles:\n%s", got)
	}
}

func TestFileSystemChangeEvents(t *testing.T) {
	app, sink := newTestApp(t)
	p, err := app.OpenProject(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := sinkEmit(sink.snapshot(), "project.added"); !ok {
		t.Fatal("expected project.added event")
	}
	target := filepath.Join(p.Root, "hello.txt")
	if err := os.WriteFile(target, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	ev := sink.waitFor(t, "fs.change", 5*time.Second)
	payload, ok := ev.payload.(map[string]string)
	if !ok {
		t.Fatalf("unexpected fs.change payload type %T", ev.payload)
	}
	if payload["projectId"] != p.ID || payload["path"] != target || payload["op"] == "" {
		t.Fatalf("bad fs.change payload: %+v", payload)
	}
}

func TestOpenProject_EmitsEngineStatus(t *testing.T) {
	app, sink := newTestApp(t)
	p, err := app.OpenProject(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ev := sink.waitFor(t, "engine.status", 5*time.Second)
	payload, ok := ev.payload.(map[string]any)
	if !ok {
		t.Fatalf("unexpected engine.status payload type %T", ev.payload)
	}
	if payload["engine"] != "fswatch" || payload["ok"] != true || payload["projectId"] != p.ID {
		t.Fatalf("bad engine.status payload: %+v", payload)
	}
}

func sinkEmit(events []fakeEvent, name string) (fakeEvent, bool) {
	for _, ev := range events {
		if ev.name == name {
			return ev, true
		}
	}
	return fakeEvent{}, false
}

// initRepoGit creates a temp git repo with one committed file, ready for
// Engine/facade tests. Minimal re-creation of the backend/git test fixture.
func initRepoGit(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-q")
	run("checkout", "-q", "-b", "master")
	if err := os.WriteFile(filepath.Join(dir, "init.txt"), []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "init")
	return dir
}

func TestGitFacadeStatusAndStage(t *testing.T) {
	root := initRepoGit(t)
	app, _ := newTestApp(t)
	p, err := app.OpenProject(root)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := app.GitStatus("missing-id"); err == nil {
		t.Fatal("unknown project should error")
	}

	st, err := app.GitStatus(p.ID)
	if err != nil {
		t.Fatalf("GitStatus: %v", err)
	}
	if st.Branch != "master" {
		t.Fatalf("branch = %q, want master", st.Branch)
	}
	if len(st.Untracked)+len(st.Unstaged)+len(st.Staged) != 0 {
		t.Fatalf("expected clean tree, got %+v", st)
	}

	// untracked file shows up
	newFile := filepath.Join(p.Root, "work.txt")
	if err := os.WriteFile(newFile, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err = app.GitStatus(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Untracked) != 1 || st.Untracked[0].Path != "work.txt" {
		t.Fatalf("untracked: %+v", st.Untracked)
	}

	// facade GitLog on initial commit
	log, err := app.GitLog(p.ID, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(log) != 1 || strings.TrimSpace(log[0].Message) != "init" {
		t.Fatalf("GitLog: %+v", log)
	}

	// GitBranches
	branches, err := app.GitBranches(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 1 || branches[0] != "master" {
		t.Fatalf("GitBranches: %v", branches)
	}

	// stage → staged change
	if err := app.GitStage(p.ID, []string{"work.txt"}); err != nil {
		t.Fatal(err)
	}
	st, err = app.GitStatus(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Staged) != 1 || st.Staged[0].Path != "work.txt" {
		t.Fatalf("staged: %+v", st.Staged)
	}

	// diff staged shows the addition
	diff, err := app.GitDiff(p.ID, "work.txt", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.Hunks) != 1 || len(diff.Hunks[0].Lines) == 0 {
		t.Fatalf("GitDiff: %+v", diff)
	}

	// commit → clean
	if err := app.GitCommit(p.ID, "work"); err != nil {
		t.Fatal(err)
	}
	st, err = app.GitStatus(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Untracked)+len(st.Unstaged)+len(st.Staged) != 0 {
		t.Fatalf("expected clean after commit, got %+v", st)
	}
	log, err = app.GitLog(p.ID, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(log) != 2 || strings.TrimSpace(log[0].Message) != "work" {
		t.Fatalf("GitLog after commit: %+v", log)
	}

	// modify → unstaged; unstage via stage+unstage roundtrip
	if err := os.WriteFile(newFile, []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err = app.GitStatus(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Unstaged) != 1 || st.Unstaged[0].Status != 'M' {
		t.Fatalf("unstaged: %+v", st.Unstaged)
	}
	if err := app.GitStage(p.ID, []string{"work.txt"}); err != nil {
		t.Fatal(err)
	}
	if err := app.GitUnstage(p.ID, []string{"work.txt"}); err != nil {
		t.Fatal(err)
	}
	st, err = app.GitStatus(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Staged) != 0 {
		t.Fatalf("staged should be empty after unstage: %+v", st.Staged)
	}

	// unstaged diff
	diff, err = app.GitDiff(p.ID, "work.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.Hunks) == 0 {
		t.Fatalf("GitDiff unstaged empty: %+v", diff)
	}
}

func TestGitFacadeStageHunks(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not found")
	}
	// Repo with a multi-paragraph file so re-editing yields >=2 hunks.
	root := initRepoGit(t)
	var sb strings.Builder
	for i := 1; i <= 30; i++ {
		sb.WriteString("para")
		sb.WriteString(strings.Repeat("a", 40))
		sb.WriteString(" line ")
		sb.WriteString(strconv.Itoa(i))
		sb.WriteString("\n")
		if i%3 == 0 {
			sb.WriteString("\n")
		}
	}
	if err := os.WriteFile(filepath.Join(root, "big.txt"), []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCmd(t, root, "add", ".")
	runGitCmd(t, root, "commit", "-q", "-m", "big")

	app, sink := newTestApp(t)
	p, err := app.OpenProject(root)
	if err != nil {
		t.Fatal(err)
	}
	pid := p.ID

	// Rewrite: distant edits → >=2 hunks.
	raw, err := os.ReadFile(filepath.Join(root, "big.txt"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	lines[0] = "EDITED-FIRST-LINE"
	lines[9] = "EDITED-MID-LINE"
	lines[len(lines)-1] = "EDITED-LAST-LINE"
	if err := os.WriteFile(filepath.Join(root, "big.txt"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Stage the first hunk only.
	if err := app.GitStageHunks(pid, "big.txt", []int{0}); err != nil {
		t.Fatalf("GitStageHunks: %v", err)
	}
	st, err := app.GitStatus(pid)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Staged) != 1 || st.Staged[0].Path != "big.txt" {
		t.Fatalf("staged after hunk stage: %+v", st.Staged)
	}
	diff, err := app.GitDiff(pid, "big.txt", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.Hunks) != 1 || len(diff.Hunks) == 0 {
		t.Fatalf("expected 1 staged hunk, got %+v", diff.Hunks)
	}

	// Unstage it → index matches HEAD (no staged diff).
	if err := app.GitUnstageHunks(pid, "big.txt", []int{0}); err != nil {
		t.Fatalf("GitUnstageHunks: %v", err)
	}
	st, err = app.GitStatus(pid)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Staged) != 0 {
		t.Fatalf("staged should be empty after hunk unstage: %+v", st.Staged)
	}

	// Unknown project → error.
	if err := app.GitStageHunks("missing-id", "big.txt", []int{0}); err == nil {
		t.Fatal("unknown project should error")
	}

	// git.status event fired after hunk ops.
	sink.waitFor(t, "git.status", 5*time.Second)
}

// runGitCmd runs git with test identity; helper for facade tests.
func runGitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func TestGitFacadeBranchesCommitLog(t *testing.T) {
	root := initRepoGit(t)
	app, _ := newTestApp(t)
	p, err := app.OpenProject(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.GitCreateBranch(p.ID, "feature"); err != nil {
		t.Fatal(err)
	}
	if err := app.GitCheckout(p.ID, "feature"); err != nil {
		t.Fatal(err)
	}
	st, err := app.GitStatus(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if st.Branch != "feature" {
		t.Fatalf("branch = %q after checkout, want feature", st.Branch)
	}
	branches, err := app.GitBranches(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 2 || branches[0] != "feature" || branches[1] != "master" {
		t.Fatalf("GitBranches: %v", branches)
	}
	if err := app.GitCheckout(p.ID, "master"); err != nil {
		t.Fatal(err)
	}
	st, _ = app.GitStatus(p.ID)
	if st.Branch != "master" {
		t.Fatalf("branch = %q, want master", st.Branch)
	}
}

func TestEmitGitStatusPayloadAndThrottle(t *testing.T) {
	root := initRepoGit(t)
	app, sink := newTestApp(t)
	p, err := app.OpenProject(root)
	if err != nil {
		t.Fatal(err)
	}
	pid := p.ID

	countStatus := func() int {
		n := 0
		for _, ev := range sink.snapshot() {
			if ev.name == "git.status" {
				n++
			}
		}
		return n
	}

	app.emitGitStatus(pid)
	ev := sink.waitFor(t, "git.status", 5*time.Second)
	payload, ok := ev.payload.(map[string]any)
	if !ok {
		t.Fatalf("unexpected git.status payload type %T", ev.payload)
	}
	if payload["projectId"] != pid {
		t.Fatalf("bad projectId: %+v", payload)
	}
	st, ok := payload["status"].(git.Status)
	if !ok {
		t.Fatalf("bad status type %T", payload["status"])
	}
	if st.Branch != "master" {
		t.Fatalf("branch = %q", st.Branch)
	}

	// immediate second call inside throttle window is dropped
	app.emitGitStatus(pid)
	if n := countStatus(); n != 1 {
		t.Fatalf("throttle should drop immediate second emit, got %d events", n)
	}

	// reset throttle map → next emit passes
	app.mu.Lock()
	delete(app.lastGitEmit, pid)
	app.mu.Unlock()
	app.emitGitStatus(pid)
	if n := countStatus(); n != 2 {
		t.Fatalf("expected 2 git.status events after throttle reset, got %d", n)
	}
}

func TestEmitGitStatus_TrailingEdge(t *testing.T) {
	root := initRepoGit(t)
	app, sink := newTestApp(t)
	p, err := app.OpenProject(root)
	if err != nil {
		t.Fatal(err)
	}
	pid := p.ID
	// Exercise the throttle with explicit calls, independent of OS-specific
	// filesystem event bursts generated by the stage operation below.
	app.mu.Lock()
	watcher := app.watchers[pid]
	delete(app.watchers, pid)
	app.mu.Unlock()
	if watcher != nil {
		watcher.Close()
	}

	app.emitGitStatus(pid) // leading edge, emits
	sink.waitFor(t, "git.status", 5*time.Second)

	// burst: change + stage inside throttle window → only trailing emit
	if err := os.WriteFile(filepath.Join(root, "init.txt"), []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.GitStage(pid, []string{"init.txt"}); err != nil {
		t.Fatal(err)
	}
	app.emitGitStatus(pid) // inside window → arms trailing timer
	app.emitGitStatus(pid) // pending already armed → ignored

	// trailing emit must carry the final state (staged change) eventually
	deadline := time.Now().Add(5 * time.Second)
	n := 0
	branch := ""
	for time.Now().Before(deadline) {
		n = 0
		hasStaged := false
		for _, ev := range sink.snapshot() {
			if ev.name != "git.status" {
				continue
			}
			n++
			if payload, ok := ev.payload.(map[string]any); ok {
				if st, ok := payload["status"].(git.Status); ok {
					if len(st.Staged) == 1 && st.Staged[0].Path == "init.txt" && st.Branch != "" {
						hasStaged = true
						branch = st.Branch
					}
				}
			}
		}
		if n >= 2 && hasStaged {
			break
		}
		if n > 2 {
			t.Fatalf("too many git.status events: %d", n)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if n < 2 {
		t.Fatalf("trailing emit never arrived, got %d git.status events", n)
	}
	if branch != "master" {
		t.Fatalf("final status missing/invalid")
	}

	// no further emissions once settle (no new events after trailing)
	time.Sleep(gitStatusThrottle + 200*time.Millisecond)
	if n := countEventsNamed(sink.snapshot(), "git.status"); n != 2 {
		t.Fatalf("expected exactly 2 git.status events, got %d", n)
	}
}

func countEventsNamed(events []fakeEvent, name string) int {
	n := 0
	for _, ev := range events {
		if ev.name == name {
			n++
		}
	}
	return n
}

func TestCloseWatcher_ClearsThrottleState(t *testing.T) {
	root := initRepoGit(t)
	app, sink := newTestApp(t)
	p, err := app.OpenProject(root)
	if err != nil {
		t.Fatal(err)
	}
	app.emitGitStatus(p.ID)
	sink.waitFor(t, "git.status", 5*time.Second)
	app.mu.Lock()
	_, has := app.lastGitEmit[p.ID]
	app.mu.Unlock()
	if !has {
		t.Fatal("expected throttle entry to exist before removal")
	}
	if err := app.RemoveProject(p.ID); err != nil {
		t.Fatal(err)
	}
	app.mu.Lock()
	_, hasLast := app.lastGitEmit[p.ID]
	_, hasPending := app.pendingEmit[p.ID]
	app.mu.Unlock()
	if hasLast || hasPending {
		t.Fatal("expected throttle state deleted on project removal")
	}
}

func TestEmitGitStatusErrorEvent(t *testing.T) {
	// project root without a git dir → New fails → git.error
	app, sink := newTestApp(t)
	p, err := app.OpenProject(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app.emitGitStatus(p.ID)
	ev := sink.waitFor(t, "git.error", 5*time.Second)
	payload, ok := ev.payload.(map[string]string)
	if !ok {
		t.Fatalf("unexpected git.error payload type %T", ev.payload)
	}
	if payload["projectId"] != p.ID || payload["message"] == "" {
		t.Fatalf("bad git.error payload: %+v", payload)
	}
}

func TestRemoveProject_StopsWatcher(t *testing.T) {
	app, sink := newTestApp(t)
	p, err := app.OpenProject(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := app.RemoveProject(p.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := sinkEmit(sink.snapshot(), "project.removed"); !ok {
		t.Fatal("expected project.removed event")
	}
	if _, err := app.reg.Get(p.ID); err == nil {
		t.Fatal("project should be removed from registry")
	}
}

func TestEmitLSPDiag_TrailingCarriesLatestPath(t *testing.T) {
	app, sink := newTestApp(t)
	pid := "p1"

	// leading edge: first path emits immediately
	d1 := []lsp.Diagnostic{{Message: "first"}}
	app.emitLSPDiag(pid, "/w/a.go", d1)
	ev, ok := sinkEmit(sink.snapshot(), "lsp.diag")
	if !ok {
		t.Fatal("expected leading lsp.diag emit")
	}
	payload, ok := ev.payload.(map[string]any)
	if !ok || payload["path"] != "/w/a.go" {
		t.Fatalf("leading payload = %#v", ev.payload)
	}

	// two bursts for DIFFERENT paths inside the throttle window; the
	// trailing emit must carry the LATEST path's diagnostics, not the
	// arm-time (first path) value.
	d2 := []lsp.Diagnostic{{Message: "second"}}
	d3 := []lsp.Diagnostic{{Message: "third"}}
	app.emitLSPDiag(pid, "/w/b.go", d2) // arms trailing timer
	app.emitLSPDiag(pid, "/w/c.go", d3) // overwrites pending latest

	deadline := time.Now().Add(2 * time.Second)
	var trailing map[string]any
	for time.Now().Before(deadline) {
		evs := sink.snapshot()
		if len(evs) >= 2 {
			trailing, ok = evs[len(evs)-1].payload.(map[string]any)
			if ok && trailing["path"] == "/w/c.go" {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if trailing == nil || trailing["path"] != "/w/c.go" {
		t.Fatalf("trailing emit did not carry latest path: %#v", trailing)
	}
	ds, ok := trailing["diagnostics"].([]lsp.Diagnostic)
	if !ok || len(ds) != 1 || ds[0].Message != "third" {
		t.Fatalf("trailing diagnostics = %#v", trailing["diagnostics"])
	}

	// exactly two emits total (leading + trailing)
	time.Sleep(lspDiagThrottle + 100*time.Millisecond)
	if n := countEventsNamed(sink.snapshot(), "lsp.diag"); n != 2 {
		t.Fatalf("expected exactly 2 lsp.diag events, got %d", n)
	}
}

// buildFakeLSPApp compiles the lspfake test server (backend/lsp/testdata) for
// facade tests that exercise the real subprocess path end to end.
func buildFakeLSPApp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "lspfake.exe")
	cmd := exec.Command("go", "build", "-o", bin, "./lsp/testdata/lspfake")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build lspfake: %v: %s", err, out)
	}
	return bin
}

func newGoModuleProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module fakeproj\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestLSPFacade_FlowWithFakeServer(t *testing.T) {
	fake := buildFakeLSPApp(t)
	lsp.OptionalBinaryPath = fake
	defer func() { lsp.OptionalBinaryPath = "" }()

	app, sink := newTestApp(t)
	root := newGoModuleProject(t)
	p, err := app.OpenProject(root)
	if err != nil {
		t.Fatal(err)
	}
	defer app.RemoveProject(p.ID)
	path := filepath.Join(root, "main.go")
	content := "package main\n\nfunc main() {}\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// didOpen → state event and version bookkeeping
	if err := app.LSPDidOpen(p.ID, path, 1, content); err != nil {
		t.Fatalf("LSPDidOpen: %v", err)
	}
	ev := sink.waitFor(t, "lsp.state", 5*time.Second)
	st, ok := ev.payload.(map[string]any)
	if !ok || st["running"] != true || st["projectId"] != p.ID {
		t.Fatalf("lsp.state payload = %#v", ev.payload)
	}

	// definition → location translated back to a path
	locs, err := app.LSPDefinition(p.ID, path, 3, 7)
	if err != nil {
		t.Fatalf("LSPDefinition: %v", err)
	}
	if len(locs) != 1 || locs[0].URI != filepath.FromSlash("/x/other.go") || locs[0].Range.Start.Line != 4 {
		t.Fatalf("definition = %#v", locs)
	}

	// hover
	h, err := app.LSPHover(p.ID, path, 0, 0)
	if err != nil || h == nil || string(h.Contents) != `"hover doc"` {
		t.Fatalf("hover = %v err %v", h, err)
	}

	// symbols
	syms, err := app.LSPSymbols(p.ID, path)
	if err != nil || len(syms) != 1 || syms[0].Name != "Main" {
		t.Fatalf("symbols = %#v err %v", syms, err)
	}

	// didChange bumps the version bookkeeping
	if err := app.LSPDidChange(p.ID, path, 2, content+"// edited\n"); err != nil {
		t.Fatalf("LSPDidChange: %v", err)
	}
	app.lspMu.Lock()
	v := app.lspVersions[p.ID][path]
	app.lspMu.Unlock()
	if v != 2 {
		t.Fatalf("version = %d, want 2", v)
	}

	if err := app.LSPDidSave(p.ID, path, content); err != nil {
		t.Fatalf("LSPDidSave: %v", err)
	}

	// lspfake publishes diagnostics on didOpen; the facade must re-emit them
	// as lsp.diag with the file path restored from the URI.
	diagEv := sink.waitFor(t, "lsp.diag", 5*time.Second)
	diag, ok := diagEv.payload.(map[string]any)
	if !ok || diag["projectId"] != p.ID || diag["path"] != path {
		t.Fatalf("lsp.diag payload = %#v", diagEv.payload)
	}
	if ds, ok := diag["diagnostics"].([]lsp.Diagnostic); !ok || len(ds) != 1 || ds[0].Message != "fake diagnostic" {
		t.Fatalf("diagnostics payload = %#v", diag["diagnostics"])
	}

	if err := app.LSPDidClose(p.ID, path); err != nil {
		t.Fatalf("LSPDidClose: %v", err)
	}

	// remove stops the server and clears bookkeeping
	if err := app.RemoveProject(p.ID); err != nil {
		t.Fatalf("RemoveProject: %v", err)
	}
	app.lspMu.Lock()
	_, hasVersion := app.lspVersions[p.ID]
	app.lspMu.Unlock()
	if hasVersion {
		t.Fatal("version bookkeeping survived RemoveProject")
	}
	if c := app.lspMgr.Client(p.ID); c != nil {
		t.Fatal("client survived RemoveProject")
	}
}

func TestLSPFacade_MissingBinaryEmitsFailureState(t *testing.T) {
	lsp.OptionalBinaryPath = ""
	t.Setenv("PATH", "/nonexistent")
	app, sink := newTestApp(t)
	root := newGoModuleProject(t)
	p, err := app.OpenProject(root)
	if err != nil {
		t.Fatal(err)
	}
	err = app.LSPDidOpen(p.ID, filepath.Join(root, "main.go"), 1, "package main\n")
	if err == nil {
		t.Fatal("expected error for missing gopls")
	}
	ev := sink.waitFor(t, "lsp.state", 5*time.Second)
	st, ok := ev.payload.(map[string]any)
	if !ok || st["running"] != false || st["projectId"] != p.ID {
		t.Fatalf("lsp.state payload = %#v", ev.payload)
	}
	if _, hasReason := st["reason"]; !hasReason {
		t.Fatalf("missing reason in payload: %#v", st)
	}
}

func TestLSPFacade_NonGoModuleRoot(t *testing.T) {
	fake := buildFakeLSPApp(t)
	lsp.OptionalBinaryPath = fake
	defer func() { lsp.OptionalBinaryPath = "" }()

	app, sink := newTestApp(t)
	p, err := app.OpenProject(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := app.LSPDidOpen(p.ID, filepath.Join(p.Root, "main.go"), 1, "package main\n"); err == nil {
		t.Fatal("expected error for non-Go-module root")
	} else if !strings.Contains(err.Error(), "not a Go module") {
		t.Fatalf("error = %v", err)
	}
	ev := sink.waitFor(t, "lsp.state", 5*time.Second)
	st, ok := ev.payload.(map[string]any)
	if !ok || st["running"] != false {
		t.Fatalf("lsp.state payload = %#v", ev.payload)
	}
}

func TestTermLifecycle(t *testing.T) {
	app, sink := newTestApp(t)
	proj, err := app.OpenProject(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	termID, err := app.TermStart(proj.ID, "", "0")
	if err != nil {
		t.Fatal(err)
	}
	defer app.TermStop(termID)
	if termID != proj.ID+"|0" {
		t.Fatalf("termID = %q, want %q", termID, proj.ID+"|0")
	}

	marker := "codesaber-facade-42"
	if err := app.TermInput(termID, []byte("echo "+marker+"\r")); err != nil {
		t.Fatal(err)
	}

	// Wait for the echoed task output, resized mid-flight.
	var gotMarker bool
	go func() {
		time.Sleep(300 * time.Millisecond)
		app.TermResize(termID, 40, 120)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var output []byte
		for _, ev := range sink.snapshot() {
			if ev.name != EventTermData {
				continue
			}
			payload, ok := ev.payload.(map[string]any)
			if !ok || payload["termId"] != termID {
				continue
			}
			if data, ok := payload["data"].([]byte); ok {
				output = append(output, data...)
			}
		}
		if strings.Contains(string(output), marker) {
			gotMarker = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !gotMarker {
		t.Fatal("marker not received")
	}

	if err := app.TermStop(termID); err != nil {
		t.Fatal(err)
	}
	ev := sink.waitFor(t, EventTermExit, 5*time.Second)
	payload := ev.payload.(map[string]any)
	if payload["termId"] != termID {
		t.Fatalf("term.exit payload = %#v", payload)
	}
	// Double TermStop: entry already removed, must report unknown.
	if err := app.TermStop(termID); err == nil {
		t.Fatal("expected error stopping already-released term")
	}
}

func TestTermInputUnknown(t *testing.T) {
	app, _ := newTestApp(t)
	if err := app.TermInput("nope|0", []byte("x")); err == nil {
		t.Fatal("expected error for unknown termID")
	}
	if err := app.TermResize("nope|0", 10, 10); err == nil {
		t.Fatal("expected error for unknown termID")
	}
	if err := app.TermStop("nope|0"); err == nil {
		t.Fatal("expected error for unknown termID")
	}
}

func TestGitFacadeAheadBehindFetchPushStats(t *testing.T) {
	root := initRepoGit(t)
	app, _ := newTestApp(t)
	p, _ := app.OpenProject(root)

	if a, err := app.GitAheadBehind(p.ID); a.Remote != "" || err != nil {
		t.Fatalf("no-upstream should yield Remote \"\", nil err: %+v err=%v", a, err)
	}
	if err := app.GitFetch(p.ID); err == nil {
		t.Fatal("fetch without remote should error")
	}
	if err := app.GitPush(p.ID); err == nil {
		t.Fatal("push without remote should error")
	}
	if err := os.WriteFile(filepath.Join(p.Root, "stats.txt"), []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := app.GitStats(p.ID, []string{"stats.txt"}, false)
	if err != nil {
		t.Fatalf("GitStats: %v", err)
	}
	if got := s["stats.txt"]; got[0] != 3 || got[1] != 0 {
		t.Fatalf("stats: %+v", got)
	}
}

func TestSettingsFacade(t *testing.T) {
	app, _ := newTestApp(t)
	app.settings = settings.NewStore(filepath.Join(t.TempDir(), "settings.json"))

	// Get with no file on disk yields defaults.
	got := app.SettingsGet()
	if want := settings.Default(); got != want {
		t.Fatalf("SettingsGet = %+v, want default %+v", got, want)
	}

	// Put persists (sanitized) and Get reads it back.
	in := settings.Model{BracketColors: false, Minimap: true, EditorFontSizePx: 25, TerminalShell: "/bin/zsh", AccentColor: "#7c3aed"}
	if err := app.SettingsPut(in); err != nil {
		t.Fatalf("SettingsPut: %v", err)
	}
	out := app.SettingsGet()
	if out.BracketColors != false || out.TerminalShell != "/bin/zsh" || out.AccentColor != "#7c3aed" {
		t.Fatalf("SettingsGet after put = %+v", out)
	}
	if out.EditorFontSizePx != 18 {
		t.Fatalf("font size not clamped: %d, want 18", out.EditorFontSizePx)
	}

	// Invalid accent color is dropped, not persisted.
	if err := app.SettingsPut(settings.Model{AccentColor: "purple"}); err != nil {
		t.Fatalf("SettingsPut garbage accent: %v", err)
	}
	if got := app.SettingsGet().AccentColor; got != "" {
		t.Fatalf("garbage accent persisted: %q", got)
	}
}
