# AGENTS.md adapter sources

The adapter relies on one location and one format.

| What | Location | Format |
|---|---|---|
| Shared project rules | `<repo>/AGENTS.md` | Plain Markdown, no frontmatter |

PromptStor owns one managed section in that file. It reads and writes nothing at user scope.

Checked on 2026-11-27.

## Which tools read the file

- Codex reads `AGENTS.md` in each directory from the project root down. A combined budget of 32 KiB applies to project files. https://learn.chatgpt.com/docs/agent-configuration/agents-md.md
- opencode reads `AGENTS.md` at the project root and in subdirectories. https://opencode.ai/docs/rules/
- Cursor reads `AGENTS.md` in the project root and any subdirectory. https://cursor.com/docs/rules.md#agentsmd
- Antigravity reads `AGENTS.md` in each directory from the workspace up to the repository root. https://antigravity.google/docs/rules
- Gemini CLI reads it only when `context.fileName` lists it. The Gemini adapter owns that setting. https://github.com/google-gemini/gemini-cli/blob/main/docs/cli/gemini-md.md
- Claude Code reads it through an `@AGENTS.md` import in `CLAUDE.md`. The Claude Code adapter owns that line. https://code.claude.com/docs/en/memory

## Scoping

`AGENTS.md` has no file scoping and no agent activation, so such rules are written with a plain text annotation and reported.
