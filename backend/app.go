// Package backend is the Wails binding facade. Every exported method on App
// becomes an RPC the frontend can call; all state lives in engines
// (project/editor/fswatch) and is namespaced per project by Project.ID.
package backend

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"codesaber/backend/adapter"
	"codesaber/backend/agentstore"
	"codesaber/backend/editor"
	"codesaber/backend/fswatch"
	"codesaber/backend/git"
	"codesaber/backend/lsp"
	"codesaber/backend/project"
	"codesaber/backend/search"
	"codesaber/backend/settings"
	"codesaber/backend/terminal"
)

// maxTreeDepth limits ListTree recursion (root children = depth 1).
const maxTreeDepth = 2

// maxIndexFiles caps IndexFiles output so huge trees can't blow up memory.
const maxIndexFiles = 20000

// skipDirs are directory names never descended into during IndexFiles walks.
// Phase 6 (tree-sitter index) replaces this walk.
var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "dist": true, "build": true,
	"vendor": true, "target": true, ".next": true,
}

// maxFileSize guards ReadFile against dumping huge binaries into memory.
const maxFileSize = 10 << 20 // 10MB

// Entry is a node in a project tree listing. Path is absolute.
type Entry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Dir  bool   `json:"dir"`
}

// EventEngineStatus is emitted when an engine's health changes for a project
// (currently: fswatch watcher startup success/failure).
const EventEngineStatus = "engine.status"

// EventGitStatus carries per-project git state snapshots: {projectId, status}.
const EventGitStatus = "git.status"

// EventGitError reports git failures the UI should surface inline: {projectId, message}.
const EventGitError = "git.error"

// GitSyncInfo is the ahead/behind sync state relative to a remote.
type GitSyncInfo struct {
	Remote string `json:"remote"`
	Branch string `json:"branch"`
	Ahead  int    `json:"ahead"`
	Behind int    `json:"behind"`
}

const maxStatsFiles = 25

// EventLSPDiag carries gopls diagnostics for one file: {projectId, path, diagnostics[]}.
const EventLSPDiag = "lsp.diag"

// EventLSPState reports language-server lifecycle per project:
// {projectId, running, reason?}. reason explains a failure (missing gopls,
// non-Go-module root).
const EventLSPState = "lsp.state"

// EventTermData streams terminal output frames: {projectId, termId, data base64}.
const EventTermData = "term.data"

// EventTermExit reports terminal session termination: {projectId, termId, code}.
const EventTermExit = "term.exit"

// lspDiagThrottle collapses gopls diagnostic bursts per project, mirroring
// the gitStatusThrottle leading/trailing pattern.
const lspDiagThrottle = 200 * time.Millisecond

// gitStatusThrottle collapses fs-event storms: per project, git.status events
// are emitted at most once per window; a change arriving inside the window
// arms one trailing timer so the FINAL state of a burst is always emitted.
const gitStatusThrottle = 400 * time.Millisecond

// App is the main service bound to the UI. Pure facade over engines;
// all engine state is per-project, namespaced by Project.ID.
type App struct {
	sink        adapter.EventSink
	reg         *project.Registry
	store       *project.Store
	buf         *editor.Service
	chats       *agentstore.Store
	lspMgr      *lsp.Manager
	mu          sync.Mutex
	watchers    map[string]*fswatch.Watcher
	closed      map[string]bool
	lastGitEmit map[string]time.Time
	pendingEmit map[string]bool
	agents      map[string]*agentSession

	// termMu guards terminal bookkeeping: sessions keyed by termID and the
	// per-project index of termIDs.
	termMu       sync.Mutex
	sessions     map[string]*terminal.Session
	projectIndex map[string][]string

	// lspMu guards LSP bookkeeping (per-path versions, diag throttles and
	// the last emitted LSP state per project).
	lspMu          sync.Mutex
	lspVersions    map[string]map[string]int
	lastDiagEmit   map[string]time.Time
	lspPendingDiag map[string]bool
	// lspPendingDiagPath/Diags hold the LATEST diagnostics seen while a
	// trailing emit is armed, so the trailing timer emits post-resolution
	// state rather than the arm-time value.
	lspPendingDiagPath   map[string]string
	lspPendingDiagLatest map[string][]lsp.Diagnostic
	// lastLSPState tracks the last emitted (running, reason) per project so
	// emitLSPState only fires on actual state changes.
	lastLSPState map[string]lspState

	// searchMu guards the per-project search cancellation registry: at most
	// one text search runs per project; a new SearchText cancels the previous.
	searchMu      sync.Mutex
	searchCancels map[string]*searchRun

	// settings persists global app preferences (settings.json).
	settings *settings.Store
}

// lspState is the dedup key for EventLSPState emissions.
type lspState struct {
	running bool
	reason  string
}

