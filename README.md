# PromptStor

PromptStor is an open source command line tool that keeps the instructions of your coding agents in one library. It works with Claude Code, Codex, Gemini CLI, Antigravity, opencode and Cursor. It covers rules, skills, commands, subagents, permissions, hooks and MCP servers.

It imports what each installed tool already has, writes the library back in each tool's own format, backs up files before it changes them, and reports drift when a file no longer matches. It owns only the parts it wrote. Everything else in your configuration files stays as it is.

## Install

Download the archive for your system and `checksums.txt` from [GitHub releases](https://github.com/antonL95/promptstor/releases). There are builds for macOS and Linux (amd64 and arm64) and for Windows (amd64, as a zip). There is no Homebrew package in release 1.

```sh
version=1.0.0
archive="promptstor_${version}_darwin_arm64.tar.gz"
base="https://github.com/antonL95/promptstor/releases/download/v${version}"
curl -fLO "$base/$archive"
curl -fLO "$base/checksums.txt"
shasum -a 256 --ignore-missing -c checksums.txt
tar -xzf "$archive" promptstor
```

On Linux, use `sha256sum` in place of `shasum -a 256`. Continue only if the check prints `OK`.

Move `promptstor` to a permanent folder on your `PATH` before the first run. Edit guards, the MCP server entry and scheduled checks record where the binary is.

## First run

Start with a dry run. It asks the same questions and shows what would change, and it writes nothing.

```sh
promptstor doctor --dry-run
```

When the preview looks right, run it for real.

```sh
promptstor doctor
```

The first run finds your installed tools, asks which one is primary (its version wins when tools disagree) and which tools to keep in sync, and builds the library. It takes a snapshot before it changes anything, shows a preview and asks before applying it. Add `--project <dir>` to include a repository.

## Day to day

Edit items in `~/.config/promptstor/library/` (see [library format](docs/library-format.md)), then sync.

| Command | What it does |
|---------|--------------|
| `promptstor sync --dry-run` | Print the changes without writing them |
| `promptstor sync` | Write the library into every selected tool |
| `promptstor sync --project <dir>` | Also sync a repository |
| `promptstor doctor` | Report drift and ask how to resolve it |
| `promptstor doctor --check` | Report drift without asking. Exit code 0 is clean, 1 is drift, 2 is an error. Add `--json` for JSON. |
| `promptstor status` | Show the last check, the library hash and the selected tools |
| `promptstor check --ci` | In a repository, compare the committed files with `.promptstor/` (see [CI check](docs/ci.md)) |
| `promptstor restore --list` | List snapshots, newest first |
| `promptstor restore <snapshot>` | Put files back the way a snapshot recorded them |
| `promptstor schedule install` | Check for drift every hour and notify on changes. Also `schedule status` and `schedule remove`. |
| `promptstor mcp` | Serve the library to agents over MCP on standard input and output |
| `promptstor adapters` | List the supported tools (see [adapter reference](docs/adapters.md)) |
| `promptstor version` | Print the version |

The first run offers to register the MCP server in your tools, so an agent can list, search, read, change and remove items and ask for the drift report. A change to the personal layer is synced at once. A change to a project becomes a git branch for you to review.

## Backups and restore

PromptStor takes a snapshot on the first run and again before every change. If a snapshot fails, nothing is written.

```sh
promptstor restore --list
promptstor restore <snapshot> --dry-run
promptstor restore <snapshot>
```

- The first run prints its snapshot id. Restoring it puts your tool files back as they were. Your library and PromptStor settings stay.
- A restore makes folders match the snapshot, so files added to a backed up folder afterwards are removed. Run it with `--dry-run` first.
- Every restore takes a snapshot of its own and prints how to undo it.
- `--tool <id>` restores the files of one tool only.
- Release 1 does not delete old snapshots.

Snapshots keep your configuration files whole, including secrets already in them. Credential files are never copied. See [privacy](docs/privacy.md) for the list and for where data is kept.

## Good to know

- **Gemini CLI** rewrites its `settings.json` on every start and replaces a symlinked `settings.json` with a regular file.
- **Codex** rewrites every MCP server table in `config.toml` when you run `codex mcp add`. It follows symlinks when it writes.
- **Codex** skips a changed hook until you trust it again in `/hooks`.
- **Antigravity**'s importer copies other tools' MCP servers into `~/.gemini/config/plugins`, including secrets in plain text.
- **Antigravity** gets no edit guard in this release, because its hook must decide every call.
- **opencode** lets the last matching rule win, so a later rule of your own can allow what PromptStor denies. Creating `~/.config/opencode/AGENTS.md` also stops opencode from reading `~/.claude/CLAUDE.md` as a fallback.
- **Cursor** support is built from its documentation only and has not yet been checked against a real install.
- A deny that a tool cannot fully enforce waits for your acknowledgement, which `promptstor doctor` asks for. In a repository, commit `.promptstor/acks.json` to acknowledge it for the team (see [CI check](docs/ci.md)).
- Personal and device items are never written into project files.
- Symlinked configuration is reported and left alone. `promptstor sync --replace-symlink <path>` replaces a symlinked file with a regular one and takes it over.

## Release 1 limits

- Windows is best effort. It is not tested in CI, scheduled checks are not available there, and the GitHub Action supports Linux and macOS only.
- There is no sync between machines yet, and no accounts or teams.

## Development

The `devbox` form runs Go in a container. It is what the maintainer uses.

```sh
devbox run tools go test ./...
devbox run tools go vet ./...
devbox run tools go build ./cmd/promptstor
```

With Go 1.27 installed, run the same commands without `devbox run tools`. Regenerate the adapter reference after changing an adapter:

```sh
go run ./cmd/promptstor adapters --markdown > docs/adapters.md
```

In a devbox, redirect inside the container so that build output stays out of the file:

```sh
devbox run tools sh -c 'go run ./cmd/promptstor adapters --markdown > docs/adapters.md'
```

Contribution rules are in [AGENTS.md](AGENTS.md).

## Licence

MIT. See [LICENSE](LICENSE).
