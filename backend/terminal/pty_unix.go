//go:build !windows

package terminal

import (
	"os"
	"os/exec"

	"github.com/creack/pty"
)

type unixShell struct {
	*os.File
	cmd *exec.Cmd
}

func defaultShell() string { return "/bin/sh" }

func startShell(shell, cwd string, rows, cols int) (shellProcess, error) {
	cmd := exec.Command(shell)
	cmd.Dir = cwd
	cmd.Env = append(cmd.Environ(), "TERM=xterm-256color")
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
	if err != nil {
		return nil, err
	}
	return &unixShell{File: ptmx, cmd: cmd}, nil
}

func (p *unixShell) Resize(rows, cols int) error {
	return pty.Setsize(p.File, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
}

func (p *unixShell) Wait() int {
	_ = p.cmd.Wait()
	if p.cmd.ProcessState != nil && p.cmd.ProcessState.ExitCode() > 0 {
		return p.cmd.ProcessState.ExitCode()
	}
	return 0
}

func (p *unixShell) Kill() error { return p.cmd.Process.Kill() }
