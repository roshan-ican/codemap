# Task 2: Bob runner and `/api/impact` endpoint

- **Tool:** IBM Bob IDE, Agent mode
- **Branch:** `ibm-bob-hackathon`
- **Started:** 2026-09-27 01:11
- **Cost:** 13.74 Bobcoins
- **Screenshots:** [result summary](02-impact-endpoint-result.png), [task view](02-impact-endpoint.png), work log `02-impact-endpoint-worklog-1..5.png`

## Goal

Connect codemap's deterministic `ImpactContext` to IBM Bob through Bob Shell
(`bob run`) and expose the result on a single HTTP endpoint.

## What Bob built

| File | Change |
|------|--------|
| `bob_runner.go` | `ImpactAnalyzer` interface; Bob Shell runner; builds the prompt from `ImpactContext` (`impactPromptFromContext`, `writePromptList`); combined `impactResponse` type |
| `map_web.go` | `analyzer` field on the server; `newMapWebServer` calls `newBobRunner(root)` and leaves it nil if `bob` is not on PATH; new `handleImpact` |
| `map_web_test.go` | `fakeAnalyzer`; tests for the success response, missing Bob (503) and Bob error mapping (5 subtests); validation tests use a nil analyzer |
| `impact.go` | sentinel errors `ErrFileNotFound` and `ErrFileUnchanged` |

## Request flow

```
POST /api/impact {"id": "path/to/file.go"}
  1. buildImpactContext(snapshot, id)      404 not found / 422 unchanged
  2. no Bob Shell installed?               503
  3. prompt → analyzer.Analyse(ctx, ...)   401 not signed in / 504 timeout / 502 failed or bad output
  4. 200 {context: ImpactContext, analysis: ImpactAnalysis}
```

If Bob is unavailable, the endpoint returns an error. It never fakes an analysis.

## Verification

Bob ran the test suite at the end of the task, and all tests passed. The same
task also answered "how do I run this project?" (build the frontend, then `go run .`).