// New wires the engines together. sink receives all backend→UI events.
func New(sink adapter.EventSink) *App {
	return NewWith(sink, project.DefaultStorePath())
}

// chatStoreFactory opens the chat transcript store; tests swap it for a
// temp-dir-backed (or in-memory) store.
var chatStoreFactory = agentstore.OpenStore

// NewWith is New with an injectable recents store path (for tests).
func NewWith(sink adapter.EventSink, storePath string) *App {
	// The store itself already falls back to an in-memory db when the
	// on-disk one can't be opened; if even opening THAT fails, boot with an
	// in-memory store rather than a nil one (nil would panic on first use).
	chats, err := chatStoreFactory()
	if err != nil {
		log.Printf("agentstore: open failed: %v (chat history will not persist)", err)
		chats, err = agentstore.OpenStoreInMemory()
		if err != nil {
			panic(fmt.Sprintf("agentstore: in-memory fallback failed: %v", err))
		}
	}
	a := &App{
		sink:                 sink,
		reg:                  project.NewRegistry(func() {}),
		store:                project.NewStore(storePath),
		buf:                  editor.New(),
		chats:                chats,
		watchers:             map[string]*fswatch.Watcher{},
		closed:               map[string]bool{},
		agents:               map[string]*agentSession{},
		sessions:             map[string]*terminal.Session{},
		projectIndex:         map[string][]string{},
		lspVersions:          map[string]map[string]int{},
		lastDiagEmit:         map[string]time.Time{},
		lspPendingDiag:       map[string]bool{},
		lspPendingDiagPath:   map[string]string{},
		lspPendingDiagLatest: map[string][]lsp.Diagnostic{},
		lastLSPState:         map[string]lspState{},
		searchCancels:        map[string]*searchRun{},
		settings:             settings.NewStore(settings.DefaultPath()),
	}
	a.lspMgr = lsp.NewManager(a.onLSPDiags)
	return a
}

// OpenProject registers the project, remembers it in recents, starts a
// filesystem watcher and emits "project.added". Watcher events are re-emitted
// as "fs.change" with {projectId, path, op}.
func (a *App) OpenProject(root string) (project.Project, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return project.Project{}, err
	}
	var savedID string
	for _, recent := range a.store.List() {
		if recent.Root == abs {
			savedID = recent.ID
			break
		}
	}
	p, err := a.reg.AddWithID(abs, savedID)
	if err != nil {
		return project.Project{}, err
	}
	a.mu.Lock()
	delete(a.closed, p.ID)
	a.mu.Unlock()
	// Fill in the real branch before Remember/emitting so the recents store
	// and "project.added" payload carry the true value, not the stub.
	if b := git.BranchAt(p.Root); b != "" {
		p.Branch = b
	}
	a.store.Remember(p.ID, p.Root, p.Branch)

	if w, werr := fswatch.New(p.Root); werr == nil {
		a.mu.Lock()
		a.watchers[p.ID] = w
		a.mu.Unlock()
		go a.forwardWatcher(p.ID, w)
		a.sink.Emit(EventEngineStatus, map[string]any{
			"engine": "fswatch", "ok": true, "projectId": p.ID,
		})
	} else {
		a.reg.SetEngineOK(p.ID, false)
		a.sink.Emit(EventEngineStatus, map[string]any{
			"engine": "fswatch", "ok": false, "projectId": p.ID,
		})
		a.sink.Emit("engine.error", map[string]string{"projectId": p.ID, "message": werr.Error()})
	}

	a.sink.Emit(project.EventAdded, p)
	return *p, nil
}

func (a *App) forwardWatcher(projectID string, w *fswatch.Watcher) {
	for ev, ok := <-w.Events(); ok; ev, ok = <-w.Events() {
		if a.isClosed(projectID) {
			continue
		}
		a.sink.Emit("fs.change", map[string]string{
			"projectId": projectID,
			"path":      ev.Path,
			"op":        ev.Op,
		})
		// Off the watcher goroutine: git status on a slow repo must not
		// stall fs.change forwarding.
		go a.emitGitStatus(projectID)
	}
}

// PickFolder opens a native folder-picker and returns the chosen absolute
// path, or an empty string if the user cancelled the dialog.
func (a *App) PickFolder() (string, error) {
	return adapter.PickFolder("Choose Project Folder")
}

// ListProjects returns currently open projects, most recently used first.
func (a *App) ListProjects() []project.Project {
	return a.reg.List()
}

// RecentProjects returns the persisted recents list.
func (a *App) RecentProjects() []project.Recent {
	return a.store.List()
}

// ForgetRecent drops a recents entry by root path.
func (a *App) ForgetRecent(root string) {
	a.store.Forget(root)
}

