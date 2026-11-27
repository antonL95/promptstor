# Checking a repository in CI

A repository keeps its project layer as plain text in `.promptstor/`. The CI check generates the project files from that folder alone and fails when the committed files differ. It needs no keys, no home folder and no manifest, so it runs in any CI.

## Folder layout

```
.promptstor/
  config.yaml
  rules/<name>.md
  skills/<name>/SKILL.md
  commands/<name>.md
  subagents/<name>.md
  permissions/<name>.yaml
  hooks/<name>.yaml
  mcp/<name>.yaml
```

`config.yaml` lists the tools whose files the repository commits.

```yaml
tools:
  - claude-code
  - codex
  - cursor
```

The tool IDs are `claude-code`, `codex`, `gemini-cli`, `antigravity`, `opencode`, `cursor` and `agents-md`. `AGENTS.md` is checked automatically when the other tools need it.

Every item needs its `promptstor` id. The check never writes to the repository, so it does not assign missing ids.

A secret in an MCP server must be an environment reference such as `ref: env:EXAMPLE_TOKEN`. The generated files then hold a placeholder and never the value.

## The command

```sh
promptstor check --ci
promptstor check --ci --dir path/to/repository
```

| Exit code | Meaning |
|-----------|---------|
| 0 | Every generated unit matches the committed files |
| 1 | A unit is missing or differs, a file cannot be compared, or a tool reports a blocking limitation |
| 2 | The check could not run, for example because `.promptstor/config.yaml` is missing or names an unknown tool |

For each difference the command prints a unified diff from the committed content to the generated content. For JSON and TOML entries it prints only the generated entry, because the committed one might hold a secret.

Lines that start with `note:` describe items a tool cannot fully express. They do not fail the check. Lines that start with `blocked:` do. Acknowledgements are stored on your machine, not in the repository, so a blocking limitation fails in CI even after you have acknowledged it locally.

## What is compared

Only the units PromptStor generates are compared: its managed sections, the files it writes as a whole, and its own keys and tables in shared configuration files. Text outside those units is ignored.

The check has no manifest, so it cannot tell that a file or section is left over from an item you have since removed from `.promptstor/`. Review those removals in the pull request.

A symlinked target is reported and never followed.

## GitHub Action

The repository root holds a composite action. It downloads the release for the runner, verifies its checksum and runs the check.

| Input | Required | Default | Meaning |
|-------|----------|---------|---------|
| `version` | yes | | The release to run, for example `v1.0.0` |
| `working-directory` | no | `.` | The folder that holds `.promptstor/` |

It supports Linux and macOS runners on x64 and ARM64.

```yaml
name: PromptStor

on:
  pull_request:
  push:
    branches: [main]

permissions:
  contents: read

jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: antonL95/promptstor@v1.0.0
        with:
          version: v1.0.0
```

## Release archive names

The action depends on these names, so the release configuration must produce exactly them. `<version>` is the tag without its leading `v`.

| Asset | Runner |
|-------|--------|
| `promptstor_<version>_linux_amd64.tar.gz` | Linux x64 |
| `promptstor_<version>_linux_arm64.tar.gz` | Linux ARM64 |
| `promptstor_<version>_darwin_amd64.tar.gz` | macOS x64 |
| `promptstor_<version>_darwin_arm64.tar.gz` | macOS ARM64 |
| `checksums.txt` | All, with one `<sha256>  <asset name>` line per archive |

Each archive holds the `promptstor` binary at its root. All assets are attached to the GitHub release for the tag `v<version>`.
