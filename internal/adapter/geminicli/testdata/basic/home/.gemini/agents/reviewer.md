---
name: reviewer
description: Reviews diffs for bugs.
tools: [read_file, grep_search]
model: example-model
max_turns: 10
mcp_servers:
  private:
    command: example
    env:
      TOKEN: fixture-token-value
---
You review code.
