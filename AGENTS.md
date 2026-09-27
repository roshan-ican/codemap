# AGENTS.md

This file provides guidance to agents when working with code in this repository.

## Commands

```bash
# Run all tests (no frontend needed)
go test ./...

# Run a single test by name
go test -run TestBuildImpactContext ./...

# Run with webembed build tag (tests embedded asset path)
go test -tags webembed ./...

# Dev server — requires frontend/dist/ to exist first
npm run build --prefix frontend
go run .

# Dev server against a base branch
go run . -base origin/main

# Release binary (all platforms, embeds frontend)
make build
```

**Critical:** `go run .` (no build tag) reads `frontend/dist/` from disk at runtime.
`go build -tags webembed` / `go install -tags webembed .` bakes `frontend/dist/` into the binary at compile time via `//go:embed`. The two code paths live in [`map_assets_dev.go`](map_assets_dev.go) and [`map_assets_embed.go`](map_assets_embed.go).

## Architecture

Single Go binary (`package main`), single-page Svelte frontend served from the same process.

Key data flow:
```
Git repo on disk
  → buildMapSnapshot()       (map_api.go)  — diffs, activity, descriptions
  → mapWebServer.snapshot    (map_web.go)  — held in memory, rebuilt on file-watch
  → GET /api/graph           — full graph JSON to Svelte UI
  → POST /api/context        — AI prompt for selected files
  → POST /api/impact         — deterministic ImpactContext + Bob LLM analysis
```

`NodeID` is always a **forward-slash relative path** from the repo root (applied by `filepath.ToSlash` in `map_build.go`). Never use OS-native separators as a NodeID.

## Two build tags, two asset modes

| Mode | Command | Assets |
|------|---------|--------|
| Dev  | `go run .` | reads `frontend/dist/` at runtime |
| Release | `go install -tags webembed .` | embeds `frontend/dist/` at compile time |

## Error handling conventions

- Sentinel errors in `impact.go`: `ErrFileNotFound`, `ErrFileUnchanged` — use `errors.Is`, not string matching.
- Bob runner sentinels in `bob_runner.go`: `ErrBobNotFound`, `ErrBobNotAuthenticated`, `ErrBobFailed`, `ErrBobTimeout`, `ErrBobUnsuccessful`, `ErrBobBadOutput`.
- Wrap with `fmt.Errorf("%w: ...", sentinel)` so callers can still use `errors.Is`.

## HTTP handler conventions (map_web.go)

Every mutating handler must:
1. Check `sameOrigin(request)` → 403 if cross-origin
2. Parse `Content-Type: application/json` → 415 if wrong
3. Use `decoder.DisallowUnknownFields()` and check for trailing content (second `Decode` must return `io.EOF`)

## Testing conventions

- All tests are in `package main` (same package as source, no `_test` suffix on package name).
- Tests that need a real Git repo use `newTestRepoWithChangedGoFile(t)` helper (defined in `impact_test.go`) — copy that pattern for new integration tests.
- Fake `bob` executable in tests: write a shell script to `t.TempDir()`, write payload to a sidecar file (`bob_output.txt`), `cat` it — avoids quoting issues with JSON in script bodies. See `writeFakeBob` in `bob_runner_test.go`.
- `prependPath(t, dir)` (in `bob_runner_test.go`) uses `t.Setenv` — automatically restored after test.
- The `webembed` tag is **not** needed to run unit tests; only needed to test the embedded-asset path.

## ImpactAnalyzer injection

`mapWebServer.analyzer` is `nil` when `bob` is absent from PATH — the server starts normally and `/api/impact` returns 503. Tests inject `&fakeAnalyzer{}` directly into the struct; no factory needed.

## Bob result envelope

`bob run --format json` stdout shape (defined as `bobResult` in `bob_runner.go`):
```json
{ "type": "result", "status": "success"|"error"|"aborted", "stats": {…}, "last_message": "<assistant text>" }
```
Parse envelope first → check `status == "success"` → parse `last_message` as `ImpactAnalysis` JSON.
Bob exits with code **3** for authentication failures.