// RemoveProject closes the project: registry removal, LSP shutdown, watcher
// shutdown and "project.removed" emission. LSP-stop failure never aborts the
// remaining cleanup steps — the project is going away either way.
func (a *App) RemoveProject(id string) error {
	p, err := a.reg.Get(id)
	if err != nil {
		return err
	}
	a.mu.Lock()
	if err := a.reg.Remove(id); err != nil {
		a.mu.Unlock()
		return err
	}
	a.buf.RemoveProject(id)
	a.mu.Unlock()
	lspErr := a.lspMgr.Remove(id)
	a.lspMu.Lock()
	delete(a.lspVersions, id)
	delete(a.lastDiagEmit, id)
	delete(a.lspPendingDiag, id)
	delete(a.lspPendingDiagPath, id)
	delete(a.lspPendingDiagLatest, id)
	delete(a.lastLSPState, id)
	a.lspMu.Unlock()
	a.searchMu.Lock()
	if run := a.searchCancels[id]; run != nil {
		run.cancel()
	}
	delete(a.searchCancels, id)
	a.searchMu.Unlock()
	a.CloseWatcher(id)
	a.stopProjectTerms(id)
	_ = a.ACPStop(id)
	a.sink.Emit(project.EventRemoved, map[string]any{"id": id, "root": p.Root})
	if len(a.reg.List()) == 0 {
		adapter.ShowWelcomeWindow()
	}
	return lspErr
}

// CloseWatcher stops and releases the watcher for projectID and marks the
// project closed so the forwarder suppresses any buffered events.
func (a *App) CloseWatcher(projectID string) {
	a.mu.Lock()
	w, ok := a.watchers[projectID]
	delete(a.watchers, projectID)
	a.closed[projectID] = true
	delete(a.lastGitEmit, projectID)
	delete(a.pendingEmit, projectID)
	a.mu.Unlock()
	if ok {
		w.Close()
	}
}

func (a *App) isClosed(projectID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.closed[projectID]
}

// ListTree walks root recursively to maxTreeDepth, dirs-first sorted, skipping
// dotfiles except .env, .gitignore and .github.
func (a *App) ListTree(root string) ([]Entry, error) {
	return walk("", root, 0)
}

func walk(projectRoot, dir string, depth int) ([]Entry, error) {
	if depth >= maxTreeDepth {
		return nil, nil
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	var dirs, files []Entry
	for _, e := range ents {
		name := e.Name()
		if !visible(name) {
			continue
		}
		path := filepath.Join(dir, name)
		entry := Entry{
			Name: name,
			Path: path, // os.ReadDir on an absolute dir yields absolute paths already
			Dir:  e.IsDir(),
		}
		if e.IsDir() {
			dirs = append(dirs, entry)
		} else {
			files = append(files, entry)
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name < dirs[j].Name })
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })

	var out []Entry
	for _, entry := range dirs {
		out = append(out, entry)
		children, err := walk(projectRoot, entry.Path, depth+1)
		if err != nil {
			return nil, err
		}
		out = append(out, children...)
	}
	out = append(out, files...)
	return out, nil
}

var allowedDots = map[string]bool{".env": true, ".gitignore": true, ".github": true}

func visible(name string) bool {
	if !strings.HasPrefix(name, ".") {
		return true
	}
	return allowedDots[name]
}

// IndexFiles returns all file paths under root (absolute), walking the full
// tree recursively. Skips .git, node_modules, dist, build, vendor, target and
// .next directories, plus all other dotfiles; capped at maxIndexFiles.
// Phase 6 (tree-sitter index) replaces this walk.
func (a *App) IndexFiles(root string) ([]string, error) {
	var out []string
	err := indexWalk(root, &out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func indexWalk(dir string, out *[]string) error {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read %s: %w", dir, err)
	}
	for _, e := range ents {
		name := e.Name()
		if strings.HasPrefix(name, ".") || (e.IsDir() && skipDirs[name]) {
			continue
		}
		path := filepath.Join(dir, name)
		if e.IsDir() {
			if err := indexWalk(path, out); err != nil {
				return err
			}
		} else {
			*out = append(*out, path)
			if len(*out) >= maxIndexFiles {
				return nil
			}
		}
	}
	return nil
}

// ReadFile returns file content, refusing files larger than maxFileSize.
func (a *App) ReadFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", path, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory", path)
	}
	if info.Size() > maxFileSize {
		return "", fmt.Errorf("file %s is larger than %d bytes", path, maxFileSize)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return string(data), nil
}

// ReadFileB64 returns binary file content base64-encoded, for image previews.
// Refuses files larger than maxFileSize.
func (a *App) ReadFileB64(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", path, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory", path)
	}
	if info.Size() > maxFileSize {
		return "", fmt.Errorf("file %s is larger than %d bytes", path, maxFileSize)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

// SaveFile persists content via editor.Service (atomic write + dirty tracking).
func (a *App) SaveFile(path, content string) error {
	return a.buf.Save(path, content)
}

// validateProjectPath refuses paths outside any open project root, so the
// create APIs can't be used to scribble anywhere on disk.
func (a *App) validateProjectPath(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", path, err)
	}
	for _, p := range a.reg.List() {
		root, err := filepath.Abs(p.Root)
		if err != nil {
			continue
		}
		if abs == root || strings.HasPrefix(abs, root+string(os.PathSeparator)) {
			return nil
		}
	}
	return fmt.Errorf("path %s is not inside an open project", path)
}

