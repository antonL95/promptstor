# Claude Code adapter sources

Checked on 2026-11-27 against code.claude.com/docs (current pages, versions up to v2.1.319).

| Short name | URL |
|---|---|
| MEM | https://code.claude.com/docs/en/memory |
| SK | https://code.claude.com/docs/en/skills |
| SUB | https://code.claude.com/docs/en/sub-agents |
| PERM | https://code.claude.com/docs/en/permissions |
| SET | https://code.claude.com/docs/en/settings |
| HOOK | https://code.claude.com/docs/en/hooks |
| MCP | https://code.claude.com/docs/en/mcp |
| DIR | https://code.claude.com/docs/en/claude-directory |
| BOOST | https://laravel.com/docs/13.x/boost and https://github.com/laravel/boost/blob/main/src/Install/Agents/ClaudeCode.php |
| BOOSTCFG | https://raw.githubusercontent.com/laravel/boost/main/src/Support/Config.php and https://raw.githubusercontent.com/laravel/boost/main/src/Console/InstallCommand.php |

## Locations

| What | User scope | Project scope | Source |
|---|---|---|---|
| Instructions | `~/.claude/CLAUDE.md` | `CLAUDE.md`, also read from `.claude/CLAUDE.md` | MEM |
| AGENTS.md | not read | read only when no CLAUDE.md exists | MEM |
| Rules | `~/.claude/rules/**/*.md` | `.claude/rules/**/*.md` | MEM, DIR |
| Skills | `~/.claude/skills/<name>/SKILL.md` | `.claude/skills/<name>/SKILL.md` | SK |
| Synced skills | `~/.claude/skills/synced/<bucket>/<skill>/` (the bucket level is observed, not documented) | none | SK |
| Commands | `~/.claude/commands/**/*.md` | `.claude/commands/**/*.md` | SK, DIR |
| Subagents | `~/.claude/agents/*.md` | `.claude/agents/*.md` | SUB |
| Permissions and hooks | `~/.claude/settings.json` | `.claude/settings.json` | SET, PERM, HOOK |
| Runtime grants | none | `.claude/settings.local.json` | PERM, SET |
| MCP servers | `~/.claude.json`, top level `mcpServers` | `.mcp.json`, `mcpServers` | MCP |
| Local scope servers | `~/.claude.json`, `projects.<path>.mcpServers` | none | MCP |
| Plugins | `~/.claude/plugins/` | none | DIR |

## Formats

- Imports (MEM): `@path` anywhere in the file, relative to the importing file, `~` and absolute paths allowed, at most four hops, ignored inside code spans and fenced code. A backslash escapes a space; quoted paths are not imported.
- Rule frontmatter (MEM): `paths` is the only field Claude Code reads, as a YAML list or a comma separated string. Other fields such as `globs` and `description` are ignored, so the adapter reports them.
- Commands (SK): skill frontmatter fields except `name` and `paths`.
- Subagents (SUB): `name` and `description` are required; a file without them is skipped.
- Permissions (PERM): Bash rules use the space and `*` form, `Bash(<cmd> *)`. `Bash(<cmd>:*)` is an equivalent legacy form at the end only and is imported as the same command. Path rules use `Read(<path>)` and `Edit(<path>)`; `Write(...)` is accepted but never matched. `//path` is absolute, `/path` is relative to the settings file, so an absolute library path is written as `//path` and a single slash rule stays native. MCP server allows are `mcp__<server>`, with `.` and spaces turned into `_`.
- Hooks (HOOK): `hooks.<Event>[] = {matcher?, hooks: [handler...]}`.
- MCP (MCP): `type` is `stdio`, `http` (alias `streamable-http`) or `sse`. An entry with `url` and no `type` is a configuration error, so the adapter always writes `type`. `${VAR}` and `${VAR:-default}` expand in `command`, `args`, `env`, `url` and `headers`.
- Laravel Boost (BOOST, BOOSTCFG): it replaces only the `<laravel-boost-guidelines>` block of CLAUDE.md, writes the `laravel-boost` server to `.mcp.json`, and lists the skills it installs under `skills` (a JSON list of skill names) in `boost.json`.

## Confirmed during this task

- `${VAR}` expansion in `~/.claude.json`: MCP states it for `.mcp.json` and says that a user or local scoped entry in `~/.claude.json` that uses `${CLAUDE_PROJECT_DIR}` "requires a default", which only makes sense if those entries expand too. The adapter writes `${VAR}` in both files.
- `boost.json` `skills`: confirmed from BOOSTCFG (`Config::getSkills`, `Config::setSkills`).

## Still unconfirmed

- Top level `mcpServers` in `~/.claude.json` for user scope. MCP only says user servers are "stored in ~/.claude.json". The location is observed on real machines and was chosen by the maintainer; the backup entry for the file is limited to `/mcpServers`.
- Import cycles: MEM does not describe them. The resolver stops at the second visit and reports the import as `cycle`.
- `CLAUDE_CONFIG_DIR` moves `~/.claude` and `~/.claude.json` (DIR, SET). The adapter does not follow it; Import reports a note when it is set.
