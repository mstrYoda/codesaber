package backend

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// PasteItem is one entry read from the pasteboard: either a file/folder
// reference (dataB64 empty, Source set) or inline image data (screenshots;
// Source empty, dataB64 holds the PNG bytes).
type PasteItem struct {
	Name    string `json:"name"`
	IsDir   bool   `json:"isDir"`
	Source  string `json:"source"`
	DataB64 string `json:"dataB64,omitempty"`
}

// swiftPasteboardReader prints one JSON object describing the pasteboard:
// {"items":[{name,isDir,source,dataB64}...]}. File URLs come through as
// items with Source set; PNG/TIFF image data becomes an item with dataB64
// (TIFF is converted to PNG by AppKit on the way out).
const swiftPasteboardReader = `import AppKit
import Foundation
struct Out: Encodable { var name: String; var isDir: Bool; var source: String; var dataB64: String }
let pb = NSPasteboard.general
var items: [Out] = []
if let urls = pb.readObjects(forClasses: [NSURL.self], options: [NSPasteboard.ReadingOptionKey.urlReadingFileURLsOnly: true]) as? [URL] {
    for u in urls {
        var isDir: ObjCBool = false
        FileManager.default.fileExists(atPath: u.path, isDirectory: &isDir)
        items.append(Out(name: u.lastPathComponent, isDir: isDir.boolValue, source: u.path, dataB64: ""))
    }
}
if items.isEmpty {
    for t in [NSPasteboard.PasteboardType.png, .tiff] {
        if let data = pb.data(forType: t) {
            var png = data
            if t == .tiff, let rep = NSBitmapImageRep(data: data), let conv = rep.representation(using: .png, properties: [:]) {
                png = conv
            }
            items.append(Out(name: "", isDir: false, source: "", dataB64: png.base64EncodedString()))
            break
        }
    }
}
let d = JSONEncoder()
FileHandle.standardOutput.write(try! d.encode(items))`

// PasteboardRead returns the pasteboard contents as pasteable items: copied
// files/folders (Source = absolute path) or a screenshot image (DataB64 =
// PNG). An empty result means nothing pasteable is on the pasteboard.
func (a *App) PasteboardRead() ([]PasteItem, error) {
	out, err := readSystemPasteboard()
	if err != nil {
		return nil, err
	}
	var items []PasteItem
	if err := json.Unmarshal(out, &items); err != nil {
		return nil, fmt.Errorf("pasteboard read: %w", err)
	}
	return items, nil
}

// PasteInto writes the items into target directory dir. File/folder items
// are copied recursively; image items are written as screenshot-<ts>.png.
// Refuses targets outside open project roots.
func (a *App) PasteInto(dir string, items []PasteItem) error {
	if err := a.validateProjectPath(dir); err != nil {
		return err
	}
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("stat %s: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	for _, it := range items {
		if it.Source != "" {
			if err := a.pasteFromSource(dir, it); err != nil {
				return err
			}
		} else if it.DataB64 != "" {
			if err := a.pasteImageData(dir, it); err != nil {
				return err
			}
		}
	}
	return nil
}

// pasteFromSource copies one file or directory tree into dir, deduplicating
// names with " copy"/" copy 2" suffixes (Finder-style) instead of failing.
func (a *App) pasteFromSource(dir string, it PasteItem) error {
	if err := a.validateProjectPath(it.Source); err != nil {
		return fmt.Errorf("clipboard source %s is outside an open project: %w", it.Source, err)
	}
	info, err := os.Stat(it.Source)
	if err != nil {
		return fmt.Errorf("stat %s: %w", it.Source, err)
	}
	target := uniqueTarget(dir, it.Name)
	if info.IsDir() {
		if err := copyTree(it.Source, target); err != nil {
			return err
		}
	} else {
		if err := copyFile(it.Source, target, info.Mode()); err != nil {
			return err
		}
	}
	return nil
}

// pasteImageData writes a pasteboard image (screenshot) as PNG, named
// screenshot-<unix>.png with the same dedup suffix scheme.
func (a *App) pasteImageData(dir string, it PasteItem) error {
	data, err := base64.StdEncoding.DecodeString(it.DataB64)
	if err != nil {
		return fmt.Errorf("decode image: %w", err)
	}
	if len(data) == 0 {
		return fmt.Errorf("empty image data")
	}
	name := it.Name
	if name == "" {
		name = fmt.Sprintf("screenshot-%d.png", time.Now().Unix())
	}
	target := uniqueTarget(dir, name)
	if err := os.WriteFile(target, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", target, err)
	}
	return nil
}

// uniqueTarget resolves name collisions in dir with " copy", " copy 2"…
// suffixes before the extension, so pasting twice never clobbers.
func uniqueTarget(dir, name string) string {
	target := filepath.Join(dir, name)
	if _, err := os.Stat(target); err != nil {
		return target
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for n := 1; ; n++ {
		suffix := " copy"
		if n > 1 {
			suffix = fmt.Sprintf(" copy %d", n)
		}
		candidate := filepath.Join(dir, base+suffix+ext)
		if _, err := os.Stat(candidate); err != nil {
			return candidate
		}
	}
}

// copyFile copies one file, preserving the mode.
func copyFile(src, dst string, mode os.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}
	if err := os.WriteFile(dst, data, mode); err != nil {
		return fmt.Errorf("write %s: %w", dst, err)
	}
	return nil
}

// copyTree recursively copies a directory tree (files, subdirs, symlinks
// skipped for safety).
func copyTree(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat %s: %w", src, err)
	}
	if err := os.MkdirAll(dst, info.Mode()); err != nil {
		return fmt.Errorf("mkdir %s: %w", dst, err)
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}
	for _, e := range entries {
		s := filepath.Join(src, e.Name())
		d := filepath.Join(dst, e.Name())
		if e.Type()&os.ModeSymlink != 0 {
			continue
		}
		if e.IsDir() {
			if err := copyTree(s, d); err != nil {
				return err
			}
		} else {
			fi, err := os.Stat(s)
			if err != nil {
				return fmt.Errorf("stat %s: %w", s, err)
			}
			if err := copyFile(s, d, fi.Mode()); err != nil {
				return err
			}
		}
	}
	return nil
}
