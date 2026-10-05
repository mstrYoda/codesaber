package acp

import (
	"errors"
	"os"
	"path/filepath"
)

func Login(name string) error {
	p, err := Resolve(name)
	if err != nil {
		return errors.New("Install this provider first.")
	}
	command := append([]string(nil), p.Command...)
	switch name {
	case "claude":
		// The pinned adapter exposes its bundled native CLI via --cli.
		if !p.Managed {
			return errors.New("Install the managed Claude adapter to use guided sign-in.")
		}
		command = append(command, "--cli", "auth", "login")
	case "gemini":
		command = command[:len(command)-1]
	case "opencode":
		command = append(command[:len(command)-1], "auth", "login")
	default:
		return errors.New("Unknown provider")
	}
	home, err := agentHome()
	if err != nil {
		return err
	}
	cwd := filepath.Join(home, "login")
	if err := os.MkdirAll(cwd, 0700); err != nil {
		return err
	}
	return openLoginTerminal(command, providerEnvironment(name, command), cwd)
}
