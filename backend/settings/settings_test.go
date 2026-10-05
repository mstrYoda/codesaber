package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefault(t *testing.T) {
	d := Default()
	if !d.BracketColors {
		t.Errorf("BracketColors default = false, want true")
	}
	if !d.Minimap {
		t.Errorf("Minimap default = false, want true")
	}
	if d.TerminalShell != "" {
		t.Errorf("TerminalShell default = %q, want empty", d.TerminalShell)
	}
	if d.EditorFontSizePx != 0 {
		t.Errorf("EditorFontSizePx default = %d, want 0 (=13)", d.EditorFontSizePx)
	}
	if d.SearchIncludeGlobs != "" {
		t.Errorf("SearchIncludeGlobs default = %q, want empty", d.SearchIncludeGlobs)
	}
	if d.AccentColor != "" {
		t.Errorf("AccentColor default = %q, want empty (=#4a5bfc)", d.AccentColor)
	}
}

func TestLoad_MissingFileReturnsDefault(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "nested", "settings.json"))
	got := s.Load()
	if got != Default() {
		t.Errorf("Load missing = %+v, want default %+v", got, Default())
	}
}

func TestSaveLoadRoundtrip(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "settings.json"))
	want := Model{
		BracketColors:      false,
		Minimap:            false,
		TerminalShell:      "/bin/zsh",
		EditorFontSizePx:   16,
		SearchIncludeGlobs: "*.go",
		AccentColor:        "#7c3aed",
	}
	if err := s.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := s.Load(); got != want {
		t.Errorf("roundtrip = %+v, want %+v", got, want)
	}
}

func TestLoad_CorruptFileReturnsDefault(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(p, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewStore(p)
	if got := s.Load(); got != Default() {
		t.Errorf("Load corrupt = %+v, want default %+v", got, Default())
	}
}

func TestSave_IsAtomicViaTmpAndRename(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(filepath.Join(dir, "settings.json"))
	if err := s.Save(Default()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("leftover temp file %q after Save", e.Name())
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatalf("read saved file: %v", err)
	}
	var m Model
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("saved file is not valid JSON: %v", err)
	}
}

func TestSanitize_ClampsFontSize(t *testing.T) {
	cases := []struct {
		in, want int
	}{
		{0, 0},   // 0 = default (13), preserved
		{13, 13}, // in range untouched
		{11, 11},
		{18, 18},
		{5, 11},  // below → clamped up
		{25, 18}, // above → clamped down
		{-3, 11},
	}
	for _, c := range cases {
		got := Sanitize(Model{EditorFontSizePx: c.in}, nil).EditorFontSizePx
		if got != c.want {
			t.Errorf("Sanitize(font=%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestSanitize_ShellWarnNotFail(t *testing.T) {
	var warns []string
	m := Sanitize(Model{TerminalShell: "/nonexistent/shell"}, func(msg string) {
		warns = append(warns, msg)
	})
	if m.TerminalShell != "/nonexistent/shell" {
		t.Errorf("invalid shell = %q, want kept (warn-not-fail)", m.TerminalShell)
	}
	if len(warns) != 1 {
		t.Errorf("warn count = %d, want 1 (warns: %v)", len(warns), warns)
	}

	// a real executable must not warn
	real, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var warns2 []string
	Sanitize(Model{TerminalShell: real}, func(msg string) { warns2 = append(warns2, msg) })
	if len(warns2) != 0 {
		t.Errorf("valid shell warned: %v", warns2)
	}

	// empty shell = default, never warns
	var warns3 []string
	Sanitize(Model{}, func(msg string) { warns3 = append(warns3, msg) })
	if len(warns3) != 0 {
		t.Errorf("empty shell warned: %v", warns3)
	}
}

func TestSanitize_AccentColorAcceptsHexAndDropsGarbage(t *testing.T) {
	if got := Sanitize(Model{AccentColor: "#7c3aed"}, nil).AccentColor; got != "#7c3aed" {
		t.Errorf("valid hex = %q, want kept", got)
	}
	if got := Sanitize(Model{AccentColor: "purple"}, nil).AccentColor; got != "" {
		t.Errorf("garbage accent = %q, want empty", got)
	}
}
