package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Reviewed versions, updated deliberately with a new compatibility test.
type agentPackage struct {
	Package, Version, Entry string
	Native                  bool
}

var agentPackages = map[string]agentPackage{
	"claude":      {"@agentclientprotocol/claude-agent-acp", "0.77.0", "dist/index.js", false},
	"opencode":    {"opencode-ai", "1.18.31", "bin/opencode", true},
	"antigravity": {"", "1.1.1", "agy_acp_server.exe", true},
}
var setupMu sync.Mutex
var installStateMu sync.Mutex
var installCancel context.CancelFunc

func CancelInstall() {
	installStateMu.Lock()
	defer installStateMu.Unlock()
	if installCancel != nil {
		installCancel()
	}
}

// CODESABER_AGENT_HOME supports portable deployments and isolated acceptance tests.
func agentHome() (string, error) {
	if dir := os.Getenv("CODESABER_AGENT_HOME"); dir != "" {
		return filepath.Abs(dir)
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "codesaber", "agents"), nil
}

type agentOptions struct {
	Isolated bool `json:"isolated"`
}

func readOptions(name string) agentOptions {
	dir, err := agentHome()
	if err != nil {
		return agentOptions{}
	}
	b, _ := os.ReadFile(filepath.Join(dir, name+"-settings.json"))
	var options agentOptions
	_ = json.Unmarshal(b, &options)
	return options
}

func SetIsolated(name string, isolated bool) error {
	if _, ok := agentPackages[name]; !ok {
		return errors.New("unknown provider")
	}
	setupMu.Lock()
	defer setupMu.Unlock()
	dir, err := agentHome()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	b, _ := json.Marshal(agentOptions{isolated})
	return os.WriteFile(filepath.Join(dir, name+"-settings.json"), b, 0600)
}

