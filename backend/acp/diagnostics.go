package acp

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Diagnostic struct {
	Ready    bool     `json:"ready"`
	Code     string   `json:"code"`
	Message  string   `json:"message"`
	Warnings []string `json:"warnings"`
	Model    string   `json:"model"`
}

// Check opens a real session without sending a model prompt or permitting tools.
func Check(ctx context.Context, root, name string) Diagnostic {
	d := Diagnostic{Warnings: configWarnings(name)}
	profile, err := Resolve(name)
	if err != nil {
		d.Code = "not-installed"
		d.Message = "Install this provider to continue."
		return d
	}
	conn, err := Spawn(*profile, ClientHandlers{})
	if err != nil {
		d.Code = "launch-failed"
		d.Message = "The provider could not start. Install the managed version, then retry."
		return d
	}
	defer conn.Close()
	if _, err = conn.Request(ctx, MethodInitialize, map[string]any{
		"protocolVersion": ProtocolVersion, "clientInfo": map[string]string{"name": "codesaber", "version": "0.3.0"}, "clientCapabilities": map[string]any{},
	}); err != nil {
		d.Code, d.Message = explainError(err)
		return d
	}
	res, err := conn.Request(ctx, MethodSessionNew, map[string]any{"cwd": root, "mcpServers": []any{}})
	if err != nil {
		d.Code, d.Message = explainError(err)
		return d
	}
	m, ok := res.(map[string]any)
	id, _ := m["sessionId"].(string)
	if !ok || id == "" {
		d.Code = "protocol-error"
		d.Message = "The provider returned an invalid session. Install the managed version and retry."
		return d
	}
	d.Ready = true
	d.Code = "connected"
	d.Message = "Session connected. No model request was sent; model access is checked when you send a message."
	if options, ok := m["configOptions"].([]any); ok {
		for _, item := range options {
			option, _ := item.(map[string]any)
			if option["category"] != "model" && option["id"] != "model" {
				continue
			}
			values, _ := option["options"].([]any)
			for _, v := range values {
				choice, _ := v.(map[string]any)
				if choice["value"] == option["currentValue"] {
					d.Model, _ = choice["name"].(string)
				}
			}
		}
	}
	return d
}

func explainError(err error) (string, string) {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout", "Connection timed out. Check your network, provider settings and sign-in, then retry."
	}
	text := strings.ToLower(err.Error())
	if strings.Contains(text, "unsupported client") || strings.Contains(text, "client is no longer supported") || (strings.Contains(text, "migrate") && strings.Contains(text, "antigravity")) {
		return "unsupported-client", "Google rejected this Gemini CLI connection. Choose Antigravity and install its official ACP server, then sign in there. Updating Gemini CLI alone may not resolve this account migration."
	}
	for _, fragment := range []string{"auth", "login", "log in", "sign in", "credential", "api key", "401", "403"} {
		if strings.Contains(text, fragment) {
			return "sign-in-required", "Sign in to this provider, or configure its API credentials, then check the connection again."
		}
	}
	return "connection-failed", "The provider could not create a session. Check sign-in and settings, or install the managed version."
}

func configWarnings(name string) []string {
	warnings := []string{}
	if name == "gemini" {
		warnings = append(warnings, "If Google asks you to migrate, choose the separate Antigravity provider. Gemini CLI and Antigravity use different connections.")
	}
	if name != "claude" || readOptions(name).Isolated {
		return warnings
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return warnings
	}
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		dir = filepath.Join(home, ".claude")
	}
	b, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil || len(b) > 1<<20 {
		return warnings
	}
	var settings struct {
		Env map[string]string `json:"env"`
	}
	if json.Unmarshal(b, &settings) != nil {
		return warnings
	}
	base := settings.Env["ANTHROPIC_BASE_URL"]
	if base != "" && os.Getenv("ANTHROPIC_BASE_URL") != "" && base != os.Getenv("ANTHROPIC_BASE_URL") {
		warnings = append(warnings, "Claude settings and the app environment specify different API endpoints. Separate settings can avoid this conflict while preserving your original settings.")
	}
	u, err := url.Parse(base)
	if err != nil {
		return warnings
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	if host == "localhost" || ip != nil && ip.IsLoopback() {
		port := u.Port()
		if port == "" {
			if u.Scheme == "https" {
				port = "443"
			} else {
				port = "80"
			}
		}
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 500*time.Millisecond)
		if err != nil {
			warnings = append(warnings, "Claude settings point to an unavailable local proxy. Start the proxy or use separate settings.")
		} else {
			conn.Close()
		}
	}
	return warnings
}

// ExplainStartError keeps raw provider stderr/credentials out of UI errors.
func ExplainStartError(err error) error { _, message := explainError(err); return errors.New(message) }
