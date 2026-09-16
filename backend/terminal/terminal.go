// Package terminal provides a PTY-backed shell session engine.
package terminal

import (
	"errors"
	"io"
	"sync"
)

const (
	maxFrameSize = 4096
	defaultRows  = 24
	defaultCols  = 80
)

// SessionOpts configures a new shell session.
type SessionOpts struct {
	ID    string
	Cwd   string
	Shell string
	Rows  int
	Cols  int
}

// Event is one frame of terminal output, or the single exit notification.
type Event struct {
	SessionID string
	Data      []byte
	Exit      bool
	Code      int
}

// shellProcess hides the Unix PTY / Windows ConPTY implementation.
type shellProcess interface {
	io.ReadWriteCloser
	Resize(rows, cols int) error
	Wait() int
	Kill() error
}

// Session is a live PTY attached to a child shell process.
type Session struct {
	opts   SessionOpts
	pty    shellProcess
	data   chan Event
	exited chan struct{}
	ready  chan struct{}
	wg     sync.WaitGroup

	waitOnce   sync.Once
	waitCode   int
	closeOnce  sync.Once
	exitedOnce sync.Once
}

// New starts opts.Shell (with Dir=opts.Cwd) inside a fresh PTY sized
// opts.Rows x opts.Cols (defaults 24x80) and begins forwarding output.
func New(opts SessionOpts) (*Session, error) {
	shell := opts.Shell
	if shell == "" {
		shell = defaultShell()
	}
	rows, cols := opts.Rows, opts.Cols
	if rows <= 0 {
		rows = defaultRows
	}
	if cols <= 0 {
		cols = defaultCols
	}
	if rows > 32767 || cols > 32767 {
		return nil, errors.New("terminal: invalid size")
	}
	ptmx, err := startShell(shell, opts.Cwd, rows, cols)
	if err != nil {
		return nil, err
	}
	s := &Session{
		opts:   opts,
		pty:    ptmx,
		data:   make(chan Event, 64),
		exited: make(chan struct{}),
		ready:  make(chan struct{}),
	}
	s.wg.Add(1)
	go s.readLoop()
	close(s.ready)
	return s, nil
}

// Data returns the event stream. It is idempotent: every call returns the
// same channel, which is closed after the single exit event.
func (s *Session) Data() <-chan Event {
	<-s.ready
	return s.data
}

// readLoop forwards output in frames of at most maxFrameSize bytes, then
// emits exactly one exit event carrying the child's exit code.
func (s *Session) readLoop() {
	defer s.wg.Done()
	defer close(s.data)
	buf := make([]byte, maxFrameSize)
	for {
		n, err := s.pty.Read(buf)
		if n > 0 {
			frame := make([]byte, n)
			copy(frame, buf[:n])
			select {
			case s.data <- Event{SessionID: s.opts.ID, Data: frame}:
			case <-s.exited:
				return
			}
		}
		if err != nil {
			break
		}
	}
	code := s.wait()
	select {
	case s.data <- Event{SessionID: s.opts.ID, Exit: true, Code: code}:
	case <-s.exited:
		return
	}
	_ = code
}

// wait blocks for the child process exactly once and records its exit code.
func (s *Session) wait() int {
	s.waitOnce.Do(func() {
		s.waitCode = s.pty.Wait()
	})
	return s.waitCode
}

// Input writes bytes to the shell's side of the PTY.
func (s *Session) Input(b []byte) error {
	_, err := s.pty.Write(b)
	return err
}

// Resize updates the PTY window size.
func (s *Session) Resize(rows, cols int) error {
	if rows <= 0 || cols <= 0 || rows > 32767 || cols > 32767 {
		return errors.New("terminal: invalid size")
	}
	return s.pty.Resize(rows, cols)
}

// Close terminates the shell, releases the PTY and joins the reader
// goroutine. It is idempotent and safe for concurrent use.
func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		_ = s.pty.Kill()
	})
	<-s.ready
	s.wait()
	_ = s.pty.Close()
	// Join the read loop FIRST: its final Exit event must be delivered while
	// the exited guard is still open, otherwise the select can race and drop
	// the exit notification the facade depends on.
	s.wg.Wait()
	s.exitedOnce.Do(func() { close(s.exited) })
	return nil
}