// CreateFile creates an empty file at path, creating parent directories as
// needed. Fails when the file already exists.
func (a *App) CreateFile(path string) error {
	if err := a.validateProjectPath(path); err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("file already exists: %s", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create dirs for %s: %w", path, err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	return f.Close()
}

// CreateFolder creates the directory at path (including parents). Fails when
// the directory already exists.
func (a *App) CreateFolder(path string) error {
	if err := a.validateProjectPath(path); err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("folder already exists: %s", path)
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return fmt.Errorf("create folder %s: %w", path, err)
	}
	return nil
}

// DeletePath removes the file or directory tree at path. The project root
// itself is never deletable.
func (a *App) DeletePath(path string) error {
	if err := a.validateProjectPath(path); err != nil {
		return err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", path, err)
	}
	for _, p := range a.reg.List() {
		if root, rerr := filepath.Abs(p.Root); rerr == nil && abs == root {
			return fmt.Errorf("cannot delete project root: %s", path)
		}
	}
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("delete %s: %w", path, err)
	}
	return nil
}

// RevealInFinder shows path in the OS file manager (Finder on macOS),
// selecting it when possible.
func (a *App) RevealInFinder(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", path, err)
	}
	if _, err := os.Stat(abs); err != nil {
		return fmt.Errorf("stat %s: %w", abs, err)
	}
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", "-R", abs).Start()
	case "windows":
		return exec.Command("explorer", "/select,", abs).Start()
	default:
		return exec.Command("xdg-open", filepath.Dir(abs)).Start()
	}
}

// EnsureWorkspaceWindow opens the workspace window if none exists (or shows
// the existing one). Called by the frontend after opening a project.
func (a *App) EnsureWorkspaceWindow() {
	adapter.EnsureWorkspaceWindow()
}

// CloseWelcome closes the welcome window after a project has been opened.
func (a *App) CloseWelcome() {
	adapter.CloseWelcomeWindow()
}

// searchRun is a handle for one in-flight search so an finishing call can
// only remove its own cancellation entry (funcs are not comparable).
type searchRun struct {
	cancel context.CancelFunc
}

// searchTimeout bounds a single text search; a wedged walk cannot pin the
// RPC forever.
const searchTimeout = 20 * time.Second

// SearchText runs a project-wide text search and returns the full result
// synchronously (MVP: no streaming; a new call for the same project cancels
// the previous one, and the frontend drops superseded responses by seq).
func (a *App) SearchText(
	projectID, term string,
	regex, caseSensitive bool,
	include, exclude string,
) (search.Result, error) {
	root, err := a.resolveRoot(projectID)
	if err != nil {
		return search.Result{}, err
	}
	a.searchMu.Lock()
	if run := a.searchCancels[projectID]; run != nil {
		run.cancel()
	}
	ctx, cancel := context.WithTimeout(context.Background(), searchTimeout)
	run := &searchRun{cancel: cancel}
	a.searchCancels[projectID] = run
	a.searchMu.Unlock()
	defer func() {
		a.searchMu.Lock()
		if a.searchCancels[projectID] == run {
			delete(a.searchCancels, projectID)
		}
		cancel()
		a.searchMu.Unlock()
	}()
	return search.Search(root, search.Query{
		Term:          term,
		Regex:         regex,
		CaseSensitive: caseSensitive,
		Include:       include,
		Exclude:       exclude,
	}, ctx)
}

// resolveRoot returns the absolute project root for the given project ID.
func (a *App) resolveRoot(projectID string) (string, error) {
	p, err := a.reg.Get(projectID)
	if err != nil {
		return "", err
	}
	return p.Root, nil
}

// gitRootGitEngine fails fast when the project is unknown, then opens the
// (stateless, cheap) git Engine per call.
func (a *App) gitEngine(projectID string) (*git.Engine, error) {
	root, err := a.resolveRoot(projectID)
	if err != nil {
		return nil, err
	}
	return git.New(root)
}

// GitStatus returns Staged/Unstaged/Untracked changes plus the current branch.
func (a *App) GitStatus(projectID string) (git.Status, error) {
	e, err := a.gitEngine(projectID)
	if err != nil {
		return git.Status{}, err
	}
	return e.Status()
}

// GitStage adds files to the index ("add"; deleted files are recorded via "rm").
func (a *App) GitStage(projectID string, paths []string) error {
	e, err := a.gitEngine(projectID)
	if err != nil {
		return err
	}
	return e.Stage(paths)
}

