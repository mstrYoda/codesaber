//go:build !windows

package acp

import "errors"

func openLoginTerminal(command, env []string, cwd string) error {
	return errors.New("Sign in using your provider CLI in a terminal, then check the connection again.")
}
