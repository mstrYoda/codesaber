# Windows support validation

## Candidate and scope

Prepared on 2026-09-16 against upstream `ee598de1e88d6b0fd64ab1d4614e6758ee48738f`.
The current upstream Vim mode and PHP highlighting/indexing changes are retained.

The contribution adds Windows terminal/process handling, clipboard support,
platform shortcuts/path handling, provider setup, ACP model selection and Windows
release artifacts. Provider setup offers Claude, Antigravity and OpenCode; Gemini
CLI is no longer offered. OpenCode Go and Zen keep distinct provider/model IDs.
New sessions use the provider's default model; model choices are not account or
quota verification.

## Local checks on the integrated candidate

- Clean `npm ci`: passed, no reported dependency vulnerabilities.
- Frontend: 55 tests in 8 files passed, including upstream Vim and PHP tests.
- TypeScript and Vite production build: passed.
- `go test ./... -timeout 120s` on Windows x64: passed.
- `go vet ./...` on Windows x64: passed.
- Wails production Windows x64 executable build: passed.
- Per-user NSIS package build: passed, using the existing Microsoft-signed
  WebView2 bootstrapper after verifying its Authenticode signature. This package
  has not undergone another install/uninstall cycle.
- `actionlint` 1.7.7 for CI and release workflows: passed (external shellcheck
  and pyflakes checks disabled).
- Linux amd64 test binaries, cross-compiled on Windows and executed in Ubuntu
  WSL: ACP, terminal, Git, project, settings, editor, agentstore, fswatch and
  search passed. LSP also passed after providing a private Linux Go 1.25.14
  toolchain (official archive SHA256 verified) to build its test server and
  using the package working directory. All ten engine packages passed.
  This exercises Linux code, including a real Unix PTY; it is
  not a full Linux desktop build or GUI acceptance test.

## Earlier Windows acceptance

Before integrating the newer upstream changes, the Windows application was used
for editing, terminal, clipboard, Git, LSP and conversation/history workflows.
Claude/MiniMax and Antigravity produced actual replies. Model changes were
confirmed by real Claude, Antigravity and OpenCode sessions without inference.
The native UI also switched between OpenCode Go and Zen and restored the original
model. These checks do not establish access to every advertised model.

The user completed manual acceptance of that Windows build. The newer upstream
integration was checked with the automated tests/build above; it has not received
another full manual acceptance pass.

An isolated per-user NSIS install/uninstall was tested on the existing Windows
computer before this integration. No clean Windows VM was used.

## CI and remaining limits

The PR workflow now runs frontend tests/build, Go vet/tests and a native desktop
compile on Ubuntu, macOS and Windows. Linux installs GTK4 and WebKitGTK 6 build
dependencies. A failing Unix matrix job does not cancel the other platform.
These jobs have been configured and statically checked, but have not run remotely
for this candidate yet. A first-time fork contribution may require a maintainer
to approve the workflow run.

macOS runtime/GUI, Linux GUI, clean Windows VM and Windows ARM64 remain untested.
Managed Antigravity installation currently supports Windows x64 only. Interactive
provider login, account entitlement and external MCP availability depend on the
user's setup; automated CI does not authenticate to paid providers. The Unix
sign-in helper asks the user to sign in through the provider CLI.

Existing upstream placeholder features remain outside this contribution's scope.
Existing build warnings about tree-sitter browser imports/eval and bundle size
remain; they did not fail the production build.
