---
name: Code comments
description: Keep source files free of narration
promptstor:
  id: ps_00000000000000000000000001
  order: 10
  targets:
    include:
      - claude
      - codex
    exclude:
      - cursor
  scope:
    - '**/*.go'
  activation: files
  locked: true
---

# Code comments

Write no comments.
