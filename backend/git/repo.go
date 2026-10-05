package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	git2 "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/format/diff"
	"github.com/go-git/go-git/v5/plumbing/format/index"
	"github.com/go-git/go-git/v5/plumbing/object"
	utilsdiff "github.com/go-git/go-git/v5/utils/diff"
	"github.com/sergi/go-diff/diffmatchpatch"
)

type Engine struct {
	dir string
	r   *git2.Repository
	wt  *git2.Worktree
}

func New(dir string) (*Engine, error) {
	r, err := git2.PlainOpen(dir)
	if err != nil {
		return nil, err
	}
	wt, err := r.Worktree()
	if err != nil {
		return nil, err
	}
	return &Engine{dir: dir, r: r, wt: wt}, nil
}

// BranchAt returns the short branch name of the repository rooted at root,
// or "" when root is not a repo or has no commit yet.
func BranchAt(root string) string {
	r, err := git2.PlainOpen(root)
	if err != nil {
		return ""
	}
	head, err := r.Head()
	if err != nil {
		return ""
	}
	return head.Name().Short()
}

func mapStatus(c git2.StatusCode) ChangeStatus {
	switch c {
	case git2.Added:
		return ChangeAdded
	case git2.Deleted:
		return ChangeDeleted
	default:
		return ChangeModified
	}
}

func (e *Engine) Status() (Status, error) {
	st := Status{Branch: "(none)", Staged: []Change{}, Unstaged: []Change{}, Untracked: []Change{}}
	if head, err := e.r.Head(); err == nil {
		st.Branch = head.Name().Short()
		st.Head = head.Hash().String()
	}
	files, err := e.wt.Status()
	if err != nil {
		return st, err
	}
	for path, fs := range files {
		switch {
		case fs.Staging == git2.Untracked:
			st.Untracked = append(st.Untracked, Change{Path: path, Status: ChangeUntracked})
		case fs.Staging != git2.Unmodified:
			st.Staged = append(st.Staged, Change{Path: path, Status: mapStatus(fs.Staging)})
		}
		if fs.Worktree == git2.Modified || fs.Worktree == git2.Deleted {
			st.Unstaged = append(st.Unstaged, Change{Path: path, Status: mapStatus(fs.Worktree)})
		}
	}
	for _, group := range [][]Change{st.Staged, st.Unstaged, st.Untracked} {
		sort.Slice(group, func(i, j int) bool { return group[i].Path < group[j].Path })
	}
	e.untrackedSizes(&st)
	return st, nil
}

func (e *Engine) Stage(paths []string) error {
	for _, p := range paths {
		if _, err := os.Stat(e.wt.Filesystem.Join(e.dir, p)); err != nil {
			if _, err := e.wt.Remove(p); err != nil {
				return err
			}
			continue
		}
		if _, err := e.wt.Add(p); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) Unstage(paths []string) error {
	for _, p := range paths {
		if err := e.wt.Reset(&git2.ResetOptions{Files: []string{p}}); err != nil {
			return err
		}
	}
	return nil
}

func parseAuthor(author string) *object.Signature {
	name, email, _ := strings.Cut(author, "<")
	email = strings.TrimSuffix(strings.TrimSpace(email), ">")
	return &object.Signature{Name: strings.TrimSpace(name), Email: email, When: time.Now()}
}

func indexMatchesHead(e *Engine) (bool, error) {
	idx, err := e.r.Storer.Index()
	if err != nil {
		return false, err
	}
	indexFiles := map[string]plumbing.Hash{}
	for _, entry := range idx.Entries {
		if entry.Mode.IsFile() {
			indexFiles[entry.Name] = entry.Hash
		}
	}

	headFiles := map[string]plumbing.Hash{}
	head, err := e.r.Head()
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return len(indexFiles) == 0, nil
		}
		return false, err
	}
	commit, err := e.r.CommitObject(head.Hash())
	if err != nil {
		return false, err
	}
	tree, err := commit.Tree()
	if err != nil {
		return false, err
	}
	walker := object.NewTreeWalker(tree, true, nil)
	defer walker.Close()
	for {
		name, entry, werr := walker.Next()
		if werr != nil {
			if errors.Is(werr, io.EOF) {
				break
			}
			return false, werr
		}
		if entry.Mode.IsFile() {
			headFiles[name] = entry.Hash
		}
	}
	if len(headFiles) != len(indexFiles) {
		return false, nil
	}
	for path, h := range headFiles {
		if ih, ok := indexFiles[path]; !ok || ih != h {
			return false, nil
		}
	}
	return true, nil
}

