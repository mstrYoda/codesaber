package acp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

// Official Google distribution, pinned from the ACP registry. The hash is of
// the downloaded 1.1.1 Windows x64 release, verified during local acceptance.
const antigravityURL = "https://dl.google.com/agy-extensions/releases/windows/agy-acp-server-agy_acp_server_1.1.1-windows-x86_64.zip"
const antigravitySHA256 = "47cb50eef14f0a4655d78cfcfda869bcea7aaee5f9787e936bc2935ea612c3b8"

func installAntigravity(ctx context.Context, parent, version string) error {
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		return errors.New("Managed Antigravity installation is currently supported on Windows x64. Install the official ACP server for your platform and add it to PATH.")
	}
	stage, err := os.MkdirTemp(parent, ".install-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	archive := filepath.Join(stage, "download.zip")
	if err := downloadVerifiedLimit(ctx, antigravityURL, archive, antigravitySHA256, 700<<20); err != nil {
		return err
	}
	if err := extractRuntime(archive, stage); err != nil {
		return err
	}
	if err := os.Remove(archive); err != nil {
		return err
	}
	for _, name := range []string{"agy_acp_server.exe", "localharness_external.exe"} {
		if _, err := os.Stat(filepath.Join(stage, name)); err != nil {
			return errors.New("Antigravity download is incomplete")
		}
	}
	if err := os.WriteFile(filepath.Join(stage, ".complete"), []byte(version), 0600); err != nil {
		return err
	}
	target := filepath.Join(parent, version)
	return promoteInstall(stage, target)
}

// Authenticate uses Google's own ACP authentication flow. The server opens the
// browser and stores credentials itself; CodeSaber never sees the tokens.
func AuthenticateAntigravity(ctx context.Context) error {
	p, err := Resolve("antigravity")
	if err != nil {
		return err
	}
	conn, err := Spawn(*p, ClientHandlers{})
	if err != nil {
		return err
	}
	defer conn.Close()
	result, err := conn.Request(ctx, MethodInitialize, map[string]any{"protocolVersion": ProtocolVersion, "clientInfo": map[string]string{"name": "codesaber", "version": "0.3.0"}, "clientCapabilities": map[string]any{}})
	if err != nil {
		return ExplainStartError(err)
	}
	m, _ := result.(map[string]any)
	methods, _ := m["authMethods"].([]any)
	for _, item := range methods {
		method, _ := item.(map[string]any)
		if method["id"] == "oauth-personal" {
			_, err = conn.Request(ctx, "authenticate", map[string]string{"methodId": "oauth-personal"})
			if err != nil {
				return ExplainStartError(err)
			}
			return nil
		}
	}
	return errors.New("The Antigravity server did not offer Google sign-in. Check your installed server version.")
}
