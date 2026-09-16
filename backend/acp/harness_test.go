package acp

import (
	"errors"
	"testing"
)

func TestDefaultProfilesIncludeOpencodeFirst(t *testing.T) {
	profiles := DefaultProfiles()
	if len(profiles) == 0 {
		t.Fatal("no profiles")
	}
	if profiles[0].Name != "opencode" {
		t.Fatalf("first profile = %q, want opencode", profiles[0].Name)
	}
	for _, p := range profiles {
		if p.Name == "gemini" {
			t.Error("retired Gemini CLI still offered")
		}
		if len(p.Command) == 0 {
			t.Errorf("profile %q: empty command", p.Name)
		}
		if p.Available {
			if _, err := lookupAvailableCheck(p); err != nil {
				t.Errorf("profile %q marked available but lookup failed: %v", p.Name, err)
			}
		}
	}
}

func TestResolveUnknownProfile(t *testing.T) {
	if _, err := Resolve("nonexistent-agent"); err == nil {
		t.Fatal("want error for unknown profile")
	}
}

func TestResolveUnavailableBinary(t *testing.T) {
	_, err := resolveCommand([]string{"claude-agent-acp"}, func(string) (string, error) { return "", errors.New("missing") })
	if err == nil {
		t.Fatal("missing adapters must not resolve")
	}
}

func TestClaudeAdapterResolution(t *testing.T) {
	for _, currentInstalled := range []bool{true, false} {
		t.Run(map[bool]string{true: "prefer current", false: "legacy fallback"}[currentInstalled], func(t *testing.T) {
			original := []string{"claude-agent-acp", "--example"}
			command, err := resolveCommand(original, func(name string) (string, error) {
				if name == "claude-agent-acp" && !currentInstalled {
					return "", errors.New("missing")
				}
				return "/installed/" + name, nil
			})
			want := "/installed/claude-agent-acp"
			if !currentInstalled {
				want = "/installed/claude-code-acp"
			}
			if err != nil || len(command) != 2 || command[0] != want || command[1] != "--example" {
				t.Fatalf("resolved %v, %v", command, err)
			}
			if original[0] != "claude-agent-acp" {
				t.Fatal("modified shared profile")
			}
		})
	}
}

func lookupAvailableCheck(p Info) (struct{}, error) {
	if !lookupAvailable(p.Command) {
		return struct{}{}, errors.New("not on PATH")
	}
	return struct{}{}, nil
}
