# Library format

The library is a set of plain text files. Each item is one rule, skill, command, subagent, permission, hook or MCP server.

## Layers

| Layer | Folder | Notes |
|-------|--------|-------|
| company | none | Release 1 reads no company folder. PromptStor uses this layer for its own edit guards and MCP server entry. |
| team | none | Release 1 reads no team folder. |
| project | `<repository>/.promptstor/` | Committed with the repository |
| personal | `~/.config/promptstor/library/` | Yours, on this machine |
| device | `~/.config/promptstor/device/` | For settings that belong to this machine only |

`XDG_CONFIG_HOME` moves `~/.config`. On Windows the folders are under `%APPDATA%\promptstor`.

The order is company, team, project, personal, device. A later layer replaces an item with the same id from an earlier one.

Scopes decide which layers reach which files.

- User files (in your home folder) use company, team, personal and device.
- Project files (in a repository) use company, team and project.

Personal and device items are therefore never written into a repository.

## Layout

Every layer folder has the same layout.

| Kind | Main file | Extra files |
|------|-----------|-------------|
| rule | `rules/<name>.md` | none |
| skill | `skills/<name>/SKILL.md` | anything else in `skills/<name>/` |
| command | `commands/<name>.md` | none |
| subagent | `subagents/<name>.md` | none |
| permission | `permissions/<name>.yaml` | none |
| hook | `hooks/<name>.yaml` | anything in `hooks/<name>/` |
| mcp | `mcp/<name>.yaml` | none |

- Entries whose names start with a dot are ignored.
- A symlink inside a layer is an error. PromptStor does not follow it.
- Skill and command names must match `^[a-z0-9][a-z0-9-]{0,63}$`. A skill name must equal its folder name.

## Common keys

Rules, skills, commands and subagents are Markdown with YAML frontmatter. The text after the frontmatter is the body. Permissions, hooks and MCP servers are YAML files without a body.

| Key | Meaning |
|-----|---------|
| `name` | The item name. Defaults to the file or folder name. |
| `description` | A short description |
| `promptstor` | The block described below |

In Markdown items, any other frontmatter key is kept with the item. What reaches a tool depends on the kind and the tool. In YAML items an unknown key is an error.

## The promptstor block

| Key | Meaning |
|-----|---------|
| `id` | Required. A unique id for the item. |
| `order` | A number. Lower values come first among items of one kind. |
| `targets` | `include` and `exclude` lists of tool ids. An empty `include` means every selected tool. |
| `scope` | A list of file globs a rule applies to |
| `activation` | `always`, `files` or `agent`. A rule with a `scope` counts as `files` unless it says `agent`. |
| `locked` | Stops later layers from replacing the item. It counts only in the company and team layers, so it has no effect in release 1 folders. |
| `removed` | Hides the item with the same id from an earlier layer |

An unknown key in the block is an error.

PromptStor assigns an id when it imports an item or creates one through its MCP server. When you write a file by hand, add the id yourself. PromptStor's own ids are `ps_` followed by 26 characters. Ids that start with `ps_builtin_` are reserved.

The tool ids are `claude-code`, `codex`, `gemini-cli`, `antigravity`, `opencode`, `cursor` and `agents-md`. What each tool can express is listed in [adapters.md](adapters.md). PromptStor reports what a tool cannot express.

## Examples

A rule, in `rules/code-style.md`:

```markdown
---
description: How Go files are written
promptstor:
  id: ps_00000000000000000000000001
  order: 10
  targets:
    exclude:
      - cursor
  scope:
    - '**/*.go'
  activation: files
---

# Code style

Keep functions short.
```

A permission, in `permissions/no-force-push.yaml`:

```yaml
promptstor:
  id: ps_00000000000000000000000002
description: Block destructive commands
deny_commands:
  - git push --force
deny_paths:
  - '**/*.pem'
allow_mcp_servers:
  - example-docs
native:
  claude-code:
    ask:
      - Bash(rm:*)
```

`deny_commands`, `deny_paths` and `allow_mcp_servers` are portable. PromptStor translates them for each tool that can express them. `native` holds settings in one tool's own shape, under its tool id.

A hook, in `hooks/format-check.yaml`:

```yaml
promptstor:
  id: ps_00000000000000000000000003
native:
  claude-code:
    event: PostToolUse
    matcher: Edit|Write
    hooks:
      - type: command
        command: gofmt -l .
```

Hooks have no portable form. Write one `native` entry for every tool that should run the hook, using that tool's event names and fields.

## MCP servers

In `mcp/example-docs.yaml`:

```yaml
promptstor:
  id: ps_00000000000000000000000004
transport: stdio
command: example-server
args:
  - --port
  - "8080"
env:
  EXAMPLE_MODE: readonly
  EXAMPLE_TOKEN:
    ref: env:EXAMPLE_TOKEN
```

| Key | Meaning |
|-----|---------|
| `transport` | `stdio` or `http` |
| `command`, `args` | The program of a `stdio` server |
| `url`, `headers` | The address and headers of an `http` server |
| `env` | Environment variables for the server |
| `resolve` | Allows PromptStor to read the secret and write its value, for tools where that is possible |

A value in `env` or `headers` is either a string or a reference.

| Reference | Meaning |
|-----------|---------|
| `ref: env:NAME` | The environment variable `NAME` |
| `ref: keychain:<service>/<account>` | An entry in the macOS Keychain or the Linux secret service |
| `ref: needs-setup:NAME` | A secret you still have to set up by hand |

Inside `command`, `args`, `url` and string values you can write `${env:NAME}`.

- A tool with its own placeholder syntax receives that placeholder, never the value.
- A keychain reference is read only when the item says `resolve: true`, or when `resolve_secrets` in `config.yaml` names the server. Its value is then written into the tool's file.
- The same setting lets PromptStor write the value of an environment reference for a tool with no placeholder syntax, which is Antigravity.
- Codex never receives a resolved value. PromptStor reports a server with this setting instead of writing it there.
- When a tool cannot use a reference, PromptStor does not write the server there and reports that it needs manual setup.
- A plain string under a secret looking name (such as one containing `token`, `key`, `secret` or `password`) is never written. Use a reference.

## Repository settings

A project layer also holds `config.yaml`, which lists the tools whose files the repository commits, and optionally `acks.json`. Both are described in [ci.md](ci.md).
