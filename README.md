# CodeSaber ⚔️

> It's like a lightsaber, but for developers.

CodeSaber is a lightweight, AI-native IDE built with **Wails v3 + Go + React + CodeMirror 6**. A modular monolith Go backend powers the editor, git, terminal, and AI agent engines — one tool to ignite your workflow.

<!-- TODO: hero screenshot here -->
![CodeSaber hero](docs/screenshots/screenshot-1789307667.png)

## 📦 Install

Grab a build from the [releases page](https://github.com/mstrYoda/codesaber/releases).

### macOS
```
brew install mstrYoda/codesaber/codesaber
```
Prefer a direct download? Grab the `.dmg` for your architecture from the releases page. The app is unsigned, so macOS may warn on first launch — right-click the app and choose **Open** to run it.

### Linux
Download an artifact from the [releases page](https://github.com/mstrYoda/codesaber/releases):

- **AppImage** (amd64): `chmod +x CodeSaber-*.AppImage && ./CodeSaber-*.AppImage`
- **Debian/Ubuntu**: `sudo dpkg -i codesaber_*.deb` (fix missing deps with `sudo apt -f install`)
- **Fedora/RHEL**: `sudo dnf install codesaber_*.rpm`
- **Tarball**: extract and run the included binary

### Windows

Windows 10 (1809+) or Windows 11, with Microsoft Edge WebView2 Runtime, is required.
Windows release builds are distributed as `CodeSaber-<version>-windows-amd64.zip`:
extract the archive and run `codesaber.exe`. Until a release containing Windows
support is published, build from source using the commands below.

The terminal uses native ConPTY. Its default shell is PowerShell 7 (`pwsh.exe`),
then Windows PowerShell, then `COMSPEC` / `cmd.exe`. A shell executable can be
selected in Settings. WSL is not required.

## ✨ Features

### 🗂 Editor Core
- **Multi-project workspace** — add/remove projects in the sidebar, each with its own state namespace
- **Full editor experience** — per-project tabs, syntax highlighting (Go, TypeScript, CSS, HTML, JSON, Markdown with preview), minimap, bracket pair colorization, breadcrumbs
- **Welcome window** — recent projects with branch + last-used info
- **Atomic save + external-change reconciliation** — auto-reload or banner when files change outside
- **Quick-open (`⌘P`)** — fuzzy file search
- **Collapsible, drag-resizable panels** (`⌘B` sidebar / `⌘J` terminal / `⌘D` dock) with persisted sizes

### 🔀 Git Panel
- Real-time status via **fsnotify** — never press refresh again
- Stage/unstage per file or per hunk, with `+/−` line stats
- Commit & Push workflow with **AI-generated commit messages**
- Inline diff viewer, branch switcher, History tab with recent commits
- Upstream sync indicators (`↑↓`), Fetch/Pull/Push actions

### 🤖 ACP AI Agent
Built on the **Agent Client Protocol (ACP)** — bring your own agent harness:
- **opencode** and **Claude Code** supported (verified), more to come
- Streaming chat, tool-call display, per-project sessions persisted across restarts
- **Side-by-side diff review** — accept or reject agent file edits and inline prompt proposals
- Crash-safe: harness dies → auto-respawn, chat survives
- **`⌘K` inline edit-at-cursor** and prompt history (↑/↓ recall)

### 🧠 Language Intelligence
- **gopls-powered LSP** — go-to-definition, hover, diagnostics gutter, document symbols
- Framework is server-agnostic; TS/Rust/Java grammar configs in the roadmap
- **`⌘T` symbol search** — web-tree-sitter (WASM) symbol index per project, invalidated on save/filesystem change

### 💻 Real Terminal
- True PTY sessions (`creack/pty` on Unix, ConPTY on Windows) per tab, `cwd` = project root
- **xterm.js** frontend with streaming over an event channel and resize propagation

### ⚙️ Performance & Polish
- **Perf HUD (`⌘⇧H`)** — live RSS memory, per-engine latency counters, terminal count, error stats
- Custom macOS titlebar with traffic-light inset
- Status bar: cursor position (Ln/Col), diagnostic counters, indentation, encoding, language
- **Settings UI** — editor/terminal/search preferences, persisted
- Engine isolation: goroutines with panic recovery and restart scaffolding

## 🚀 Usage Examples

**Open a project**
```
wails3 dev          # development mode with hot-reload
wails3 build        # production build
```

**Daily workflow cheat sheet**

| Action | Shortcut |
|---|---|
| Quick-open file | `⌘P` |
| Symbol search | `⌘T` (`sym:` prefix in quick-open) |
| Command palette | `⌘⇧P` |
| Inline AI edit | `⌘K` |
| Toggle sidebar / terminal / dock | `⌘B` / `⌘J` / `⌘D` |
| Perf HUD | `⌘⇧H` |
| Close tab | `⌘W` |

**Ask the agent to change code, then review:**
1. Open the Agent tab and chat with opencode or Claude Code
2. Proposed edits appear as diff cards — accept or reject side-by-side
3. The AI commit message generator picks it up for your Git panel commit

**Ship faster with git + AI:**
1. Git tab shows live changes the moment you save
2. Stage files, click **Generate commit message** (AI, via your running harness)
3. Commit & Push — done

## 🗺 Roadmap

- Light "Milk" theme
- tree-sitter text search replacing index walk
- SSH remote development · DAP debugging · more LSP servers
- New Project… / Clone Repo… on the welcome screen
- Multi-window per project, detachable agent window

See [`docs/backlog.md`](docs/backlog.md) for the full backlog and status.

## 🏗 Development

```
wails3 task generate:bindings   # regenerate TS bindings (always -ts!)
wails3 dev
```

> Bindings note: always use `wails3 task generate:bindings` — plain `wails3 generate bindings` emits `.js` and clobbers the committed `.ts` bindings.

### Build and test on Windows

Install Git, Go (the version in `go.mod` or newer), Node.js 22+, and the
[WebView2 Runtime](https://developer.microsoft.com/en-us/microsoft-edge/webview2/).
From PowerShell in the repository root:

```powershell
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.20
$env:Path = "$(go env GOPATH)\bin;$env:Path"
npm --prefix frontend ci
npm --prefix frontend test
npm --prefix frontend run build
go test ./... -timeout 120s
wails3 task windows:build ARCH=amd64
.\bin\codesaber.exe
```

Windows shortcuts use Ctrl in place of Cmd. ConPTY tests run on Windows CI
and cover shell I/O, resizing, exit status, and working directories containing
spaces and Unicode characters. Windows CI also builds an executable artifact.

PR checks run frontend tests/build, Go vet/tests and a desktop compile on Windows,
macOS and Linux. Automated builds do not replace GUI acceptance on each platform.
See [Windows support validation](docs/windows-support-validation.md) for local
test evidence and the remaining platform limitations.

The Windows clipboard reader supports copied files/folders and PNG images
through the built-in Windows PowerShell STA clipboard APIs. File sources and
paste targets must belong to open projects.

### Set up an AI provider on Windows

Open **Agent**, choose a provider, and click **Install managed version**.
CodeSaber downloads a pinned adapter into `%APPDATA%\codesaber\agents`;
it does not replace your global CLI installation. For Node-based adapters,
CodeSaber uses Node.js 22+ with npm or downloads a private Windows runtime.
Use **Check connection**, **Sign in** if needed, then **Start**.
Checking a connection opens an ACP session without sending a model prompt;
it does not prove that your account can access every model.

- **Claude** uses the Claude Agent ACP adapter. An installed `claude` command
  alone is not an ACP server. Existing API environment variables are inherited.
- **Antigravity (Google)** uses Google's official ACP server, currently available
  through managed setup on Windows x64 (approximately 468 MB download).
  **Sign in** opens Google's browser flow. The Antigravity desktop application's
  login is not automatically transferred. Complete account selection yourself.
- **OpenCode** uses its built-in ACP server and your configured provider.
  This includes **OpenCode Go** (subscription) and **OpenCode Zen** (usage-based
  billing, with some free models). Choose the appropriate connection in **Sign in**,
  then start a new session. The model selector shows the connection and model ID:
  `opencode-go/...` uses Go, while `opencode/...` uses Zen. Selecting a Zen model
  does not use your Go subscription. CodeSaber does not purchase plans or check balances.

**Repair installation** downloads a fresh copy. Failed replacement restores the
previous installation. **Cancel setup** cancels an in-progress download/install.
For conflicting CLI settings, **Use separate provider settings** selects an
app-owned configuration directory; API environment variables still apply and
a separate sign-in may be required. Managed binaries can also be placed in a
portable/test directory through `CODESABER_AGENT_HOME`.

After **Start**, use the **Model** selector above the conversation. It lists the
models advertised by the current provider session and applies your choice through
ACP. Model changes are disabled while a response is running. Failed changes leave
the last confirmed selection visible; use refresh to recheck the provider state.
The provider's default applies to each new session. Listing a model does not prove
account access or available quota. Gemini CLI is no longer offered as a provider;
use Antigravity for the Google connection.

Pinned versions: Claude adapter 0.77.0, OpenCode 1.18.31,
Antigravity ACP 1.1.1, private Node.js 24.21.0. Provider credentials, subscriptions,
model availability, and service restrictions remain provider requirements.

Agent startup and model access are separate checks. Optional local integration
tests (PowerShell) are:

```powershell
$env:CODESABER_TEST_GOPLS = '1'; go test ./backend/lsp -run TestRealGoplsWindowsPaths -v
$env:CODESABER_TEST_AGENT = 'opencode'; go test ./backend/acp -run TestRealAgentHandshake -v
```

The agent test opens an ACP session without sending a model prompt. Clipboard
integration tests require exclusive clipboard access; set
`CODESABER_TEST_CLIPBOARD=1` and run `TestWindowsClipboardFormats` in `./backend`.
The test restores the original clipboard after testing files and images.

## 📜 License

<!-- TODO: add license -->
