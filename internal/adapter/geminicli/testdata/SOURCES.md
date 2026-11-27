# Gemini CLI adapter sources

Checked on 2026-11-27 against Gemini CLI 0.71.0. Paths under `https://github.com/google-gemini/gemini-cli/blob/v0.71.0/` are written as `G/`. The research pass also read `main` and PRs 26941 and 16252.

| Short name | URL |
|---|---|
| MEM | G/docs/cli/gemini-md.md, G/docs/tools/memory.md, G/packages/core/src/utils/memoryDiscovery.ts, G/packages/core/src/tools/memoryTool.ts, https://github.com/google-gemini/gemini-cli/pull/26941 |
| IMP | G/docs/reference/memport.md, G/packages/core/src/utils/memoryImportProcessor.ts |
| CMD | G/docs/cli/custom-commands.md, G/packages/cli/src/services/FileCommandLoader.ts |
| SKILLS | G/docs/cli/skills.md, G/packages/core/src/skills/skillLoader.ts, G/packages/core/src/config/storage.ts |
| SUB | G/docs/core/subagents.md, G/packages/core/src/agents/agentLoader.ts, G/packages/core/src/tools/tool-names.ts |
| POLICY | G/docs/reference/policy-engine.md, G/packages/core/src/policy/toml-loader.ts, G/packages/core/src/policy/utils.ts, G/packages/core/src/policy/policy-engine.ts, G/packages/core/src/policy/stable-stringify.ts, G/packages/core/src/policy/config.ts, G/packages/cli/src/config/policy.ts |
| TOOLS | G/packages/core/src/tools/definitions/base-declarations.ts, G/packages/core/src/tools/read-file.ts, G/packages/core/src/tools/write-file.ts, G/packages/core/src/tools/edit.ts |
| HOOKS | G/docs/hooks/reference.md, G/packages/core/src/hooks/types.ts |
| MCP | G/docs/tools/mcp-server.md, G/packages/core/src/tools/mcp-client.ts, G/packages/core/src/tools/mcp-client-manager.ts, G/packages/cli/src/commands/mcp/add.ts |
| ENV | G/packages/cli/src/utils/envVarResolver.ts, G/packages/core/src/utils/paths.ts |
| SET | G/docs/reference/configuration.md, G/schemas/settings.schema.json, G/packages/cli/src/config/settings.ts, G/packages/cli/src/utils/commentJson.ts |
| EXT | G/docs/extensions/reference.md |

## Locations

| What | User scope | Project scope | Source |
|---|---|---|---|
| Memory file | `~/.gemini/<first context.fileName>`, default `GEMINI.md` | `GEMINI.md`, imported only | MEM |
| Context file names | read from `/context/fileName` | PromptStor writes `["AGENTS.md","GEMINI.md"]` to `.gemini/settings.json` | MEM, SET |
| Commands | `~/.gemini/commands/**/*.toml` | `.gemini/commands/**/*.toml` | CMD |
| Skills | `~/.agents/skills`, also `~/.gemini/skills` (imported only) | `.agents/skills`, also `.gemini/skills` (imported only) | SKILLS |
| Subagents | `~/.gemini/agents/*.md` | `.gemini/agents/*.md` | SUB |
| Denies | `~/.gemini/policies/promptstor.toml` | not supported | POLICY |
| Runtime grants | `~/.gemini/policies/auto-saved.toml`, never read or written by the adapter | none | POLICY |
| Allowed MCP servers and tools | `/mcp/allowed`, `/tools/allowed` in `settings.json` | the same in `.gemini/settings.json` | SET |
| Hooks | `/hooks` in `settings.json` | `/hooks` in `.gemini/settings.json` | HOOKS |
| MCP servers | `/mcpServers` in `settings.json` | `/mcpServers` in `.gemini/settings.json` | MCP |
| Extensions | `~/.gemini/extensions/` | none | EXT |

## Formats

- Imports (IMP): `@path` after whitespace, where the path starts with `.`, `/` or a letter. `~` is not expanded. Imports stay inside the nearest folder with `.git`, at most five hops. The `@[label](path)` form is not import syntax.
- Commands (CMD): `prompt` is required and `description` is optional. Nothing else is read. A subfolder becomes a `:` namespace.
- Skills (SKILLS): a skill needs `name` and `description` in its frontmatter.
- Subagents (SUB): the frontmatter schema is strict. Local agents take `name` (`^[a-z0-9-_]+$`), `description`, `display_name`, `tools`, `mcp_servers`, `model`, `temperature`, `max_turns`, `timeout_mins` and `kind`. Files whose name starts with `_` are ignored. The adapter carries these as item meta with a `gemini_` prefix, and never carries `mcp_servers` or remote agents.
- Policy rules (POLICY): `[[rule]]` with `toolName`, `commandPrefix` or `argsPattern`, `decision` and `priority` (0 to 999). `commandPrefix` is allowed only with `run_shell_command`. `argsPattern` is RE2, matched against the arguments as JSON with sorted keys, where each top level pair is wrapped in NUL bytes. A pattern with a quantifier after a group is rejected as a nested quantifier. User tier rules written by "always allow" use priority 950, so the adapter writes 999.
- File tools (TOOLS): `read_file`, `write_file` and `replace` take `file_path`. A path deny matches that text only, so it is always reported as blocking.
- Hooks (HOOKS): `hooks.<Event>[] = {matcher?, sequential?, hooks: [{type: "command", command, name?, timeout?, description?}]}`. Events: SessionStart, SessionEnd, BeforeAgent, AfterAgent, BeforeModel, AfterModel, BeforeToolSelection, BeforeTool, AfterTool, PreCompress, Notification.
- MCP (MCP): stdio uses `command`, `args`, `env`. Remote servers use `url` with `type` `http` or `sse`, plus `headers`, which is what `gemini mcp add` writes. `httpUrl` is deprecated and wins over `url`. A `url` without `type` is treated as HTTP by the source, whatever the older docs say.
- `mcp.allowed` (MCP): a non empty list blocks every server that is not in it, and also allows the tools of the listed servers. The adapter therefore only adds to a list that already has entries.
- Environment (ENV): `$VAR`, `${VAR}` and `${VAR:-default}` expand in every string of `settings.json`. The adapter writes `${VAR}`. `GEMINI_CLI_HOME` replaces the home folder.
- Settings (SET): `settings.json` is JSONC.

## Behaviour to note in the user docs

- Gemini CLI rewrites `settings.json` on load and on many actions. It keeps comments and key order and normalises the indent to two spaces.
- The write is a temp file plus rename, so a symlinked `settings.json` becomes a regular file.
- The model edits the memory file directly. An edit inside the managed section is labelled "agent memory write".

## Resolved during this task

- `GEMINI_CLI_HOME`: confirmed in ENV. The adapter does not follow it; Import reports a note when it is set.
- Shape of `[[rule]]` versus `[[rules]]` in the subagent docs: the loader reads `[[rule]]` with `decision`.
- Subagent MCP key: the loader reads `mcp_servers`, not `mcpServers`.

## Still unconfirmed

- `tools.exclude` argument narrowing. The adapter never writes `tools.exclude`.
- Whether `write_file` writes through a symlinked memory file. PromptStor never writes through a symlink.
- Extension enablement file name. The whole extensions folder is reported as foreign.
- Whether Antigravity resolves `@[label](~/path.md)`. Gemini CLI does not, so those lines are reported as unresolved.
