# PromptStor

PromptStor is an open source Go CLI that keeps the instructions of every coding agent on a machine (Claude Code, Codex, Gemini CLI, Antigravity, opencode, Cursor) in one library. It imports each tool's rules, skills, commands, subagents, permissions, hooks and MCP servers, writes them back in each tool's native format, backs up everything before it changes anything, and reports drift.

Release 1 is local only: `doctor`, the library, the tool adapters, backups and restore, managed files, edit guards, a local MCP server, scheduled drift checks and a CI check. Sync, accounts and teams come later and are out of scope here.

These guidelines apply to every human and agent working in this repository. Where they conflict with a task description, these win; say so in your report.

## Safety rules

PromptStor edits other tools' configuration on real machines. A bug here destroys someone's setup, so these rules come first.

1. **Never write through a symlink.** Check the path and every parent below the home or project root (`fsx.LinkedAncestor`). A symlinked target is reported, never followed.
2. **Own units, not files.** PromptStor owns only the sections, keys, array elements or tables it wrote, as recorded in the manifest. Every byte outside an owned unit stays identical, including comments, formatting and key order.
3. **Never copy credentials.** Credential files and keys on the exclusion list in `internal/backup` are never read, stored, logged or restored. Never print a secret value, not even in a test failure.
4. **Snapshot before writing.** Every apply takes a snapshot first; if the snapshot fails, nothing is written.
5. **Report, never guess.** When a tool cannot express an item, or a format detail is unconfirmed, the adapter reports it. It never drops an item silently and never writes a format it has not verified.
6. **Tests never touch the real home.** Only `cmd/promptstor` builds `paths.Env` from the operating system. Code under `internal/` takes a `paths.Env` and never calls `os.UserHomeDir` or reads `HOME` or `XDG_*` (enforced by `internal/archguard`).
7. **The built binary runs on a real machine only as a dry run** (`promptstor doctor --dry-run`) until backup and restore are proven. Any run that writes to a real home needs the maintainer's explicit approval.

## Build and test

The maintainer's machine has no Go toolchain on the host. Every Go command runs in the devbox:

```
devbox run tools go test ./...
devbox run tools go vet ./...
devbox run tools go mod tidy
devbox run tools go test ./internal/<pkg>/ -run <TestName>
devbox run tools go test ./internal/<pkg>/ -update
devbox run tools env GOOS=darwin GOARCH=arm64 go build -o dist/promptstor ./cmd/promptstor
```

Never install Go, golangci-lint or any other toolchain on the host. The container mounts `.git`, `.claude`, `AGENTS.md` and `CLAUDE.md` read only, so edit those on the host. Contributors with a local Go 1.27 toolchain run the same commands without the `devbox run tools` prefix.

CI runs `go vet`, `go test` and golangci-lint on GitHub hosted runners (`ubuntu-latest`, `macos-latest`) only.

## Code

- Go 1.27, `CGO_ENABLED=0`, one static binary. darwin and linux are fully supported, windows is best effort.
- **No code comments.** No `//` prose, no doc comments, no section banners, no TODO or FIXME, no commented out code. If code needs a comment, rename it or split it. The only allowed comments are tool directives: `//go:build`, `//go:generate`, `//go:embed`, `//nolint:<linter>` naming a specific linter, and generated code markers. `internal/archguard` enforces this.
- Standard library first. The allowed dependencies are cobra, `tailscale/hujson`, `go.yaml.in/yaml/v3`, `pelletier/go-toml/v2` (reading only) and `modelcontextprotocol/go-sdk`. Adding any other dependency needs a reason in the commit message.
- Wrap errors with the file or item they concern: `fmt.Errorf("read %s: %w", path, err)`. No panics outside `main`.
- Small interfaces, defined by the package that consumes them. No package level mutable state except the adapter registry.
- Output that users or tests compare is deterministic: sort maps and lists before printing or writing.
- State files are written with mode 0600 and state directories with 0700.
- Package names say what the package does. Keep one responsibility per file, and split a file before it grows hard to read.

## Package map

| Package | Responsibility |
|---|---|
| `cmd/promptstor` | Entry point; the only place that reads the real environment |
| `internal/cli` | Cobra commands |
| `internal/paths` | `paths.Env` and every location PromptStor uses |
| `internal/fsx` | Atomic writes, symlink checks, the run lock |
| `internal/testenv` | Fixture homes, golden files, tree hashes for tests |
| `internal/library` | Library items, layer files, merge and locks, built in items |
| `internal/managed` | Managed Markdown sections and the notice |
| `internal/manifest` | Ownership manifest and unit hashing |
| `internal/structedit` | Format preserving JSON, JSONC and TOML edits |
| `internal/plan` | Desired units, diff, apply |
| `internal/adapter` | Adapter contract and registry; one subpackage per tool |
| `internal/mdimport` | Import resolution per tool syntax |
| `internal/backup` | Snapshots, restore, credential exclusions |
| `internal/policy` | Permission gate and acknowledgements |
| `internal/secrets` | MCP secret references and resolution |
| `internal/config`, `internal/engine` | Configuration and the sync engine |
| `internal/drift`, `internal/doctor` | Drift reports and the doctor command |
| `internal/guard` | Edit guards for each tool |
| `internal/mcpserver` | Local MCP server |
| `internal/ci`, `internal/schedule`, `internal/notify` | CI check, scheduled checks, notifications |
| `internal/archguard` | Tests that enforce these guidelines |

Packages appear as release 1 lands; this table is the target layout.

## Tests

- Test first: write the failing test, see it fail for the expected reason, then implement.
- Tests run on copies of fixtures in `t.TempDir()` through `testenv.Home`. No test reads the real home, uses the network or depends on the clock without injecting `Now`.
- Golden files live next to the test in `testdata/golden/` and are refreshed with `-update`. Review every golden diff before committing it.
- Fixtures are synthetic. They are shaped like real tool files, but no real path, username, project name, MCP server name, account or secret value may appear in them. This repository is public.
- Table driven tests where the cases share one shape. Test names say the behaviour: `TestDiffBlocksSymlinkedParentDir`.

## Adapters

Each tool adapter lives in `internal/adapter/<tool>/` and has:

- `testdata/SOURCES.md` listing every file location and format it relies on, with the vendor doc or source URL and the date checked;
- synthetic fixtures for user and project scope;
- a capability table, and import, export and round trip tests.

A tool's formats change often. When a format detail cannot be confirmed from vendor docs or source, the adapter reports it instead of writing it.

## Documentation

- README and files under `docs/` never use dashes (— or -) as punctuation in sentences. Use periods, commas or parentheses. List bullets are fine.
- `docs/adapters.md` is generated by `promptstor adapters --markdown`; never edit it by hand.

## Git

- Conventional commit subjects: `feat:`, `fix:`, `test:`, `refactor:`, `docs:`, `chore:`. One task or one logical change per commit.
- No AI attribution anywhere: no `Co-Authored-By` trailers for AI tools, no "Generated with" footers, no mention of an AI tool in commits, pull requests or issues.
- Never push, open a pull request or tag a release without the maintainer's go ahead.
- `.superpowers/`, `solo.yml` and `.zed/` are local only and excluded from git through `.git/info/exclude`.

## Done means

- `go vet`, `go test ./...` and golangci-lint pass in the devbox.
- `gofmt` and `goimports` leave no diff.
- No comments other than allowed directives.
- New behaviour has tests, and golden diffs were reviewed.
- Docs touched by the change are updated.
