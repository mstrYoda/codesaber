package project

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Branch returns the git branch for root. Replaced by the git engine in Phase 2.
var Branch = func(root string) string { return "(unknown)" }

type Registry struct {
	mu       sync.Mutex
	projects map[string]*Project
	notify   func()
}

func NewRegistry(notify func()) *Registry {
	return &Registry{
		projects: make(map[string]*Project),
		notify:   notify,
	}
}

func (r *Registry) Add(root string) (*Project, error) {
	return r.AddWithID(root, "")
}

// AddWithID reuses a persisted project identity so transcripts remain attached
// when the project is reopened. Empty IDs are allocated for new projects.
func (r *Registry) AddWithID(root, id string) (*Project, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", abs, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", abs)
	}

	r.mu.Lock()
	if id == "" {
		id = uuid.NewString()
	}
	if _, exists := r.projects[id]; exists {
		r.mu.Unlock()
		return nil, fmt.Errorf("project already open: %s", id)
	}
	for _, p := range r.projects {
		if p.Root == abs {
			r.mu.Unlock()
			return nil, fmt.Errorf("project already open: %s", abs)
		}
	}
	var notifyFn func()
	p := &Project{
		ID:       id,
		Name:     filepath.Base(abs),
		Root:     abs,
		Branch:   Branch(abs),
		LastUsed: time.Now(),
		EngineOK: true,
	}
	r.projects[p.ID] = p
	notifyFn = r.notify
	r.mu.Unlock()
	notifyFn()
	return p, nil
}

// SetEngineOK updates the engine-health flag for an already-registered
// project. The facade calls it after watcher setup succeeds or fails, before
// emitting "project.added", so the payload reflects real engine status.
func (r *Registry) SetEngineOK(id string, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p, exists := r.projects[id]; exists {
		p.EngineOK = ok
	}
}

func (r *Registry) List() []Project {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Project, 0, len(r.projects))
	for _, p := range r.projects {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastUsed.After(out[j].LastUsed) })
	return out
}

func (r *Registry) Get(id string) (*Project, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	proj, ok := r.projects[id]
	if !ok {
		return nil, fmt.Errorf("project not found: %s", id)
	}
	p := *proj
	return &p, nil
}

func (r *Registry) Remove(id string) error {
	r.mu.Lock()
	if _, ok := r.projects[id]; !ok {
		r.mu.Unlock()
		return fmt.Errorf("project not found: %s", id)
	}
	delete(r.projects, id)
	notifyFn := r.notify
	r.mu.Unlock()
	notifyFn()
	return nil
}