// GitUnstage resets paths back out of the index.
func (a *App) GitUnstage(projectID string, paths []string) error {
	e, err := a.gitEngine(projectID)
	if err != nil {
		return err
	}
	return e.Unstage(paths)
}

// GitStageHunks stages selected hunks (0-based indices into the file's
// unified diff) via the git binary. Requires `git` on PATH; otherwise it
// fails fast with a clear availability error.
func (a *App) GitStageHunks(projectID, path string, hunkIdx []int) error {
	root, err := a.resolveRoot(projectID)
	if err != nil {
		return err
	}
	if err := git.StageHunks(root, path, hunkIdx); err != nil {
		return err
	}
	go a.emitGitStatus(projectID)
	return nil
}

// GitUnstageHunks reverts selected staged hunks (0-based indices into the
// file's staged diff) out of the index via the git binary.
func (a *App) GitUnstageHunks(projectID, path string, hunkIdx []int) error {
	root, err := a.resolveRoot(projectID)
	if err != nil {
		return err
	}
	if err := git.UnstageHunks(root, path, hunkIdx); err != nil {
		return err
	}
	go a.emitGitStatus(projectID)
	return nil
}

// GitCommit commits staged changes with the fixed "codesaber <codesaber@local>" identity.
func (a *App) GitCommit(projectID, message string) error {
	e, err := a.gitEngine(projectID)
	if err != nil {
		return err
	}
	return e.Commit(message, "codesaber <codesaber@local>")
}

// GitBranches lists local branches (refs/heads, sorted, short names).
func (a *App) GitBranches(projectID string) ([]string, error) {
	e, err := a.gitEngine(projectID)
	if err != nil {
		return nil, err
	}
	return e.Branches()
}

// GitCreateBranch creates a branch pointing at the current HEAD.
func (a *App) GitCreateBranch(projectID, name string) error {
	e, err := a.gitEngine(projectID)
	if err != nil {
		return err
	}
	return e.CreateBranch(name)
}

// GitCheckout switches worktree to the given local branch.
func (a *App) GitCheckout(projectID, name string) error {
	e, err := a.gitEngine(projectID)
	if err != nil {
		return err
	}
	return e.CheckoutBranch(name)
}

// GitDiff returns the diff for path against HEAD (staged) or the index (unstaged).
func (a *App) GitDiff(projectID, path string, staged bool) (git.DiffPatch, error) {
	e, err := a.gitEngine(projectID)
	if err != nil {
		return git.DiffPatch{}, err
	}
	if staged {
		return e.DiffStaged(path)
	}
	return e.DiffUnstaged(path)
}

// GitLog returns the n most recent commit entries walking first-parent from HEAD.
func (a *App) GitLog(projectID string, n int) ([]git.LogEntry, error) {
	e, err := a.gitEngine(projectID)
	if err != nil {
		return nil, err
	}
	return e.Log(n)
}

// GitAheadBehind reports the ahead/behind counts versus the remote-tracking
// branch of HEAD. Remote is always "origin" (MVP); a missing upstream or
// absent tracking ref yields Remote "" with zeros instead of an error.
func (a *App) GitAheadBehind(projectID string) (GitSyncInfo, error) {
	info := GitSyncInfo{Remote: "origin"}
	e, err := a.gitEngine(projectID)
	if err != nil {
		return GitSyncInfo{Remote: ""}, err
	}
	ahead, behind, aerr := e.AheadBehind(info.Remote)
	if aerr != nil {
		if errors.Is(aerr, git.ErrNoUpstream) {
			info.Remote = ""
			return info, nil
		}
		return GitSyncInfo{Remote: ""}, aerr
	}
	info.Ahead, info.Behind = ahead, behind
	return info, nil
}

// GitFetch fetches all configured remotes (30s timeout, no auth MVP).
func (a *App) GitFetch(projectID string) error {
	e, err := a.gitEngine(projectID)
	if err != nil {
		return err
	}
	return e.Fetch()
}

// GitPush pushes HEAD to the first remote with its default refspec (MVP: no auth).
func (a *App) GitPush(projectID string) error {
	e, err := a.gitEngine(projectID)
	if err != nil {
		return err
	}
	return e.Push()
}

// GitStats computes on-demand per-file [additions, deletions]; capped at
// maxStatsFiles paths per call to keep it cheap.
func (a *App) GitStats(projectID string, paths []string, staged bool) (map[string][2]int, error) {
	e, err := a.gitEngine(projectID)
	if err != nil {
		return nil, err
	}
	if len(paths) <= maxStatsFiles {
		return e.Stats(paths, staged)
	}
	return e.Stats(paths[:maxStatsFiles], staged)
}

