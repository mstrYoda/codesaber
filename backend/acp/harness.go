package acp

import (
	"errors"
	"fmt"
	"os/exec"
)

// Info describes one launchable ACP agent profile.
type Info struct {
	Name      string   `json:"name"`
	Command   []string `json:"command"`
	Available bool     `json:"available"`
	Managed   bool     `json:"managed"`
	Version   string   `json:"version"`
	Isolated  bool     `json:"isolated"`
}

// ACP launch commands per agent, documented on https://agentclientprotocol.com:
//   - opencode: native ACP support, `opencode acp`
//   - claude: via claude-agent-acp (legacy claude-code-acp is also accepted)
var profileCommands = []Info{
	{Name: "opencode", Command: []string{"opencode", "acp"}},
	{Name: "claude", Command: []string{"claude-agent-acp"}},
	{Name: "antigravity", Command: []string{"agy_acp_server"}},
}

// DefaultProfiles returns the known agent profiles with Available set by an
// exec.LookPath check on the command's first element.
func DefaultProfiles() []Info {
	out := make([]Info, 0, len(profileCommands))
	for _, p := range profileCommands {
		if managed, err := managedProfile(p.Name); err == nil {
			out = append(out, *managed)
			continue
		}
		p.Isolated = readOptions(p.Name).Isolated
		command, err := resolveCommand(p.Command, findExecutable)
		p.Available = err == nil
		if err == nil {
			p.Command = command
		}
		out = append(out, p)
	}
	return out
}

// Resolve returns the profile for name with its availability refreshed, or an
// error if the agent binary is not on PATH.
func Resolve(name string) (*Info, error) {
	if managed, err := managedProfile(name); err == nil {
		return managed, nil
	}
	for _, p := range profileCommands {
		if p.Name != name {
			continue
		}
		command, err := resolveCommand(p.Command, findExecutable)
		if err != nil {
			return nil, fmt.Errorf("acp: agent %q not found on PATH", name)
		}
		profile := p
		profile.Command = command
		profile.Available = true
		profile.Isolated = readOptions(name).Isolated
		return &profile, nil
	}
	return nil, errors.New("acp: unknown agent profile " + name)
}

func lookupAvailable(command []string) bool {
	if len(command) == 0 {
		return false
	}
	_, err := resolveCommand(command, exec.LookPath)
	return err == nil
}

// Prefer the current adapter, preserving compatibility with existing installs.
// Resolve the absolute executable once so discovery and launch use the same file.
func resolveCommand(command []string, lookup func(string) (string, error)) ([]string, error) {
	if len(command) == 0 {
		return nil, errors.New("acp: empty command")
	}
	names := []string{command[0]}
	if command[0] == "claude-agent-acp" {
		names = append(names, "claude-code-acp")
	}
	var lastErr error
	for _, name := range names {
		path, err := lookup(name)
		if err == nil {
			return append([]string{path}, command[1:]...), nil
		}
		lastErr = err
	}
	return nil, lastErr
}
