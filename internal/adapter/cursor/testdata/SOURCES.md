# Cursor adapter sources

Checked on 2026-11-27 against the vendor docs only. Cursor was not installed on the machine and no source is published. Every page under `https://cursor.com/` is also served as Markdown by appending `.md`. Paths under `https://cursor.com/docs/` are written as `D/`.

| Short name | URL |
|---|---|
| RULES | D/rules, https://cursor.com/help/customization/rules |
| PLUGINREF | D/reference/plugins |
| CUSTOMIZE | D/customize-cursor |
| SKILLS | D/skills |
| AGENTS | D/subagents |
| MCP | D/mcp |
| CLIPERM | D/cli/reference/permissions |
| CLICONFIG | D/cli/reference/configuration |
| EDITORPERM | D/reference/permissions, D/agent/security/run-modes |
| HOOKS | D/hooks |
| GLOBFORUM | https://forum.cursor.com/t/glob-pattern-rules-are-never-respected-by-agent/160133, https://forum.cursor.com/t/rule-frontmatter-format/146274 (community reports, not documentation) |

## Locations

| What | User scope | Project scope | Source |
|---|---|---|---|
| Rules | account data, never written | `.cursor/rules/**/*.mdc`; shared rules come from `AGENTS.md` | RULES, CUSTOMIZE |
| Commands | `~/.cursor/commands/` | `.cursor/commands/` | CUSTOMIZE, https://cursor.com/changelog/1-6 |
| Skills | `~/.agents/skills/` written; `~/.cursor/skills/` also imported | `.agents/skills/` written; `.cursor/skills/` also imported | SKILLS |
| Subagents | `~/.cursor/agents/` (editor only, the CLI ignores it) | `.cursor/agents/` | AGENTS, CUSTOMIZE |
| MCP servers | `~/.cursor/mcp.json` | `.cursor/mcp.json` | MCP |
| CLI permissions | `/permissions/deny` in `~/.cursor/cli-config.json` | `/permissions/deny` in `.cursor/cli.json` | CLIPERM, CLICONFIG |
| Hooks | `~/.cursor/hooks.json` | `.cursor/hooks.json` | HOOKS |
| Noted, not imported | `~/.cursor/rules/`, `~/.cursor/permissions.json` | `.cursorrules`, `.cursor/permissions.json`, `.md` files in `.cursor/rules` | RULES, EDITORPERM |
| Never read | CLI and MCP sign in storage (location not documented) | | research |

## Formats

- Rules (RULES): frontmatter is `description`, `globs`, `alwaysApply`. `globs` is one comma separated value and is written unquoted, as in the docs example `globs: docs/**/*.md, docs/**/*.mdx`, so a pattern that starts with `*` is not strict YAML. The adapter therefore reads the `globs` line itself and the other fields as YAML. A plain `.md` file in the folder is ignored by Cursor.
- Rule types (RULES): `alwaysApply: true` is always, `globs` is attached by file, a `description` alone is chosen by the agent, neither is manual. Manual rules have no PromptStor activation and are not imported.
- Commands (CUSTOMIZE, PLUGINREF): the Markdown body is the prompt.
- Skills (SKILLS): `name` is lowercase letters, digits and hyphens and equals the folder name. `description` is required.
- Subagents (AGENTS): `name`, `description`, `model`, `readonly`, `is_background`. There is no tools field.
- MCP (MCP): stdio is `type: "stdio"`, `command`, `args`, `env`, `envFile`; remote is `url`, `headers`, `auth`, and SSE or streamable HTTP is inferred. `${env:NAME}` is expanded in `command`, `args`, `env`, `url`, `headers`. `envFile`, and a `type` already on a remote server, are kept. A stdio server written without `type` stays without it. A server with `auth` or an unknown field is reported and left alone.
- Other variables (MCP): `${userHome}`, `${workspaceFolder}`, `${workspaceFolderBasename}`, `${pathSeparator}`, `${/}`. A server that uses one is reported on import, because the library would read it as an environment variable.
- CLI permissions (CLIPERM, CLICONFIG): `Shell(commandBase)`, `Read(pathOrGlob)`, `Write(pathOrGlob)` with `**`, `*`, `?`. Deny beats allow. `commandBase` is the first word of the command line. The file is pure JSON, the CLI writes its own state into it and repairs missing fields.
- Editor permissions (EDITORPERM): allow lists and classifier hints only, no deny. Every deny therefore needs an acknowledgement.
- Hooks (HOOKS): `{"version":1,"hooks":{"<event>":[{...}]}}`. Handler fields are `command`, `type`, `timeout`, `matcher`, `failClosed`, `loop_limit`. Only command handlers are written or imported.

## Resolved during this task

| Research item | Resolution |
|---|---|
| 1. YAML list `globs` | Not relied on. Only the comma string is written. A list is reported on import. GLOBFORUM reports that lists and quoted values do not match. |
| 2. `.cursor/rules` inside package folders | Not scanned. Only the project root folder and its subfolders. |
| 3. `~/.cursor/rules` in editor and CLI | CUSTOMIZE documents it only for self hosted workers. Never written, noted on import. |
| 4. Nested and user `CLAUDE.md` | Not relied on. Only the project root file is assumed, for the "Cursor also reads CLAUDE.md" report. |
| 5. Command frontmatter | No frontmatter is written. Dropped fields are reported. `description` is read on import, other keys are noted. Only top level `.md` files are read. |
| 6. Skill name precedence | Not relied on. The first folder found wins on import and the other is noted. |
| 7. MCP enable state and OAuth storage | Neither read nor written. |
| 8. CLI permission merge order | Not relied on. Only denies are written and deny beats allow. |
| 9. IDE allow list storage | Not read. |
| 10. `.cursorindexingignore` | Not used. |
| 11. `preToolUse` covering every edit | Belongs to the edit guard task. The adapter only writes the hooks it is given. |
| 12. Cloud agents and `.cursor/rules` | Not relied on. |
| 13. CLI credential storage | Not read. |

## Still unconfirmed

- Whether a quoted `description` is unquoted by Cursor. The docs show one quoted example. PromptStor quotes only when YAML requires it.
- How `command:args` patterns match arguments with spaces. A deny of more than one word is reported as blocking and not written.
- Whether Cursor expands `~` in permission paths. A home path is written in full at user scope and refused at project scope.
- Where the CLI looks when `CURSOR_CONFIG_DIR` is set, or on Linux with a moved `XDG_CONFIG_HOME`. The docs name both overrides, so user denies are then reported and not written.
