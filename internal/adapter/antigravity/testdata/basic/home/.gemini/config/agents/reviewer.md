---
name: reviewer
description: Reviews diffs.
tools:
  - view_file
  - grep_search
mainAgent: false
model: pro
commandExecutionPolicy: sandbox
mcpServers:
  - name: tracker
    env:
      TRACKER_TOKEN: fixture-agent-secret
---
Be strict.