// emitGitStatus is the throttled entry point: it emits immediately when the
// throttle window has elapsed; otherwise it arms (at most once) a trailing
// timer for the remaining wait so the final state of a burst is never lost.
// No-op for unknown projects.
func (a *App) emitGitStatus(projectID string) {
	a.mu.Lock()
	if a.lastGitEmit == nil {
		a.lastGitEmit = map[string]time.Time{}
	}
	if a.pendingEmit == nil {
		a.pendingEmit = map[string]bool{}
	}
	if time.Since(a.lastGitEmit[projectID]) < gitStatusThrottle {
		if a.pendingEmit[projectID] {
			// a trailing emit is already scheduled for this project
			a.mu.Unlock()
			return
		}
		a.pendingEmit[projectID] = true
		wait := gitStatusThrottle - time.Since(a.lastGitEmit[projectID])
		a.mu.Unlock()
		time.AfterFunc(wait, func() { a.emitGitStatusNow(projectID) })
		return
	}
	a.lastGitEmit[projectID] = time.Now()
	a.mu.Unlock()

	a.emitGitStatusNow(projectID)
}

// emitGitStatusNow performs the (unthrottled) status computation and emission;
// it is called inline on the leading edge and from the trailing timer (which
// passes the bypass implicitly by not going through emitGitStatus again).
func (a *App) emitGitStatusNow(projectID string) {
	a.mu.Lock()
	if a.lastGitEmit == nil {
		a.lastGitEmit = map[string]time.Time{}
	}
	if a.pendingEmit == nil {
		a.pendingEmit = map[string]bool{}
	}
	if a.pendingEmit[projectID] {
		a.lastGitEmit[projectID] = time.Now()
	}
	delete(a.pendingEmit, projectID)
	a.mu.Unlock()

	a.emitGitStatusBody(projectID)
}

func (a *App) emitGitStatusBody(projectID string) {
	root, err := a.resolveRoot(projectID)
	if err != nil {
		return
	}
	e, gerr := git.New(root)
	if gerr != nil {
		a.sink.Emit(EventGitError, map[string]string{"projectId": projectID, "message": gerr.Error()})
		return
	}
	st, serr := e.Status()
	if serr != nil {
		a.sink.Emit(EventGitError, map[string]string{"projectId": projectID, "message": serr.Error()})
		return
	}
	a.sink.Emit(EventGitStatus, map[string]any{"projectId": projectID, "status": st})
}

// onLSPDiags is the Manager-level diagnostics callback: it converts the
// server URI back to a workspace path and funnels the delivery through the
// per-project throttle.
func (a *App) onLSPDiags(projectID, uri string, diags []lsp.Diagnostic) {
	path, err := lsp.FromURI(uri)
	if err != nil {
		path = uri
	}
	a.emitLSPDiag(projectID, path, diags)
}

// emitLSPDiag applies the leading/trailing throttle (windows per project)
// before emitting EventLSPDiag, so diagnostic bursts cost one event each.
// While a trailing emit is armed, each new burst overwrites the stored
// path/diagnostics; the trailing timer therefore emits the LATEST state
// (post-resolution), never the arm-time snapshot.
func (a *App) emitLSPDiag(projectID, path string, diags []lsp.Diagnostic) {
	a.lspMu.Lock()
	if time.Since(a.lastDiagEmit[projectID]) < lspDiagThrottle {
		if !a.lspPendingDiag[projectID] {
			a.lspPendingDiag[projectID] = true
			wait := lspDiagThrottle - time.Since(a.lastDiagEmit[projectID])
			a.lspPendingDiagPath[projectID] = path
			a.lspPendingDiagLatest[projectID] = diags
			a.lspMu.Unlock()
			time.AfterFunc(wait, func() { a.emitLSPDiagTrailing(projectID) })
			return
		}
		a.lspPendingDiagPath[projectID] = path
		a.lspPendingDiagLatest[projectID] = diags
		a.lspMu.Unlock()
		return
	}
	a.lastDiagEmit[projectID] = time.Now()
	a.lspMu.Unlock()

	a.sink.Emit(EventLSPDiag, map[string]any{
		"projectId": projectID, "path": path, "diagnostics": diags,
	})
}

// emitLSPDiagTrailing fires the armed trailing emit with the latest stored
// path/diagnostics for the project.
func (a *App) emitLSPDiagTrailing(projectID string) {
	a.lspMu.Lock()
	if !a.lspPendingDiag[projectID] {
		a.lspMu.Unlock()
		return
	}
	a.lspPendingDiag[projectID] = false
	a.lastDiagEmit[projectID] = time.Now()
	path := a.lspPendingDiagPath[projectID]
	diags := a.lspPendingDiagLatest[projectID]
	delete(a.lspPendingDiagPath, projectID)
	delete(a.lspPendingDiagLatest, projectID)
	a.lspMu.Unlock()

	a.sink.Emit(EventLSPDiag, map[string]any{
		"projectId": projectID, "path": path, "diagnostics": diags,
	})
}