func providerEnvironment(name string, command []string) []string {
	env := os.Environ()
	// npm shims and SDK subprocesses can find our private runtime as well.
	if len(command) > 0 {
		env = replaceEnv(env, "PATH", filepath.Dir(command[0])+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	if readOptions(name).Isolated {
		dir, err := agentHome()
		if err != nil {
			return env
		}
		config := filepath.Join(dir, "config", name)
		switch name {
		case "claude":
			env = replaceEnv(env, "CLAUDE_CONFIG_DIR", config)
		case "gemini":
			env = replaceEnv(env, "GEMINI_CLI_HOME", config)
		case "opencode":
			env = replaceEnv(env, "XDG_CONFIG_HOME", config)
		case "antigravity":
			env = replaceEnv(env, "GEMINI_HOME", config)
		}
	}
	return env
}

func replaceEnv(env []string, key, value string) []string {
	out := make([]string, 0, len(env)+1)
	for _, entry := range env {
		k, _, _ := strings.Cut(entry, "=")
		if !strings.EqualFold(k, key) {
			out = append(out, entry)
		}
	}
	return append(out, key+"="+value)
}

func managedProfile(name string) (*Info, error) {
	pkg, ok := agentPackages[name]
	if !ok {
		return nil, errors.New("unknown provider")
	}
	home, err := agentHome()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(home, name, pkg.Version)
	if _, err := os.Stat(filepath.Join(dir, ".complete")); err != nil {
		return nil, err
	}
	command, err := installedCommand(dir, name, pkg)
	if err != nil {
		return nil, err
	}
	return &Info{Name: name, Command: command, Available: true, Managed: true, Version: pkg.Version, Isolated: readOptions(name).Isolated}, nil
}

func installedCommand(dir, name string, pkg agentPackage) ([]string, error) {
	if name == "antigravity" {
		entry := filepath.Join(dir, pkg.Entry)
		if _, err := os.Stat(entry); err != nil {
			return nil, err
		}
		return []string{entry}, nil
	}
	entry := filepath.Join(dir, "node_modules", filepath.FromSlash(pkg.Package), filepath.FromSlash(pkg.Entry))
	if pkg.Native && runtime.GOOS == "windows" {
		entry += ".exe"
	}
	if _, err := os.Stat(entry); err != nil {
		return nil, err
	}
	command := []string{entry}
	if !pkg.Native {
		node, _, err := findNode()
		if err != nil {
			return nil, err
		}
		command = []string{node, entry}
	}
	switch name {
	case "gemini":
		command = append(command, "--experimental-acp")
	case "opencode":
		command = append(command, "acp")
	}
	return command, nil
}

// Install downloads only the allowlisted package into an app-owned versioned
// directory. A failed download never replaces a usable install.
func Install(ctx context.Context, name string) error {
	return install(ctx, name, false)
}

func Repair(ctx context.Context, name string) error { return install(ctx, name, true) }

func install(ctx context.Context, name string, repair bool) error {
	pkg, ok := agentPackages[name]
	if !ok {
		return errors.New("unknown provider")
	}
	if !setupMu.TryLock() {
		return errors.New("Another provider setup is running. Please wait.")
	}
	defer setupMu.Unlock()
	ctx, cancel := context.WithCancel(ctx)
	installStateMu.Lock()
	installCancel = cancel
	installStateMu.Unlock()
	defer func() { cancel(); installStateMu.Lock(); installCancel = nil; installStateMu.Unlock() }()
	if _, err := managedProfile(name); err == nil && !repair {
		return nil
	}
	home, err := agentHome()
	if err != nil {
		return err
	}
	parent := filepath.Join(home, name)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return err
	}
	// The lock file also protects against simultaneous setup in another app instance.
	lock, err := acquireInstallLock(filepath.Join(home, ".install-lock"))
	if err != nil {
		return errors.New("Provider setup is running in another CodeSaber window. Wait for it to finish or close that window, then retry.")
	}
	_ = lock.Close()
	defer os.Remove(filepath.Join(home, ".install-lock"))
	if name == "antigravity" {
		return installAntigravity(ctx, parent, pkg.Version)
	}
	node, npm, err := ensureNode(ctx)
	if err != nil {
		return err
	}
	stage, err := os.MkdirTemp(parent, ".install-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage) // only the freshly created staging directory
	config := filepath.Join(stage, ".npmrc")
	if err := os.WriteFile(config, []byte("registry=https://registry.npmjs.org/\n"), 0600); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, node, npm, "install", "--prefix", stage, "--userconfig", config,
		"--registry=https://registry.npmjs.org/", "--cache", filepath.Join(home, "npm-cache"), "--no-audit", "--no-fund", "--save-exact", pkg.Package+"@"+pkg.Version)
	configureChildProcess(cmd)
	cmd.Dir = stage
	cmd.Env = replaceEnv(os.Environ(), "PATH", filepath.Dir(node)+string(os.PathListSeparator)+os.Getenv("PATH"))
	// No npm output is returned to the webview: registry credentials can occur in errors.
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return errors.New("Setup cancelled. You can retry at any time.")
		}
		if ctx.Err() != nil {
			return errors.New("Setup timed out. Check your connection and retry.")
		}
		return errors.New("Provider download or install failed. Check network access to npmjs.org and retry.")
	}
	if _, err := installedCommand(stage, name, pkg); err != nil {
		return errors.New("Downloaded provider is incomplete. Retry setup.")
	}
	if err := os.WriteFile(filepath.Join(stage, ".complete"), []byte(pkg.Version), 0600); err != nil {
		return err
	}
	target := filepath.Join(parent, pkg.Version)
	return promoteInstall(stage, target)
}

func promoteInstall(stage, target string) error {
	backup := ""
	// Preserve incomplete/corrupt installs for inspection rather than delete them.
	if _, err := os.Stat(target); err == nil {
		backup = target + ".previous-" + fmt.Sprint(time.Now().UnixNano())
		if err := os.Rename(target, backup); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(stage, target); err != nil {
		if backup != "" {
			if restoreErr := os.Rename(backup, target); restoreErr != nil {
				return fmt.Errorf("install failed: %w; previous installation remains at %s: %v", err, backup, restoreErr)
			}
		}
		return err
	}
	return nil
}

func findExecutable(name string) (string, error) {
	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}
	if runtime.GOOS == "windows" {
		for _, dir := range []string{filepath.Join(os.Getenv("APPDATA"), "npm"), filepath.Join(os.Getenv("USERPROFILE"), ".local", "bin"), filepath.Join(os.Getenv("ProgramFiles"), "nodejs")} {
			for _, ext := range []string{".exe", ".cmd"} {
				path := filepath.Join(dir, name+ext)
				if info, err := os.Stat(path); err == nil && !info.IsDir() {
					return path, nil
				}
			}
		}
	}
	return "", exec.ErrNotFound
}
