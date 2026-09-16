package backend

import (
	"bytes"
	"encoding/base64"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Opt-in because this temporarily owns the desktop clipboard. The outer STA
// process materializes and restores all original formats without logging them.
func TestWindowsClipboardFormats(t *testing.T) {
	mode := os.Getenv("CODESABER_CLIPBOARD_CHILD")
	if mode != "" {
		items, err := (&App{}).PasteboardRead()
		if err != nil {
			t.Fatal(err)
		}
		if mode == "image" {
			if len(items) != 1 {
				t.Fatalf("image count: %d", len(items))
			}
			data, err := base64.StdEncoding.DecodeString(items[0].DataB64)
			if err != nil {
				t.Fatal(err)
			}
			im, err := png.Decode(bytes.NewReader(data))
			if err != nil || im.Bounds().Dx() != 4 {
				t.Fatalf("invalid PNG: %v", err)
			}
		} else {
			if len(items) != 2 || items[0].Name != "çığ.txt" || items[0].IsDir || !items[1].IsDir {
				t.Fatalf("unexpected file list: %+v", items)
			}
		}
		app, _ := newTestApp(t)
		root := os.Getenv("CODESABER_CLIPBOARD_ROOT")
		if _, err := app.OpenProject(root); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(root, "pasted-"+mode)
		if err := os.Mkdir(target, 0700); err != nil {
			t.Fatal(err)
		}
		if err := app.PasteInto(target, items); err != nil {
			t.Fatal(err)
		}
		entries, err := os.ReadDir(target)
		if err != nil || len(entries) != len(items) {
			t.Fatalf("paste output: count=%d err=%v", len(entries), err)
		}
		if mode == "files" {
			data, err := os.ReadFile(filepath.Join(target, "çığ.txt"))
			if err != nil || string(data) != "clipboard fixture" {
				t.Fatalf("copied bytes: %q, %v", data, err)
			}
		}
		return
	}
	if os.Getenv("CODESABER_TEST_CLIPBOARD") != "1" {
		t.Skip("requires exclusive clipboard access")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "çığ.txt"), []byte("clipboard fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "klasör"), 0700); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	script := `
$ErrorActionPreference='Stop'
Add-Type -AssemblyName System.Windows.Forms
$original=[Windows.Forms.Clipboard]::GetDataObject()
$saved=New-Object Windows.Forms.DataObject
if ($null -ne $original) {
  foreach ($format in $original.GetFormats($false)) { $saved.SetData($format, $false, $original.GetData($format, $false)) }
}
try {
  $files=New-Object Collections.Specialized.StringCollection
  [void]$files.Add((Join-Path $env:CODESABER_CLIPBOARD_ROOT 'çığ.txt'))
  [void]$files.Add((Join-Path $env:CODESABER_CLIPBOARD_ROOT 'klasör'))
  [Windows.Forms.Clipboard]::SetFileDropList($files)
  $env:CODESABER_CLIPBOARD_CHILD='files'
  & $env:CODESABER_CLIPBOARD_EXE '-test.run=^TestWindowsClipboardFormats$' '-test.v'
  if ($LASTEXITCODE -ne 0) { throw 'file clipboard check failed' }
  $bitmap=New-Object Drawing.Bitmap 4,4
  try { [Windows.Forms.Clipboard]::SetImage($bitmap) } finally { $bitmap.Dispose() }
  $env:CODESABER_CLIPBOARD_CHILD='image'
  & $env:CODESABER_CLIPBOARD_EXE '-test.run=^TestWindowsClipboardFormats$' '-test.v'
  if ($LASTEXITCODE -ne 0) { throw 'image clipboard check failed' }
} finally {
  if ($null -eq $original) { [Windows.Forms.Clipboard]::Clear() }
  else { [Windows.Forms.Clipboard]::SetDataObject($saved, $true) }
}
`
	cmd := exec.Command(filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe"), "-NoProfile", "-NonInteractive", "-STA", "-Command", script)
	cmd.Env = append(os.Environ(), "CODESABER_CLIPBOARD_EXE="+exe, "CODESABER_CLIPBOARD_ROOT="+root)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clipboard integration: %v\n%s", err, out)
	}
}
