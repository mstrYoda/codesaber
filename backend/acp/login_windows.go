package acp

import (
	"encoding/base64"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf16"
)

func openLoginTerminal(command, env []string, cwd string) error {
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	parts := []string{"&"}
	for _, arg := range command {
		parts = append(parts, quote(arg))
	}
	script := "$Host.UI.RawUI.WindowTitle = 'CodeSaber - provider sign-in'; " + strings.Join(parts, " ") + "; Write-Host 'Return to CodeSaber and check the connection after signing in.'"
	words := utf16.Encode([]rune(script))
	bytes := make([]byte, len(words)*2)
	for i, w := range words {
		binary.LittleEndian.PutUint16(bytes[i*2:], w)
	}
	// Only fixed switches and base64 reach cmd parsing. Provider paths are
	// PowerShell single-quoted inside the encoded command, never shell-expanded.
	cmd := exec.Command(filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe"), "/d", "/c", "start", "\"\"", "powershell.exe", "-NoProfile", "-NoExit", "-EncodedCommand", base64.StdEncoding.EncodeToString(bytes))
	configureChildProcess(cmd)
	cmd.Env = env
	cmd.Dir = cwd
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}
