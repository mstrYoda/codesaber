//go:build !windows

package acp

import "os/exec"

func configureChildProcess(cmd *exec.Cmd) {}

func trackChildProcess(cmd *exec.Cmd) (func(), error) { return func() {}, nil }
