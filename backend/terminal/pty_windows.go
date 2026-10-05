package terminal

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"github.com/UserExistsError/conpty"
)

type windowsShell struct {
	*conpty.ConPty
	done      chan struct{}
	code      int
	process   *os.Process
	closeOnce sync.Once
	mu        sync.Mutex
	closed    bool
}

func defaultShell() string {
	for _, shell := range []string{"pwsh.exe", "powershell.exe"} {
		if path, err := exec.LookPath(shell); err == nil {
			return path
		}
	}
	if shell := os.Getenv("COMSPEC"); shell != "" {
		return shell
	}
	return "cmd.exe"
}

func startShell(shell, cwd string, rows, cols int) (shellProcess, error) {
	path, err := exec.LookPath(shell)
	if err != nil {
		return nil, err
	}
	// Quote the executable separately, including paths under Program Files.
	cpty, err := conpty.Start(syscall.EscapeArg(path), conpty.ConPtyWorkDir(cwd), conpty.ConPtyDimensions(cols, rows))
	if err != nil {
		return nil, fmt.Errorf("terminal: Windows ConPTY (Windows 10 1809 or newer required): %w", err)
	}
	process, err := os.FindProcess(cpty.Pid())
	if err != nil {
		_ = cpty.Close()
		return nil, err
	}
	p := &windowsShell{ConPty: cpty, process: process, done: make(chan struct{})}
	go func() {
		code, err := cpty.Wait(context.Background())
		p.code = int(code)
		if err != nil {
			p.code = -1
		}
		// ConPTY holds the output pipe open after the shell exits. Close the
		// console while the reader drains it so the stream receives EOF.
		_ = p.Close()
		_ = p.process.Release()
		close(p.done)
	}()
	return p, nil
}

func (p *windowsShell) Wait() int { <-p.done; return p.code }

func (p *windowsShell) Kill() error { return p.process.Kill() }

func (p *windowsShell) Resize(rows, cols int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return os.ErrClosed
	}
	return p.ConPty.Resize(cols, rows)
}

func (p *windowsShell) Write(data []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return 0, os.ErrClosed
	}
	return p.ConPty.Write(data)
}

func (p *windowsShell) Close() error {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.closed = true
		_ = p.ConPty.Close()
	})
	return nil
}