func (e *Engine) Commit(msg, author string) error {
	same, err := indexMatchesHead(e)
	if err != nil {
		return err
	}
	if same {
		return fmt.Errorf("nothing staged")
	}
	_, err = e.wt.Commit(msg, &git2.CommitOptions{Author: parseAuthor(author)})
	return err
}

func (e *Engine) Log(n int) ([]LogEntry, error) {
	head, err := e.r.Head()
	if err != nil {
		return nil, err
	}
	entries := []LogEntry{}
	hash := head.Hash()
	for len(entries) < n {
		commit, err := e.r.CommitObject(hash)
		if err != nil {
			return nil, err
		}
		entries = append(entries, LogEntry{
			Hash:    hash.String(),
			Author:  commit.Author.Name,
			Message: commit.Message,
			Time:    commit.Author.When.Format("2006-01-02 15:04:05"),
		})
		if commit.NumParents() == 0 {
			break
		}
		hash = commit.ParentHashes[0]
	}
	return entries, nil
}

// Branches returns sorted short branch names (refs/heads).
func (e *Engine) Branches() ([]string, error) {
	iter, err := e.r.References()
	if err != nil {
		return nil, err
	}
	defer iter.Close()
	var out []string
	if err := iter.ForEach(func(ref *plumbing.Reference) error {
		if ref.Name().IsBranch() {
			out = append(out, ref.Name().Short())
		}
		return nil
	}); err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

func (e *Engine) CreateBranch(name string) error {
	head, err := e.r.Head()
	if err != nil {
		return err
	}
	ref := plumbing.NewBranchReferenceName(name)
	if err := e.r.CreateBranch(&config.Branch{Name: name}); err != nil {
		return err
	}
	return e.r.Storer.SetReference(plumbing.NewHashReference(ref, head.Hash()))
}

func (e *Engine) CheckoutBranch(name string) error {
	return e.wt.Checkout(&git2.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(name),
	})
}

