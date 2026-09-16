//go:build !windows

package backend

import (
	"fmt"
	"os/exec"
	"strings"
)

func readSystemPasteboard() ([]byte, error) {
	cmd := exec.Command("swift", "-e", swiftPasteboardReader)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("pasteboard read: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}
