# IBM Bob sessions

Evidence of IBM Bob usage for the IBM Bob 2.0 Hackathon. Bob IDE was the
development partner for the Bob integration in codemap: the `BobRunner`, the
`POST /api/impact` endpoint and the tests around them.

**Demo video:** https://www.tella.tv/video/codemap-understand-code-git-changes-297a

Per the hackathon guide, each Bob IDE task has a **task session summary
screenshot** next to a short written summary.

| # | Bob IDE task | Mode | Bobcoins | Summary | Screenshot |
|---|--------------|------|---------:|---------|------------|
| 1 | `/init`: generate `AGENTS.md` + `.bob/` mode rules | Agent | ~2.9 | [01-init-agents-md.md](01-init-agents-md.md) | [01-init-agents-md.png](01-init-agents-md.png) |
| 2 | Bob runner + `/api/impact` endpoint | Agent | 13.74 | [02-impact-endpoint.md](02-impact-endpoint.md) | [result](02-impact-endpoint-result.png), [task view](02-impact-endpoint.png), work log [1](02-impact-endpoint-worklog-1.png) [2](02-impact-endpoint-worklog-2.png) [3](02-impact-endpoint-worklog-3.png) [4](02-impact-endpoint-worklog-4.png) [5](02-impact-endpoint-worklog-5.png) |
| 3 | "Analyze impact with Bob" UI in `App.svelte` | Agent | ~4.9 | see below | [task view](03-ui-button.png), [result](03-ui-button-result.png) |
| 4 | Commit the hackathon work in 7 reviewed commits | Agent | — | see below | [04-commits.png](04-commits.png) |

[Bob IDE recent tasks](00-bob-ide-recent-tasks.png) shows the task list with costs.

Bobcoins used so far: **~35 of 40** (Bob IDE shows 34.42 on the main task, plus 0.25 for the first `/init`).

## How codemap and Bob split the work

```
codemap  = deterministic repository intelligence (diff, callers, dependencies, tests)
IBM Bob  = semantic explanation (what changed, what is affected, why, what to test)
```

## Task 3: UI

Bob added the "Analyze impact with Bob" button, loading state, per-status error messages (503/401/504/502) and the result panel (Summary, Affected areas, Reasoning, Relevant tests, codemap evidence) to `frontend/src/App.svelte`. Only that file changed; the frontend build and `go test ./...` pass.

## Task 4: Commits

Bob ran the safety checks (branch, `.env` and cache ignored, gofmt, vet, tests, frontend build) and created the seven commits on `ibm-bob-hackathon`. The branch was then pushed to GitHub.
