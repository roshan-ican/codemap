# AGENTS.md

This file provides guidance to agents when working with code in this repository.

## Architectural constraints

- **Single in-memory snapshot** — `mapWebServer.snapshot` is the only state. There is no database, cache, or persistent store. All analysis is re-derived from this struct on every request. Rebuilds happen on a 250 ms debounced file-watch timer.
- **Snapshot is replaced atomically** under `mu` (sync.RWMutex). Handlers must snapshot-copy under RLock; they must never hold a lock across any blocking call (IO, Bob subprocess).
- **Two asset strategies share one function name** (`mapAssets()`) via build tag — `!webembed` reads disk, `webembed` uses `embed.FS`. Any change to how assets are served must be applied to both files.
- **No sub-packages** — the entire backend is `package main`. Internal separation is by filename convention only (`map_api.go`, `map_web.go`, `bob_runner.go`, etc.).

## Layering: deterministic vs. LLM

```
Git + graph  →  ImpactContext  (deterministic, always available)
                    ↓
             impactPromptFromContext()
                    ↓
             BobRunner.Analyse()  (LLM, may be unavailable)
                    ↓
             ImpactAnalysis
                    ↓
             impactResponse{Context + Analysis}  → HTTP
```
The deterministic layer must never be blocked by Bob availability. If `analyzer == nil`, context generation still succeeds; only the analysis step returns 503.

## Extension points

- **New language analyzer** — implement `Analyzer` interface (see `analyzer.go`), add to the slice in `map_scan.go`. The graph builder (`map_build.go`) is language-agnostic.
- **New API endpoint** — add handler method on `*mapWebServer`, register in `routes()`. Follow the 5-step request-validation pattern (origin → content-type → decode → trailing-content → business logic).
- **New Bob error** — add sentinel in `bob_runner.go`, handle in `handleImpact`'s switch, test in `TestImpactHandlerBobErrors`.

## What must stay unchanged when adding features

- `buildImpactContext` signature and sentinel errors — used by both the handler and direct tests.
- `mapSnapshot` field names — the snapshot is serialised/deserialised nowhere, but tests construct it directly; renaming fields breaks many tests at once.
- `sameOrigin` logic — security boundary for all mutating handlers.
