package backend

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Windows PowerShell is shipped with Windows and provides the STA clipboard
// APIs without a compiler/runtime install. The script is fixed, with no path
// interpolation; UTF-8 preserves file names independently of console locale.
const windowsPasteboardReader = `
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)
Add-Type -AssemblyName System.Windows.Forms
$items = @()
if ([Windows.Forms.Clipboard]::ContainsFileDropList()) {
    foreach ($path in [Windows.Forms.Clipboard]::GetFileDropList()) {
        $entry = Get-Item -LiteralPath $path -Force
        $items += @{ name=$entry.Name; isDir=[bool]$entry.PSIsContainer; source=$entry.FullName; dataB64='' }
    }
} elseif ([Windows.Forms.Clipboard]::ContainsImage()) {
    $image = [Windows.Forms.Clipboard]::GetImage()
    $stream = New-Object IO.MemoryStream
    try {
        $image.Save($stream, [Drawing.Imaging.ImageFormat]::Png)
        $items += @{ name=''; isDir=$false; source=''; dataB64=[Convert]::ToBase64String($stream.ToArray()) }
    } finally { $stream.Dispose(); $image.Dispose() }
}
ConvertTo-Json -InputObject @($items) -Compress -Depth 3
`

func readSystemPasteboard() ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	shell := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	cmd := exec.CommandContext(ctx, shell, "-NoProfile", "-NonInteractive", "-STA", "-Command", windowsPasteboardReader)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("pasteboard read: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}
