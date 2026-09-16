package git

import "testing"

func TestStatusHeadChangesOnSameBranchCommit(t *testing.T) {
	dir := initRepo(t)
	engine := newEngine(t, dir)
	before, err := engine.Status()
	if err != nil {
		t.Fatal(err)
	}
	commitFile(t, dir, "second.txt", "second")
	after, err := engine.Status()
	if err != nil {
		t.Fatal(err)
	}
	if before.Head == "" || after.Head == "" || before.Head == after.Head || before.Branch != after.Branch {
		t.Fatalf("HEAD must change independently of branch: before=%+v after=%+v", before, after)
	}
}
