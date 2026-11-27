# Privacy

In release 1 nothing leaves the machine. PromptStor has no account, no sync service, no telemetry and no update check. It makes no network connections of its own.

One setting changes that, and it is off by default. With `open_project_prs: true` in `config.yaml`, a project change made through the PromptStor MCP server is pushed to the repository's `origin` and opened as a draft pull request with the `gh` command.

Two things are outside PromptStor. The agents you configure send what they read to their own providers, including anything they read through the PromptStor MCP server. The [GitHub Action](ci.md) downloads a release from GitHub inside your CI runner.

## What runs locally

- Edit guards are hooks that call the `promptstor` binary on your machine. They read the list of files PromptStor owns.
- The MCP server talks to an agent over standard input and output. It opens no port. It returns library items in full, including any plain values stored in them.
- Scheduled checks run on your machine through launchd, a systemd user timer or cron, and show a desktop notification.
- Keychain references are read with `security` on macOS and `secret-tool` on Linux.

## Where PromptStor keeps data

| Folder | Content |
|--------|---------|
| `~/.config/promptstor/` | Your library, device items and `config.yaml` |
| `~/.local/state/promptstor/` | Backups, the ownership manifest, acknowledgements, the last check and its log |

`XDG_CONFIG_HOME` and `XDG_STATE_HOME` move these folders. On Windows they are under `%APPDATA%` and `%LOCALAPPDATA%`.

## Secrets

Backups keep your own configuration files whole, including secret values you already have in them. Backup files are written with mode 0600 inside folders with mode 0700. Treat the state folder as carefully as the files it copies.

Secrets that PromptStor writes itself, by resolving a reference, are redacted in backups. A restore resolves them again.

When PromptStor imports an MCP server, it replaces the secrets it recognises with references. Recognition goes by names and common patterns, so look over what you import before you commit or share it.

## Credential exclusions

PromptStor never copies the following into a backup, and never restores them.

Files with these names, in any folder:

- `auth.json`
- `.credentials.json`
- `oauth_creds.json`
- `mcp-oauth-tokens-v2.json`
- `mcp_oauth_tokens.json`
- `mcp-auth.json`
- `google_accounts.json`

Any other file whose name contains `token`, `credential`, `secret` or `oauth`, in any letter case. Markdown files and folders are not excluded by this rule.

These paths:

- `~/.config/opencode/service.json` (also under `XDG_CONFIG_HOME`)
- everything in `~/.local/share/opencode/`
- everything in `~/.claude/backups/`

These keys of `~/.claude.json`, which are removed before anything is stored and kept as they are on restore:

- `/customApiKeyResponses`
- `/oauthAccount`
- `/primaryApiKey`
