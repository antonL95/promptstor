# Codex adapter sources

Checked on 2026-11-27 against Codex CLI 0.172.0. Docs under `developers.openai.com/codex/*` redirect to `learn.chatgpt.com/docs/*`. Source paths are under `https://github.com/openai/codex/blob/rust-v0.172.0/codex-rs/`, written as `SRC/`.

| Short name | URL |
|---|---|
| AGENTS | https://learn.chatgpt.com/docs/agent-configuration/agents-md.md and SRC/core/src/agents_md.rs |
| SKILLS | https://learn.chatgpt.com/docs/build-skills.md, SRC/ext/skills/src/host_roots.rs, SRC/skills/src/parser.rs |
| PROMPTS | https://learn.chatgpt.com/docs/custom-prompts.md |
| SUB | https://learn.chatgpt.com/docs/agent-configuration/subagents.md, SRC/config/src/config_toml.rs, SRC/agent-roles/src/agent_role_config.rs |
| RULES | https://learn.chatgpt.com/docs/agent-configuration/rules.md, SRC/core/src/exec_policy.rs, SRC/execpolicy/src/parser.rs, SRC/execpolicy/src/amend.rs |
| PERM | https://learn.chatgpt.com/docs/permissions.md, SRC/core/src/config/permissions.rs, SRC/core/src/config/permission_path.rs, SRC/config/src/permissions_toml.rs |
| HOOKS | https://learn.chatgpt.com/docs/hooks.md, SRC/hooks/src/lib.rs, SRC/config/src/hook_config.rs |
| MCP | https://learn.chatgpt.com/docs/extend/mcp.md, SRC/cli/src/mcp_cmd.rs |
| STATE | SRC/core/src/config/edit.rs |
| CONFIG | https://learn.chatgpt.com/docs/config-file/config-advanced.md |

## Locations

`~/.codex` is replaced by `CODEX_HOME` when that variable holds an absolute path (AGENTS).

| What | User scope | Project scope | Source |
|---|---|---|---|
| Instructions | `~/.codex/AGENTS.md`, overridden by a non empty `AGENTS.override.md` | `AGENTS.md`, written by the agents-md adapter | AGENTS |
| Skills | `~/.agents/skills/<name>/SKILL.md`; `~/.codex/skills` is deprecated | `.agents/skills/<name>/SKILL.md` | SKILLS |
| Bundled skills | `~/.codex/skills/.system/` | none | SKILLS |
| Commands | written as skills in `~/.codex/skills`; `~/.codex/prompts/*.md` is imported only | not supported | SKILLS, PROMPTS |
| Subagents | `~/.codex/agents/*.toml` | `.codex/agents/*.toml` | SUB |
| Deny commands | `~/.codex/rules/promptstor.rules` | `.codex/rules/promptstor.rules` | RULES |
| Runtime grants | `~/.codex/rules/default.rules` | none | RULES |
| Deny paths | `[permissions.promptstor]` in `config.toml` | the same in `.codex/config.toml` | PERM |
| Hooks | `~/.codex/hooks.json` | `.codex/hooks.json` | HOOKS |
| MCP servers | `[mcp_servers.<name>]` in `config.toml` | the same in `.codex/config.toml` | MCP |
| Profiles | `~/.codex/<name>.config.toml`, reported only | none | CONFIG |
| Plugins | `~/.codex/plugins/` | none | STATE |

## Formats

- Skills (SKILLS): the parser reads `name`, `description` and `metadata.short-description`. A skill without a description fails to load. Names are at most 64 characters.
- Subagents (SUB): `name`, `description` and `developer_instructions` are required. Optional keys the adapter carries: `model` (item meta `codex_model`), `model_reasoning_effort`, `model_reasoning_summary`, `model_verbosity`.
- Rules (RULES): `prefix_rule(pattern=[...], decision="forbidden", justification="...")`. A pattern element matches one argv token literally. Codex appends its own approvals to `default.rules`.
- Permission profiles (PERM): a profile without `extends` grants nothing beyond its own entries, so the adapter writes `extends = ":workspace"`. Filesystem keys are absolute, `~/` or special paths. Relative patterns go under `filesystem.":workspace_roots"` and may not contain `.` or `..` components at the start or `..` anywhere. Glob characters are `* ? [ ]`, and globs are accepted with `deny`. Profiles are ignored when `sandbox_mode` is set.
- Hooks (HOOKS): `hooks.<Event>[] = {matcher?, hooks: [handler...]}`. Events: SessionStart, SessionEnd, SubagentStart, PreToolUse, PermissionRequest, PostToolUse, PreModelRequest, PostModelRequest, PreCompact, PostCompact, UserPromptSubmit, SubagentStop, Stop, Interrupt. Codex stores trust in `[hooks.state.*]`, which PromptStor never touches, and skips a changed hook until it is trusted again in `/hooks`.
- MCP (MCP): stdio servers use `command`, `args`, `env`, `env_vars`. HTTP servers use `url`, `bearer_token_env_var`, `http_headers`, `env_http_headers`. There is no SSE transport and no `${VAR}` expansion. Server names may use letters, numbers and `- _ : @ / .` (`validate_server_name`).
- `codex mcp add` and `codex mcp remove` rewrite every `[mcp_servers.*]` table (MCP). Codex writes through a symlinked `config.toml` (STATE); PromptStor never does.
- Codex owned tables (STATE): `[projects.*]`, `[hooks.state.*]`, `[[skills.config]]`, `[notice]`, `[tui]`, `[plugins.*]`, `[marketplaces.*]`.

## Resolved during this task

- `disable-model-invocation`: not read by Codex (SKILLS parser fields are `name`, `description`, `metadata`), so command skills are written without it. The documented counterpart is `agents/openai.yaml` with `policy.allow_implicit_invocation: false`, which the adapter does not generate.
- `~/.codex/prompts`: no loader in the 0.172.0 source. Files are imported with a note and never written.
- `.codex/skills` inside a project: found in source only. Reported, not imported, never written.
- `oauth_resource` and every other `mcp_servers` key outside the nine above: a server table that holds one is reported and left as it is.
- Hook exit codes other than 0 and 2: not relied on.
- The `.rules` symlink skip: not relied on, because PromptStor never writes through a symlink.

## Still unconfirmed

- Whether a profile `deny` is enforced for `apply_patch` and built in file read tools, not only for sandboxed shell commands. PERM documents deny paths for the sandbox. Every path deny therefore reports a blocking result that needs acknowledgement. While `default_permissions = "promptstor"` and the other conditions hold, `[permissions.promptstor]` is always written, with no entries if needed, so the root key never names a missing profile.
- `/etc/codex/config.toml` and managed configuration can also set `sandbox_mode`. The adapter checks only the user and project `config.toml`.
- Whether the desktop app or IDE extension still reads `~/.codex/prompts`.
