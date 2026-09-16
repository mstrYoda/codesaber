// Package settings persists global app preferences as JSON at
// <user-config-dir>/codesaber/settings.json. It is intentionally tiny: one Model,
// Load/Save with an atomic tmp+rename write, and Sanitize for input
// validation at the RPC boundary.
package settings

import (
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

// Model is the full settings document. Zero values mean "default":
// EditorFontSizePx 0 renders as 13px and AccentColor "" renders as
// #4a5bfc (the :root CSS defaults).
type Model struct {
	BracketColors      bool   `json:"bracketColors"`
	Minimap            bool   `json:"minimap"`
	TerminalShell      string `json:"terminalShell"`              // "" = platform default
	EditorFontSizePx   int    `json:"editorFontSizePx,omitempty"` // 0 = 13
	SearchIncludeGlobs string `json:"searchIncludeGlobs"`
	AccentColor        string `json:"accentColor,omitempty"` // hex, "" default #4a5bfc
	PerfHud            bool   `json:"perfHud,omitempty"`
	VimMode            bool   `json:"vimMode,omitempty"`
}

// Default returns the settings used when nothing has been persisted yet.
func Default() Model {
	return Model{BracketColors: true, Minimap: true}
}

// Store persists the settings document at a fixed path. Safe for the
// serialized RPC access pattern; Load/Save are independent filesystem ops.
type Store struct{ path string }

// NewStore returns a Store persisting to path.
func NewStore(path string) *Store { return &Store{path: path} }

// DefaultPath returns <user-config-dir>/codesaber/settings.json, falling back to
// ~/.config/codesaber/settings.json (then the temp dir) when UserConfigDir fails.
func DefaultPath() string {
	if base, err := os.UserConfigDir(); err == nil {
		return filepath.Join(base, "codesaber", "settings.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "codesaber", "settings.json")
	}
	return filepath.Join(home, ".config", "codesaber", "settings.json")
}

// Load reads the settings file; a missing or corrupt file yields Default.
func (s *Store) Load() Model {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return Default()
	}
	var m Model
	if err := json.Unmarshal(data, &m); err != nil {
		return Default()
	}
	return m
}

// Save persists m atomically: write to a temp file in the same directory,
// then rename over the target so a crash never leaves a truncated file.
func (s *Store) Save(m Model) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "settings.json.tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op after successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.path)
}

// fontSizeMin/Max bound the editor font size stepper.
const (
	fontSizeMin = 11
	fontSizeMax = 18
)

// Sanitize validates user-supplied settings: the font size is clamped into
// [11,18] (0 stays 0 = default), a non-empty terminal shell that does not
// exist on disk logs a warning but is kept (warn-not-fail), and the accent
// color must be empty or a #rrggbb hex string. warn is injectable for tests;
// nil uses the standard logger.
func Sanitize(m Model, warn func(string)) Model {
	if warn == nil {
		warn = func(msg string) { log.Print("settings: " + msg) }
	}
	if px := m.EditorFontSizePx; px != 0 {
		if px < fontSizeMin {
			m.EditorFontSizePx = fontSizeMin
		} else if px > fontSizeMax {
			m.EditorFontSizePx = fontSizeMax
		}
	}
	if m.TerminalShell != "" {
		if _, err := exec.LookPath(m.TerminalShell); err != nil {
			warn("terminal shell " + m.TerminalShell + " does not exist; keeping value")
		}
	}
	if m.AccentColor != "" && !isHexColor(m.AccentColor) {
		warn("accent color " + m.AccentColor + " is not #rrggbb; resetting")
		m.AccentColor = ""
	}
	return m
}

// isHexColor reports whether s looks like #rgb or #rrggbb.
func isHexColor(s string) bool {
	if len(s) != 7 || s[0] != '#' {
		return false
	}
	for _, c := range s[1:] {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}
