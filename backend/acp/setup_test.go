package acp

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestInstallerLockPreservesActiveProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".install-lock")
	if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
		t.Fatal(err)
	}
	if lock, err := acquireInstallLock(path); err == nil {
		lock.Close()
		t.Fatal("active install lock replaced")
	}
}

func TestCancelledRepairPreservesInstalledFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODESABER_AGENT_HOME", home)
	target := filepath.Join(home, "claude", agentPackages["claude"].Version)
	if err := os.MkdirAll(target, 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(target, "original")
	if err := os.WriteFile(marker, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Repair(ctx, "claude"); err == nil {
		t.Fatal("cancelled repair succeeded")
	}
	b, err := os.ReadFile(marker)
	if err != nil || string(b) != "keep" {
		t.Fatal("existing install was changed on failure")
	}
	if _, err := os.Stat(filepath.Join(home, ".install-lock")); !os.IsNotExist(err) {
		t.Fatal("lock was not released")
	}
}

func TestDownloadRejectsWrongChecksum(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("test runtime")) }))
	defer server.Close()
	if err := downloadVerified(context.Background(), server.URL, filepath.Join(t.TempDir(), "node.zip"), strings.Repeat("0", 64)); err == nil {
		t.Fatal("corrupt runtime accepted")
	}
	h := sha256.Sum256([]byte("test runtime"))
	if err := downloadVerified(context.Background(), server.URL, filepath.Join(t.TempDir(), "node.zip"), hex.EncodeToString(h[:])); err != nil {
		t.Fatal(err)
	}
}
func TestRuntimeArchiveCannotEscape(t *testing.T) {
	for _, name := range []string{"../outside", "..\\outside", "C:/outside", "/outside"} {
		t.Run(name, func(t *testing.T) {
			var b bytes.Buffer
			w := zip.NewWriter(&b)
			f, _ := w.Create(name)
			f.Write([]byte("bad"))
			w.Close()
			dir := t.TempDir()
			archive := filepath.Join(dir, "node.zip")
			os.WriteFile(archive, b.Bytes(), 0600)
			if err := extractRuntime(archive, dir); err == nil {
				t.Fatal("unsafe path accepted")
			}
		})
	}
}
func TestFailedPromotionRestoresExistingInstall(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "current")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(target, "working")
	if err := os.WriteFile(marker, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := promoteInstall(filepath.Join(dir, "missing-stage"), target); err == nil {
		t.Fatal("missing stage should fail")
	}
	data, err := os.ReadFile(marker)
	if err != nil || string(data) != "original" {
		t.Fatalf("working installation lost: %v", err)
	}
}

func TestProviderSettingsAreScoped(t *testing.T) {
	t.Setenv("CODESABER_AGENT_HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", "original")
	if err := SetIsolated("claude", true); err != nil {
		t.Fatal(err)
	}
	env := providerEnvironment("claude", []string{"node"})
	for _, e := range env {
		if strings.HasPrefix(e, "CLAUDE_CONFIG_DIR=") && !strings.Contains(e, "config"+string(os.PathSeparator)+"claude") {
			t.Fatal("wrong config path")
		}
	}
	if os.Getenv("CLAUDE_CONFIG_DIR") != "original" {
		t.Fatal("global environment mutated")
	}
	if readOptions("gemini").Isolated {
		t.Fatal("other provider changed")
	}
	if err := SetIsolated("../../escape", true); err == nil {
		t.Fatal("unknown provider accepted")
	}
	if err := Install(context.Background(), "../../escape"); err == nil {
		t.Fatal("unknown package accepted")
	}
}
func TestDiagnosticsDoNotExposeProviderSecrets(t *testing.T) {
	if code, _ := explainError(errors.New("Antigravity authentication failed")); code != "sign-in-required" {
		t.Fatalf("Antigravity auth was misclassified as %s", code)
	}
	for _, err := range []error{errors.New("auth failed token=secret"), errors.New("HTTP 503 secret"), context.DeadlineExceeded} {
		_, msg := explainError(err)
		if strings.Contains(msg, "secret") {
			t.Fatal("raw error leaked")
		}
	}
}

// Explicit network opt-in. No model inference; also tests missing system Node.
func TestManagedProviderAcceptance(t *testing.T) {
	name := os.Getenv("CODESABER_INSTALL_TEST")
	if name == "" {
		t.Skip("set CODESABER_INSTALL_TEST to opt into provider downloads")
	}
	if os.Getenv("CODESABER_AGENT_HOME") == "" {
		t.Fatal("explicit isolated CODESABER_AGENT_HOME required")
	}
	if os.Getenv("CODESABER_TEST_NO_NODE") == "1" {
		t.Setenv("PATH", filepath.Join(os.Getenv("SystemRoot"), "System32"))
		t.Setenv("ProgramFiles", t.TempDir())
		t.Setenv("APPDATA", t.TempDir())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 9*time.Minute)
	defer cancel()
	if err := Install(ctx, name); err != nil {
		t.Fatal(err)
	}
	p, err := Resolve(name)
	if err != nil || !p.Managed {
		t.Fatalf("managed profile missing: %v", err)
	}
	if err := SetIsolated(name, name == "claude"); err != nil {
		t.Fatal(err)
	}
	probeCtx, probeCancel := context.WithTimeout(ctx, 45*time.Second)
	defer probeCancel()
	d := Check(probeCtx, t.TempDir(), name)
	t.Logf("provider=%s version=%s ready=%v code=%s message=%s model=%s", name, p.Version, d.Ready, d.Code, d.Message, d.Model)
	if name == "claude" && !d.Ready {
		t.Fatal("expected configured Claude/MiniMax session")
	}
}
