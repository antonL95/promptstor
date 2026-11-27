# opencode adapter sources

Checked on 2026-11-27 against opencode V2. Source facts were read from `anomalyco/opencode` at tag `v2.4.2` (commit `fc31a485317970bcf6be7b431a9830aaef875731`). Paths under `https://github.com/anomalyco/opencode/blob/v2.4.2/packages/` are written as `S/`. Paths under `https://opencode.ai/docs/` are written as `D/`.

| Short name | URL |
|---|---|
| CONFIG | D/config/, S/core/src/config.ts, S/core/src/config/discovery.ts |
| NORM | D/migrate-v1/, S/core/src/config/normalize.ts, S/core/src/v1/config/migrate.ts |
| PATHS | S/util/src/global.ts, S/util/src/global-roots.ts |
| RULES | D/rules/, S/core/src/config/plugin/instruction.ts |
| AGENTS | D/agents/, S/core/src/config/plugin/agent.ts, S/schema/src/config/agent.ts, S/schema/src/config/model.ts |
| COMMANDS | D/commands/, S/core/src/config/plugin/command.ts, S/schema/src/config/command.ts |
| SKILLS | D/skills/ |
| PERM | D/permissions/, S/schema/src/permission.ts, S/core/src/permission/policy.ts, S/core/src/util/wildcard.ts, S/core/src/file-access.ts, S/core/src/shell/permissions.ts, S/core/src/tool/plugin/grep.ts |
| MCP | D/mcp-servers/, S/schema/src/mcp.ts, S/core/src/v1/config/mcp.ts, S/core/src/config/plugin/mcp.ts |
| PLUGINS | D/plugins/, https://opencode.ai/config.json |

## Locations

| What | User scope | Project scope | Source |
|---|---|---|---|
| Config folder | `$XDG_CONFIG_HOME/opencode`, or `OPENCODE_CONFIG_DIR` | project root and `.opencode/` | PATHS, CONFIG |
| Config read | `opencode.json`, then `opencode.jsonc` | root pair, then `.opencode/` pair | CONFIG |
| Config written | `opencode.jsonc` | `<root>/opencode.jsonc` | CONFIG |
| Rules | managed section in `AGENTS.md` | `AGENTS.md`, written by agents-md | RULES |
| Skills | `~/.agents/skills/` written; also `skills/`, `skill/` imported | `.agents/skills/` written; also `.opencode/skills/`, `.opencode/skill/` imported | SKILLS |
| Commands | `commands/`, also `command/` imported | `.opencode/commands/` | COMMANDS |
| Subagents | `agents/`, also `agent/`, `modes/`, `mode/` imported | `.opencode/agents/` | AGENTS |
| Permissions | `/permissions` in `opencode.jsonc` | same | PERM |
| MCP servers | `/mcp/servers/<name>` in `opencode.jsonc` | same | MCP |
| Plugins and packages, foreign | `plugins/`, `plugin/`, `package.json`, lock files, `node_modules/`, `.gitignore` | same under `.opencode/` | PLUGINS |
| Never read | `~/.local/share/opencode/`, `service.json` | | research |

## Formats

- Merge (CONFIG): both names are loaded from every folder, `.json` first, so `.jsonc` wins. Every `.opencode` config beats every direct project file. Permission arrays are appended. A later MCP server definition replaces the whole earlier one.
- V1 and V2 (NORM): V1 `permission` and `tools` are converted and their `bash` action becomes `shell`. Native `permissions` rules are not converted, so a V2 rule with action `bash` never matches. Inside one document the order is `tools`, `permission`, `permissions`.
- Agents (AGENTS): the file name relative to the folder is the id. V2 frontmatter is `description`, `mode`, `model` (`provider/model#variant`), `hidden`, `color` (`#rrggbb`), `steps`, `disabled`, `request`, `permissions`, and the body is the system prompt. A file with an unknown key and none of `permissions`, `request`, `system`, `disabled` is read as V1, so only V2 keys are written. `mode` is written as `subagent` unless the item carries `opencode_mode`. `request` is never carried because headers can hold secrets. V1 `permission` and `tools` in an agent are reported, not converted.
- Built in agents: build, compaction, explore, general, plan, summary, title. PromptStor neither imports nor writes a file with one of these ids.
- Commands (COMMANDS): frontmatter is `description`, `agent`, `model`, `subagent` (V1 `subtask`). The body is the template.
- Permissions (PERM): `{"action","resource","effect"}`, last match wins. The shell action is `shell`. `*` matches any characters including `/`, `?` one character, everything else is literal. A trailing ` *` also matches the bare command. `~`, `~/` and `$HOME` are expanded for `read` and `edit`. Paths inside the workspace are matched relative to the working directory and paths outside it are absolute.
- PromptStor appends each deny at the end of the array, as a `json-element` unit keyed `/permissions@#<hash>`. Rules in a later layer (project config over user config, `.opencode` over the project root) and rules on an agent are evaluated after it and can allow again. The same applies to rules the user adds below it, and to an identical rule that already sits above an `allow`.
- Path denies write one `edit` and one `read` rule. The `grep` tool is gated by the search pattern only and shell commands are gated by command text only, so every path deny needs an acknowledgement.
- MCP (MCP): local is `type`, `command` (array), `environment`; remote is `type`, `url`, `headers`. The settings `cwd`, `disabled`, `codemode`, `timeout`, `protocol`, `pool` and `oauth: false` of an existing server are kept. A server with any other field is reported and left alone.
- Substitution (CONFIG): `{env:VAR}` and `{file:path}` are replaced anywhere in the config text. A server that would carry `{file:` is never written or imported.
- Writes by opencode itself (CONFIG, MCP): `opencode mcp add` writes to the first config file that exists, following symlinks. A settings change from the app writes the `shell` key into the last existing global config file.

## Resolved during this task

- `OPENCODE_CONFIG_DIR`: confirmed in S/util/src/global.ts. A relative value is reported, because opencode would resolve it against its own working directory. A value saved with `opencode service set env` is not visible to PromptStor.
- Shell action name: `shell`, confirmed in S/core/src/shell/permissions.ts and S/core/src/config/normalize.ts. `bash` is not written.
- Allow lists for MCP servers: tool actions are named `<server>_<tool>`, and a `<server>_*` rule would also match other servers sharing the prefix. `AllowMCPServers` is reported.
- Hooks: only JavaScript plugins exist.

## Still unconfirmed

- Remote MCP servers that only speak SSE. The `sse` transport is reported.
- `OPENCODE_DISABLE_CLAUDE_CODE_PROMPT` scope, V2 credential storage, plugin package locations and the contents of `service.json`. The adapter depends on none of them.
- How the shell scanner treats environment prefixes and `sh -c` wrappers when it matches a deny.
