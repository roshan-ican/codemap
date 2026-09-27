# Task 1: `/init` project rules

- **Tool:** IBM Bob IDE, Agent mode
- **Cost:** about 2.9 Bobcoins (the task total went from 13.74 to 16.61)
- **Screenshot:** [01-init-agents-md.png](01-init-agents-md.png)

An earlier `/init` ran before the Bob integration code existed, so it had little
to document. It was re-run after Steps 1–3, inside the same Bob task.

Bob explored the repository and created four rule files:

| File | Purpose |
|------|---------|
| `AGENTS.md` | Commands, build tags, handler conventions, test patterns, the Bob response format |
| `.bob/rules-agent/AGENTS.md` | Coding: coupling between files that isn't obvious, the required handler pattern, how to wrap errors |
| `.bob/rules-ask/AGENTS.md` | Questions and research: where data actually lives, gotchas in the data model |
| `.bob/rules-plan/AGENTS.md` | Planning: architectural constraints, layering, extension points |

Key facts Bob recorded for future tasks include:
- The `webembed` build tag changes how frontend assets are loaded.
- Diff text lives only server-side in `mapSnapshot.Diffs`.
- The actual reply from `bob run --format json` is in `last_message`.
- Every POST handler must reject unknown fields and trailing content.

These rules give later Bob tasks the project context up front, so they spend
fewer Bobcoins rediscovering it.
