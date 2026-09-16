package acp

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const nodeVersion = "24.21.0"

var nodeChecksums = map[string]string{
	"amd64": "158f7685b44de51f6c0df1d153526cbcd3e1bc739a8dfc607721cef75de9e541",
	"arm64": "8779b1bde1d39f8d420e3b57aa657b39891af434d3de44a919044cec06785921",
}

func privateNodeDir() string {
	home, err := agentHome()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "runtime", "node-"+nodeVersion+"-"+runtime.GOARCH)
}
func findNode() (string, string, error) {
	var candidates []string
	if runtime.GOOS == "windows" {
		candidates = append(candidates, filepath.Join(privateNodeDir(), "node.exe"))
	}
	if node, err := findExecutable("node"); err == nil {
		candidates = append(candidates, node)
	}
	for _, node := range candidates {
		if _, err := os.Stat(node); err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		cmd := exec.CommandContext(ctx, node, "--version")
		configureChildProcess(cmd)
		out, err := cmd.Output()
		cancel()
		parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(string(out)), "v"), ".")
		major, _ := strconv.Atoi(parts[0])
		if err != nil || major < 22 {
			continue
		}
		for _, npm := range []string{filepath.Join(filepath.Dir(node), "node_modules", "npm", "bin", "npm-cli.js"), filepath.Join(filepath.Dir(node), "..", "lib", "node_modules", "npm", "bin", "npm-cli.js")} {
			if _, err := os.Stat(npm); err == nil {
				return node, npm, nil
			}
		}
	}
	return "", "", errors.New("Node.js 22 or newer with npm is required.")
}

func ensureNode(ctx context.Context) (string, string, error) {
	if node, npm, err := findNode(); err == nil {
		return node, npm, nil
	}
	checksum, supported := nodeChecksums[runtime.GOARCH]
	if runtime.GOOS != "windows" || !supported {
		return "", "", errors.New("Install Node.js 22 or newer with npm, then retry provider setup.")
	}
	target := privateNodeDir()
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return "", "", err
	}
	stage, err := os.MkdirTemp(filepath.Dir(target), ".download-")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(stage)
	arch := "x64"
	if runtime.GOARCH == "arm64" {
		arch = "arm64"
	}
	base := "node-v" + nodeVersion + "-win-" + arch
	archive := filepath.Join(stage, "node.zip")
	if err := downloadVerified(ctx, "https://nodejs.org/dist/v"+nodeVersion+"/"+base+".zip", archive, checksum); err != nil {
		return "", "", err
	}
	if err := extractRuntime(archive, stage); err != nil {
		return "", "", err
	}
	if err := promoteInstall(filepath.Join(stage, base), target); err != nil {
		return "", "", err
	}
	return findNode()
}

func downloadVerified(ctx context.Context, url, dest, expected string) error {
	return downloadVerifiedLimit(ctx, url, dest, expected, 160<<20)
}

func downloadVerifiedLimit(ctx context.Context, url, dest, expected string, limit int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("Download failed. Check your network connection and retry.")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Download returned HTTP %d", resp.StatusCode)
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, limit+1))
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if n > limit || !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), expected) {
		return errors.New("Checksum verification failed; downloaded code was not executed.")
	}
	return nil
}

func extractRuntime(archive, dest string) error {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer r.Close()
	var total int64
	for _, file := range r.File {
		name := strings.ReplaceAll(file.Name, "\\", "/")
		if !filepath.IsLocal(filepath.FromSlash(name)) || strings.Contains(name, ":") || file.Mode()&os.ModeSymlink != 0 {
			return errors.New("Unsafe runtime archive path")
		}
		path := filepath.Join(dest, filepath.FromSlash(name))
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(path, 0700); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		src, err := file.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
		if err != nil {
			src.Close()
			return err
		}
		n, err := io.Copy(out, io.LimitReader(src, (1<<30)-total+1))
		total += n
		src.Close()
		closeErr := out.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if total > 1<<30 {
			return errors.New("Runtime archive is too large")
		}
	}
	return nil
}