// emitLSPState reports running/reason for the project's gopls lifecycle,
// but only when the (running, reason) pair actually changed since the last
// emission for the project — repeated didOpen flows must not spam lsp.state.
func (a *App) emitLSPState(projectID string, running bool, reason string) {
	a.lspMu.Lock()
	prev, seen := a.lastLSPState[projectID]
	next := lspState{running: running, reason: reason}
	if seen && prev == next {
		a.lspMu.Unlock()
		return
	}
	a.lastLSPState[projectID] = next
	a.lspMu.Unlock()

	payload := map[string]any{"projectId": projectID, "running": running}
	if reason != "" {
		payload["reason"] = reason
	}
	a.sink.Emit(EventLSPState, payload)
}

// ensureLSP resolves the project root, verifies it is a Go module, and
// ensures the gopls client is running; success/failure is reported via
// EventLSPState.
func (a *App) ensureLSP(projectID string) (*lsp.Client, error) {
	root, err := a.resolveRoot(projectID)
	if err != nil {
		a.emitLSPState(projectID, false, err.Error())
		return nil, err
	}
	if _, statErr := os.Stat(filepath.Join(root, "go.mod")); statErr != nil {
		err := fmt.Errorf("lsp: project root is not a Go module (missing go.mod)")
		a.emitLSPState(projectID, false, err.Error())
		return nil, err
	}
	c, err := a.lspMgr.Ensure(projectID, lsp.ToURI(root))
	if err != nil {
		a.emitLSPState(projectID, false, err.Error())
		return nil, err
	}
	a.emitLSPState(projectID, true, "")
	return c, nil
}

// setLSPVersion records the frontend's doc version for projectID/path.
func (a *App) setLSPVersion(projectID, path string, version int) {
	a.lspMu.Lock()
	if a.lspVersions[projectID] == nil {
		a.lspVersions[projectID] = map[string]int{}
	}
	a.lspVersions[projectID][path] = version
	a.lspMu.Unlock()
}

// LSPEnsure explicitly starts the project's gopls (state reporting happens
// inside ensureLSP). Guests call it on opening the first .go tab; startup is
// otherwise lazy via LSPDidOpen.
func (a *App) LSPEnsure(projectID string) error {
	_, err := a.ensureLSP(projectID)
	return err
}

// LSPDidOpen notifies gopls of an opened document (full text sync).
func (a *App) LSPDidOpen(projectID, path string, version int, content string) error {
	c, err := a.ensureLSP(projectID)
	if err != nil {
		return err
	}
	a.setLSPVersion(projectID, path, version)
	return c.DidOpen(lsp.ToURI(path), int32(version), content)
}

// LSPDidChange sends the full document content at the new version.
func (a *App) LSPDidChange(projectID, path string, version int, content string) error {
	c := a.lspMgr.Client(projectID)
	if c == nil {
		return fmt.Errorf("lsp: no running server for project %s", projectID)
	}
	a.setLSPVersion(projectID, path, version)
	return c.DidChange(lsp.ToURI(path), int32(version), content)
}

// LSPDidSave signals a save ("includeText" style with the saved content
// handled by the caller passing text; empty text means notify-only).
func (a *App) LSPDidSave(projectID, path, content string) error {
	if c := a.lspMgr.Client(projectID); c != nil {
		return c.DidSave(lsp.ToURI(path), content)
	}
	return nil
}

// LSPDidClose tells gopls the document is no longer open; no-op when the
// server is not running (never spawns one for a close).
func (a *App) LSPDidClose(projectID, path string) error {
	if c := a.lspMgr.Client(projectID); c != nil {
		return c.DidClose(lsp.ToURI(path))
	}
	return nil
}

