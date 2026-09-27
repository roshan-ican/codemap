# AGENTS.md

This file provides guidance to agents when working with code in this repository.

## Before editing

- Read `AGENTS.md` in the project root for the full picture.
- All source is `package main` — there are no sub-packages.
- `NodeID` must always be a forward-slash relative path (repo root = prefix); never use `filepath.Join` to build one.

## Non-obvious coupling

- `buildImpactContext` (impact.go) is called by both `handleImpact` (map_web.go) and tests directly — it is the **only** place that reads `mapSnapshot.Diffs`; do not read diffs elsewhere.
- `mapWebServer.snapshot` is replaced wholesale on each file-watch rebuild. Handlers must copy `server.snapshot` under `server.mu.RLock()` before doing any work — never hold the lock across IO or Bob calls.
- `buildImpactPrompt` (bob_runner.go) appends the schema instruction to whatever `impactPromptFromContext` returns. Only `Analyse` calls `buildImpactPrompt`; callers pass raw context text.
- `mapAssets()` has two implementations behind a build tag — do not add a third; choose `map_assets_dev.go` (dev) or `map_assets_embed.go` (release).

## Required patterns when adding a new POST handler

```go
if !sameOrigin(request) { ... 403 }
mime.ParseMediaType(...)          // 415 if not application/json
decoder.DisallowUnknownFields()
decoder.Decode(&payload)          // 400 on error or empty required field
decoder.Decode(&struct{}{})       // must return io.EOF — else 400 (trailing content)
```

## Error wrapping rule

Sentinel errors must be the **first** `%w` verb so `errors.Is` traversal works:
```go
fmt.Errorf("%w: %w", ErrBobBadOutput, underlyingErr)  // correct
fmt.Errorf("context: %w", ErrBobBadOutput)             // wrong — loses sentinel position
```

## Adding a new Bob error sentinel

Add to the `var (...)` block in `bob_runner.go`, add a `case errors.Is(err, ErrNew*):` branch in `handleImpact` (map_web.go), and add a table row to `TestImpactHandlerBobErrors` (map_web_test.go).
