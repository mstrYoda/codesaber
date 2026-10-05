package git

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	git2 "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		// Windows can briefly retain deleted object entries. Retry only this
		// test-owned directory before TempDir's final cleanup, with a hard bound.
		t.Cleanup(func() {
			deadline := time.Now().Add(2 * time.Second)
			for {
				err := os.RemoveAll(dir)
				if err == nil {
					return
				}
				if time.Now().After(deadline) {
					t.Errorf("cleanup fixture: %v", err)
					return
				}
				time.Sleep(20 * time.Millisecond)
			}
		})
	}
	r, err := git2.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	w, _ := r.Worktree()
	os.WriteFile(filepath.Join(dir, "init.txt"), []byte("one"), 0o644)
	w.Add("init.txt")
	_, err = w.Commit("init", &git2.CommitOptions{
		Author: &object.Signature{Name: "t", Email: "t@t"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestStatusTracksStagedUnstagedUntracked(t *testing.T) {
	dir := initRepo(t)
	os.WriteFile(filepath.Join(dir, "modified.txt"), []byte("mod"), 0o644)
	os.WriteFile(filepath.Join(dir, "init.txt"), []byte("one-t"), 0o644)

	e, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	st, err := e.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Branch == "" {
		t.Fatal("branch empty")
	}
	if len(st.Untracked) != 1 || st.Untracked[0].Path != "modified.txt" {
		t.Fatalf("untracked: %+v", st.Untracked)
	}
	if len(st.Unstaged) != 1 || st.Unstaged[0].Path != "init.txt" || st.Unstaged[0].Status != ChangeModified {
		t.Fatalf("unstaged: %+v", st.Unstaged)
	}
	if len(st.Staged) != 0 {
		t.Fatalf("staged should be empty: %+v", st.Staged)
	}

	f, err := os.OpenFile(filepath.Join(dir, "init.txt"), os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("changed")
	f.Close()

	if err := e.Stage([]string{"init.txt", "modified.txt"}); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	st, _ = e.Status()
	if len(st.Staged) != 2 {
		t.Fatalf("staged after stage: %+v", st.Staged)
	}
	if err := e.Unstage([]string{"modified.txt"}); err != nil {
		t.Fatalf("Unstage: %v", err)
	}
	st, _ = e.Status()
	if len(st.Staged) != 1 || st.Staged[0].Path != "init.txt" {
		t.Fatalf("staged after unstage: %+v", st.Staged)
	}
	if len(st.Unstaged) != 0 || st.Untracked[0].Path != "modified.txt" {
		t.Fatalf("post-unstage rest: %+v %+v", st.Unstaged, st.Untracked)
	}
}

func TestStatusSortsPaths(t *testing.T) {
	dir := initRepo(t)
	for _, name := range []string{"c.txt", "a.txt", "b.txt"} {
		os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644)
	}
	e, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	st, err := e.Status()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range st.Untracked {
		got = append(got, c.Path)
	}
	if len(got) != 3 || got[0] != "a.txt" || got[1] != "b.txt" || got[2] != "c.txt" {
		t.Fatalf("untracked order: %v", got)
	}

	if err := e.Stage([]string{"b.txt", "a.txt", "init.txt"}); err != nil {
		t.Fatal(err)
	}
	st, _ = e.Status()
	var staged []string
	for _, c := range st.Staged {
		staged = append(staged, c.Path)
	}
	if len(staged) != 2 || staged[0] != "a.txt" || staged[1] != "b.txt" {
		t.Fatalf("staged order: %v", staged)
	}
}

func TestCommitNothingStaged(t *testing.T) {
	dir := initRepo(t)
	e, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "init.txt"), []byte("dirty"), 0o644)
	if err := e.Commit("msg", "codesaber <codesaber@local>"); err == nil || err.Error() != "nothing staged" {
		t.Fatalf("Commit dirty worktree, empty index: err=%v", err)
	}
	if err := e.Stage([]string{"init.txt"}); err != nil {
		t.Fatal(err)
	}
	if err := e.Commit("staged commit", "codesaber <codesaber@local>"); err != nil {
		t.Fatalf("Commit with staged change: %v", err)
	}
	nodes, err := e.Log(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) < 2 || nodes[0].Message != "staged commit" {
		t.Fatalf("log: %+v", nodes)
	}
}

func TestCommit(t *testing.T) {
	dir := initRepo(t)
	e, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "init.txt"), []byte("three"), 0o644)
	if err := e.Stage([]string{"init.txt"}); err != nil {
		t.Fatal(err)
	}
	before := time.Now().Truncate(time.Second)
	if err := e.Commit("bump", "codesaber <codesaber@local>"); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	st, _ := e.Status()
	if len(st.Staged)+len(st.Unstaged)+len(st.Untracked) != 0 {
		t.Fatalf("should be clean: %+v", st)
	}
	nodes, _ := e.Log(5)
	if len(nodes) < 2 || nodes[0].Message != "bump" {
		t.Fatalf("log: %+v", nodes)
	}
	head, err := e.r.Head()
	if err != nil {
		t.Fatal(err)
	}
	commit, err := e.r.CommitObject(head.Hash())
	if err != nil {
		t.Fatal(err)
	}
	if commit.Author.When.Before(before) || commit.Author.When.After(time.Now()) {
		t.Fatalf("persisted commit timestamp is not current: %v", commit.Author.When)
	}
}

func TestBranchesAndCheckout(t *testing.T) {
	dir := initRepo(t)
	e, _ := New(dir)
	if err := e.CreateBranch("feature/x"); err != nil {
		t.Fatal(err)
	}
	if err := e.CheckoutBranch("feature/x"); err != nil {
		t.Fatal(err)
	}
	st, _ := e.Status()
	if st.Branch != "feature/x" {
		t.Fatalf("branch: %q", st.Branch)
	}
	b, err := e.r.Branch("feature/x")
	if err != nil {
		t.Fatal(err)
	}
	if b.Remote != "" || b.Merge.String() != "" {
		t.Fatalf("branch config should be bare: remote=%q merge=%q", b.Remote, b.Merge)
	}
}

func TestBranchAt(t *testing.T) {
	dir := initRepo(t)
	if got := BranchAt(dir); got != "master" && got != "main" {
		t.Fatalf("BranchAt: %q", got)
	}
	if got := BranchAt(t.TempDir()); got != "" {
		t.Fatalf("non-repo branch: %q", got)
	}
}