// LSPDefinition resolves Go-to-definition at a position; result URIs are
// translated back into absolute filesystem paths for the frontend.
func (a *App) LSPDefinition(projectID, path string, line, col int32) ([]lsp.Location, error) {
	c := a.lspMgr.Client(projectID)
	if c == nil {
		return nil, fmt.Errorf("lsp: no running server for project %s", projectID)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	locs, err := c.Definition(ctx, lsp.ToURI(path), line, col)
	if err != nil {
		return nil, err
	}
	for i := range locs {
		if p, perr := lsp.FromURI(locs[i].URI); perr == nil {
			locs[i].URI = p
		}
	}
	return locs, nil
}

// LSPHover resolves hover markdown-ish content at a position (nil = none).
func (a *App) LSPHover(projectID, path string, line, col int32) (*lsp.Hover, error) {
	c := a.lspMgr.Client(projectID)
	if c == nil {
		return nil, fmt.Errorf("lsp: no running server for project %s", projectID)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return c.Hover(ctx, lsp.ToURI(path), line, col)
}

// LSPSymbols returns hierarchical document symbols for the file.
func (a *App) LSPSymbols(projectID, path string) ([]lsp.DocumentSymbol, error) {
	c := a.lspMgr.Client(projectID)
	if c == nil {
		return nil, fmt.Errorf("lsp: no running server for project %s", projectID)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return c.DocumentSymbols(ctx, lsp.ToURI(path))
}

// TermStart spawns a shell session inside a PTY for the project and returns
// its termID ("projectId|suffix"). shell is the binary to run ("" defaults
// to PowerShell on Windows, /bin/sh on Unix); cwd is the project root.
func (a *App) TermStart(projectID, shell, suffix string) (string, error) {
	root, err := a.resolveRoot(projectID)
	if err != nil {
		return "", err
	}
	termID := projectID + "|" + suffix
	sess, err := terminal.New(terminal.SessionOpts{
		ID:    termID,
		Cwd:   root,
		Shell: shell,
	})
	if err != nil {
		return "", err
	}
	a.termMu.Lock()
	a.sessions[termID] = sess
	a.projectIndex[projectID] = append(a.projectIndex[projectID], termID)
	a.termMu.Unlock()

	go a.pumpTerm(projectID, termID, sess)
	return termID, nil
}

// pumpTerm forwards the session's event stream to the sink. Data frames go
// through a small bounded queue with drop-on-full so a slow UI never blocks
// the session reader; dropped frames are counted. The single Exit event is
// always delivered (it is never dropped) and releases the session entry.
func (a *App) pumpTerm(projectID, termID string, sess *terminal.Session) {
	queue := make(chan terminal.Event, 256)
	var dropped int

	go func() {
		for ev := range sess.Data() {
			select {
			case queue <- ev:
			default:
				if !ev.Exit {
					dropped++
				}
			}
		}
	}()

	for ev := range queue {
		if ev.Exit {
			a.releaseTerm(projectID, termID)
			a.sink.Emit(EventTermExit, map[string]any{
				"projectId": projectID,
				"termId":    termID,
				"code":      ev.Code,
			})
			return
		}
		a.sink.Emit(EventTermData, map[string]any{
			"projectId": projectID,
			"termId":    termID,
			"data":      ev.Data,
		})
	}
	_ = dropped
}

// stopProjectTerms stops and releases every terminal session opened for a
// project (called when the project is removed).
func (a *App) stopProjectTerms(projectID string) {
	a.termMu.Lock()
	ids := append([]string(nil), a.projectIndex[projectID]...)
	delete(a.projectIndex, projectID)
	a.termMu.Unlock()
	for _, termID := range ids {
		a.releaseTerm(projectID, termID)
	}
}

// releaseTerm removes the session entry and closes it (idempotent).
func (a *App) releaseTerm(projectID, termID string) {
	a.termMu.Lock()
	sess, ok := a.sessions[termID]
	delete(a.sessions, termID)
	idx := a.projectIndex[projectID]
	for i, id := range idx {
		if id == termID {
			a.projectIndex[projectID] = append(idx[:i], idx[i+1:]...)
			break
		}
	}
	a.termMu.Unlock()
	if ok {
		_ = sess.Close()
	}
}

// TermInput writes raw bytes to the terminal's stdin.
func (a *App) TermInput(termID string, data []byte) error {
	a.termMu.Lock()
	sess, ok := a.sessions[termID]
	a.termMu.Unlock()
	if !ok {
		return fmt.Errorf("terminal: unknown session %s", termID)
	}
	return sess.Input(data)
}

// TermResize resizes the terminal's PTY window.
func (a *App) TermResize(termID string, rows, cols int) error {
	a.termMu.Lock()
	sess, ok := a.sessions[termID]
	a.termMu.Unlock()
	if !ok {
		return fmt.Errorf("terminal: unknown session %s", termID)
	}
	return sess.Resize(rows, cols)
}

// TermStop terminates and releases the terminal session.
func (a *App) TermStop(termID string) error {
	a.termMu.Lock()
	_, ok := a.sessions[termID]
	a.termMu.Unlock()
	if !ok {
		return fmt.Errorf("terminal: unknown session %s", termID)
	}
	a.releaseTerm(sidProject(termID), termID)
	return nil
}

// sidProject splits termID into its projectID part.
func sidProject(termID string) string {
	if i := strings.IndexByte(termID, '|'); i >= 0 {
		return termID[:i]
	}
	return termID
}

// SettingsGet returns the persisted global settings (defaults when no file
// exists yet).
func (a *App) SettingsGet() settings.Model {
	return a.settings.Load()
}

// SettingsPut sanitizes and persists the settings document. Invalid values
// are corrected (font size clamped, garbage accent color dropped) or kept
// with a warning (unknown shell path) — Put never fails on user input, only
// on I/O errors. MVP note: no settings.updated broadcast; a single workspace
// window is assumed, so cross-window sync is out of scope for now.
func (a *App) SettingsPut(m settings.Model) error {
	return a.settings.Save(settings.Sanitize(m, nil))
}
