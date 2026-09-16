//go:build !windows

package acp

import "syscall"

func processAlive(pid int) bool { return syscall.Kill(pid, 0) != syscall.ESRCH }
