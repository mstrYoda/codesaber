package git

type ChangeStatus byte

const (
	ChangeAdded     ChangeStatus = 'A'
	ChangeModified  ChangeStatus = 'M'
	ChangeDeleted   ChangeStatus = 'D'
	ChangeUntracked ChangeStatus = 'U'
)

type Change struct {
	Path   string       `json:"path"`
	Status ChangeStatus `json:"status"`
	Size   int64        `json:"size,omitempty"`
}

type Status struct {
	Branch    string   `json:"branch"`
	Head      string   `json:"head"`
	Staged    []Change `json:"staged"`
	Unstaged  []Change `json:"unstaged"`
	Untracked []Change `json:"untracked"`
}

type DiffHunk struct {
	Header    string   `json:"header"`
	Lines     []string `json:"lines"`
	Additions int      `json:"additions"`
	Deletions int      `json:"deletions"`
}

type DiffPatch struct {
	OldPath string     `json:"oldPath"`
	NewPath string     `json:"newPath"`
	Hunks   []DiffHunk `json:"hunks"`
}

type LogEntry struct {
	Hash    string `json:"hash"`
	Author  string `json:"author"`
	Message string `json:"message"`
	Time    string `json:"time"`
}
