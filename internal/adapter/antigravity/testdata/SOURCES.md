# Antigravity adapter sources

Checked on 2026-11-27. Paths under `https://antigravity.google/docs/` are written as `A/`. Every page also has a Markdown copy at the same address with `.md` appended.

The research pass also read the docs that ship inside the app as builtin skills. They are cited as `bundled:<file>`. This task did not reread them.

| Short name | URL |
|---|---|
| DISC | A/customization-discovery |
| RULES | A/rules, bundled:agy-customizations/docs/rules.md, bundled:migrate-agent-settings/SKILL.md |
| SKILLS | A/skills |
| WORKFLOWS | A/migration/workflows-to-skills, bundled:migrate-workflows/SKILL.md |
| SUB | A/subagents |
| PERM | A/permissions (section "Antigravity CLI"), A/cli/commands/permissions |
| HOOKS | A/hooks |
| MCP | A/mcp, A/cli/gcli-migration |
| PLUGINS | A/plugins, bundled:migrate-agent-settings/SKILL.md |
| CLI | A/cli/reference |

## Locations

| What | User scope | Project scope | Source |
|---|---|---|---|
| Rules with frontmatter | `~/.gemini/config/rules/*.md` | `.agents/rules/*.md` | RULES |
| Standalone rules, imported only | `~/.gemini/config/{GEMINI,AGENTS}.md`, `~/.gemini/{GEMINI,AGENTS}.md` | read by other adapters | RULES |
| Skills | `~/.gemini/config/skills/<name>/` | `.agents/skills/<name>/` | SKILLS, DISC |
| Commands | written as skills | not written | WORKFLOWS |
| Subagents | `~/.gemini/config/agents/<name>.md`, also `<name>/agent.md` (imported only) | `.agents/agents/` | SUB |
| Denies | `/permissions/deny` in `~/.gemini/antigravity-cli/settings.json` | none | PERM |
| Hooks | `~/.gemini/config/hooks.json` | `.agents/hooks.json`, noted and not managed | HOOKS |
| MCP servers | `/mcpServers` in `~/.gemini/config/mcp_config.json` | `.agents/mcp_config.json` | MCP |
| Plugins, foreign | `~/.gemini/config/plugins/`, `config/import_manifest.json`, `antigravity/builtin/` | not read | PLUGINS |
| App data folders, for detection | `~/.gemini/antigravity`, `antigravity-ide`, `antigravity-cli` | none | HOOKS |

## Formats

- Rules (RULES): `trigger` is required and is `always_on`, `model_decision`, `glob` or `manual`. A missing or unknown trigger discards the rule. `description` is required for `model_decision`. `globs` (alias `glob`) is one comma separated string. Limits are 24,000 bytes per file and a 20,000 token budget for global and always on rules.
- Globs (bundled): matched against absolute paths and file names, so a relative pattern with a folder needs a `**/` prefix. There is no brace expansion.
- Skills (SKILLS): `description` is required and `name` defaults to the folder name. No other frontmatter field is documented, so `disable-model-invocation` is never written.
- Subagents (SUB): `name`, `description`, `tools`, `mainAgent`, `subagent`, `model` (`inherit`, `flash`, `pro`), `commandExecutionPolicy` (`off`, `auto`, `eager`, `sandbox`), `mcpServers`, `skills`, `plugins`. A misspelled tool name can hang the subagent. The adapter carries these as item meta with an `antigravity_` prefix and never carries `mcpServers`. Built in names are `research`, `browser` and `self`.
- Permissions (PERM): rules are `action(target)`, and deny wins over ask and allow. `command(prefix)` matches word by word, and `regex:` switches to a regular expression. `read_file` and `write_file` take an absolute path or one relative to a workspace root, cover everything below it, and have no globs. A denied read also denies the write. The adapter writes both actions and expands `~/`, because `~` in a rule target is not documented.
- Hooks (HOOKS): an object keyed by hook name. A definition holds optional `enabled` and the events `SessionStart`, `PreInvocation`, `PostInvocation`, `Stop` (a list of handlers) and `PreToolUse`, `PostToolUse` (a list of `{matcher?, hooks: [handler]}`). A handler is `{type?: "command", command, timeout?}`.
- MCP (MCP): `command`, `args`, `env`, `cwd` for stdio and `serverUrl` (or `url`), `headers` for Streamable HTTP. `httpUrl` and legacy SSE are not supported. The control fields `disabled`, `enabledTools`, `disabledTools`, `tools` and `timeoutSeconds` are kept as they are, and `cwd` is kept on stdio servers. A server with any other field is reported and left alone.

## Resolved during this task

- `disable-model-invocation`: not documented on A/skills or A/migration/workflows-to-skills. Never written.
- Environment interpolation in `mcp_config.json`: not documented on A/mcp. The capability table has no env reference and a value that needs one is reported.
- `url` and `serverUrl`: A/mcp names `serverUrl` and accepts `url`. The adapter writes `serverUrl`.
- Subagent tool names: A/subagents uses `view_file` style names. Tool lists travel only as `antigravity_tools` and are not translated.
- `~` in permission targets: undocumented, so the adapter writes the expanded path.

## Still unconfirmed

- The `@[label](path)` include. A/rules now documents it (section "File includes"), but the owner decision for release 1 treats it as unconfirmed: each line is reported as unresolved and is not followed.
- Where the App and IDE keep permissions. Every deny therefore needs an acknowledgement.
- Whether the App rewrites `mcp_config.json` beyond the `disabled` field.
- The meaning of `import_manifest.json` fields. The file is reported as foreign and never read.
- Hook exit codes and timeout behaviour.
- `~/.agents/skills` is not documented as read by any surface and is not used.