func blobContent(e *Engine, h plumbing.Hash) ([]byte, error) {
	if h.IsZero() {
		return nil, nil
	}
	blob, err := e.r.BlobObject(h)
	if err != nil {
		return nil, err
	}
	r, err := blob.Reader()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

func indexEntryHash(e *Engine, path string) (plumbing.Hash, error) {
	idx, err := e.r.Storer.Index()
	if err != nil {
		return plumbing.ZeroHash, err
	}
	entry, err := idx.Entry(path)
	if err != nil {
		return plumbing.ZeroHash, err
	}
	return entry.Hash, nil
}

func headBlobHash(e *Engine, path string) (plumbing.Hash, error) {
	head, err := e.r.Head()
	if err != nil {
		return plumbing.ZeroHash, err
	}
	commit, err := e.r.CommitObject(head.Hash())
	if err != nil {
		return plumbing.ZeroHash, err
	}
	tree, err := commit.Tree()
	if err != nil {
		return plumbing.ZeroHash, err
	}
	file, err := tree.File(path)
	if err != nil {
		return plumbing.ZeroHash, err
	}
	return file.Blob.Hash, nil
}

func isNotExistErr(err error) bool {
	return errors.Is(err, plumbing.ErrObjectNotFound) ||
		errors.Is(err, object.ErrFileNotFound) ||
		errors.Is(err, plumbing.ErrReferenceNotFound)
}

func buildPatch(oldPath, newPath string, from, to []byte) (DiffPatch, error) {
	patch := DiffPatch{OldPath: oldPath, NewPath: newPath, Hunks: []DiffHunk{}}
	if bytes.Equal(from, to) {
		return patch, nil
	}
	ud := utilsdiff.Do(string(from), string(to))
	var sb strings.Builder
	if err := diff.NewUnifiedEncoder(&sbWriter{&sb}, 3).Encode(&chunkPatch{
		fromPath: oldPath,
		toPath:   newPath,
		chunks:   ud,
	}); err != nil {
		return DiffPatch{}, err
	}
	return parseUnified(patch, sb.String()), nil
}

type sbWriter struct{ sb *strings.Builder }

func (w *sbWriter) Write(p []byte) (int, error) { return w.sb.Write(p) }

type chunkPatch struct {
	fromPath string
	toPath   string
	chunks   []diffmatchpatch.Diff
}

func (p *chunkPatch) FilePatches() []diff.FilePatch {
	return []diff.FilePatch{p}
}

func (p *chunkPatch) Message() string { return "" }

func (p *chunkPatch) IsBinary() bool { return false }

func (p *chunkPatch) Files() (diff.File, diff.File) {
	return &chunkFile{path: p.fromPath}, &chunkFile{path: p.toPath}
}

func (p *chunkPatch) Chunks() []diff.Chunk {
	out := make([]diff.Chunk, 0, len(p.chunks))
	for _, c := range p.chunks {
		var op diff.Operation
		switch c.Type {
		case diffmatchpatch.DiffInsert:
			op = diff.Add
		case diffmatchpatch.DiffDelete:
			op = diff.Delete
		}
		out = append(out, chunkChunk{content: c.Text, op: op})
	}
	return out
}

type chunkFile struct {
	path string
}

func (f *chunkFile) Hash() plumbing.Hash     { return plumbing.ZeroHash }
func (f *chunkFile) Mode() filemode.FileMode { return filemode.Regular }
func (f *chunkFile) Path() string            { return f.path }

type chunkChunk struct {
	content string
	op      diff.Operation
}

func (c chunkChunk) Content() string      { return c.content }
func (c chunkChunk) Type() diff.Operation { return c.op }

func parseUnified(p DiffPatch, text string) DiffPatch {
	var cur *DiffHunk
	for _, line := range strings.Split(text, "\n") {
		switch {
		case strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ "):
			continue
		case strings.HasPrefix(line, "@@"):
			p.Hunks = append(p.Hunks, DiffHunk{Header: line, Lines: []string{}})
			cur = &p.Hunks[len(p.Hunks)-1]
		case cur != nil:
			if strings.HasPrefix(line, "+") {
				cur.Additions++
			} else if strings.HasPrefix(line, "-") {
				cur.Deletions++
			}
			cur.Lines = append(cur.Lines, line)
		}
	}
	return p
}

func (e *Engine) DiffStaged(path string) (DiffPatch, error) {
	headHash, err := headBlobHash(e, path)
	if err != nil && !isNotExistErr(err) {
		return DiffPatch{}, err
	}
	idxHash, err := indexEntryHash(e, path)
	if err != nil && !errors.Is(err, index.ErrEntryNotFound) {
		return DiffPatch{}, err
	}
	if headHash.IsZero() && idxHash.IsZero() {
		return DiffPatch{OldPath: path, NewPath: path, Hunks: []DiffHunk{}}, nil
	}
	from, err := blobContent(e, headHash)
	if err != nil {
		return DiffPatch{}, err
	}
	to, err := blobContent(e, idxHash)
	if err != nil {
		return DiffPatch{}, err
	}
	return buildPatch(path, path, from, to)
}

func (e *Engine) DiffUnstaged(path string) (DiffPatch, error) {
	idxHash, err := indexEntryHash(e, path)
	if err != nil && !errors.Is(err, index.ErrEntryNotFound) {
		return DiffPatch{}, err
	}
	if idxHash.IsZero() {
		data, readErr := os.ReadFile(e.wt.Filesystem.Join(e.dir, path))
		if readErr != nil {
			if os.IsNotExist(readErr) {
				return DiffPatch{OldPath: path, NewPath: path, Hunks: []DiffHunk{}}, nil
			}
			return DiffPatch{}, readErr
		}
		return buildPatch(path, path, nil, data)
	}
	data, err := os.ReadFile(e.wt.Filesystem.Join(e.dir, path))
	if err != nil {
		if !os.IsNotExist(err) {
			return DiffPatch{}, err
		}
		data = nil
	}
	from, err := blobContent(e, idxHash)
	if err != nil {
		return DiffPatch{}, err
	}
	return buildPatch(path, path, from, data)
}

// ErrNoUpstream reports that no remote-tracking ref exists for HEAD's branch.
var ErrNoUpstream = errors.New("no upstream for current branch")

const revWalkCap = 5000

func (e *Engine) untrackedSizes(st *Status) {
	for i := range st.Untracked {
		c := &st.Untracked[i]
		if fi, err := os.Stat(e.wt.Filesystem.Join(e.dir, c.Path)); err == nil {
			c.Size = fi.Size()
		} else {
			c.Size = -1
		}
	}
}

// countPatchLineStats counts + and - line totals from the raw diff chunks.
// Stats computes per-file [additions, deletions] for the given paths, against
// HEAD (staged) or the index (unstaged). Untracked-style missing sides count as
// whole-file additions/deletions.
func (e *Engine) Stats(paths []string, staged bool) (map[string][2]int, error) {
	out := make(map[string][2]int, len(paths))
	for _, p := range paths {
		var patch DiffPatch
		var err error
		if staged {
			patch, err = e.DiffStaged(p)
		} else {
			patch, err = e.DiffUnstaged(p)
		}
		if err != nil {
			return nil, err
		}
		var add, del int
		for _, h := range patch.Hunks {
			add += h.Additions
			del += h.Deletions
		}
		out[p] = [2]int{add, del}
	}
	return out, nil
}

// remoteHash resolves refs/remotes/<remote>/<branch> for the current HEAD
// branch; ErrNoUpstream when absent.
func (e *Engine) remoteHash(remote string) (plumbing.Hash, error) {
	head, err := e.r.Head()
	if err != nil {
		return plumbing.ZeroHash, err
	}
	branch := head.Name().Short()
	if head.Name().IsTag() || branch == "" {
		return plumbing.ZeroHash, ErrNoUpstream
	}
	ref, err := e.r.Reference(plumbing.NewRemoteReferenceName(remote, branch), false)
	if err != nil {
		return plumbing.ZeroHash, ErrNoUpstream
	}
	return ref.Hash(), nil
}

// ancestors returns the set of commit hashes reachable from start, capped.
func (e *Engine) ancestors(start plumbing.Hash) map[plumbing.Hash]bool {
	seen := map[plumbing.Hash]bool{}
	queue := []plumbing.Hash{start}
	for len(queue) > 0 && len(seen) < revWalkCap {
		h := queue[0]
		queue = queue[1:]
		if h.IsZero() || seen[h] {
			continue
		}
		commit, err := e.r.CommitObject(h)
		if err != nil {
			continue
		}
		seen[h] = true
		queue = append(queue, commit.ParentHashes...)
	}
	return seen
}

// AheadBehind counts commits reachable from HEAD but not from the remote's
// tracking ref (ahead) and the reverse (behind), capped at revWalkCap each.
func (e *Engine) AheadBehind(remote string) (ahead, behind int, err error) {
	head, err := e.r.Head()
	if err != nil {
		return 0, 0, err
	}
	base, err := e.remoteHash(remote)
	if err != nil {
		return 0, 0, err
	}
	if base == head.Hash() {
		return 0, 0, nil
	}
	mine := e.ancestors(head.Hash())
	theirs := e.ancestors(base)
	for h := range mine {
		if !theirs[h] {
			ahead++
		}
	}
	for h := range theirs {
		if !mine[h] {
			behind++
		}
	}
	return ahead, behind, nil
}

// Fetch fetches all configured remotes with a 30s context, plain (untracked
// credentials MVP).
func (e *Engine) Fetch() error {
	remotes, err := e.r.Remotes()
	if err != nil {
		return err
	}
	if len(remotes) == 0 {
		return fmt.Errorf("no git remote configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var errs []string
	for _, rem := range remotes {
		if err := rem.FetchContext(ctx, &git2.FetchOptions{}); err != nil {
			if errors.Is(err, git2.NoErrAlreadyUpToDate) {
				continue
			}
			errs = append(errs, rem.Config().Name+": "+err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("fetch failed: %s", strings.Join(errs, "; "))
	}
	return nil
}

// Push pushes HEAD to the first remote tracking-style refspec (MVP: no auth).
func (e *Engine) Push() error {
	remotes, err := e.r.Remotes()
	if err != nil {
		return err
	}
	if len(remotes) == 0 {
		return fmt.Errorf("no git remote configured")
	}
	head, err := e.r.Head()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rem := remotes[0]
	err = rem.PushContext(ctx, &git2.PushOptions{
		RefSpecs: []config.RefSpec{config.RefSpec(head.Name() + ":" + head.Name())},
	})
	if errors.Is(err, git2.NoErrAlreadyUpToDate) {
		return nil
	}
	return err
}
