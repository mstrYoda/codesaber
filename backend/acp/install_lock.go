package acp

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func acquireInstallLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if os.IsExist(err) {
		b, readErr := os.ReadFile(path)
		pid, parseErr := strconv.Atoi(strings.TrimSpace(string(b)))
		if readErr == nil && parseErr == nil && pid > 0 && !processAlive(pid) {
			if err := os.Remove(path); err != nil {
				return nil, err
			}
			f, err = os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		}
	}
	if err != nil {
		return nil, err
	}
	if _, err := fmt.Fprint(f, os.Getpid()); err != nil {
		f.Close()
		os.Remove(path)
		return nil, err
	}
	return f, nil
}
