package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	bobRunTimeout = 90 * time.Second
	bobMaxTurns   = "3"
	bobMaxCost    = "1"
)

// ImpactAnalysis is the structured output Bob produces for a changed file.
// Every field is inferred by the LLM from the ImpactContext evidence.
type ImpactAnalysis struct {
	Summary       string   `json:"summary"`
	AffectedAreas []string `json:"affectedAreas"`
	Reasoning     []string `json:"reasoning"`
	RelevantTests []string `json:"relevantTests"`
}

// impactResponse is the combined HTTP response for POST /api/impact.
// Context carries deterministic Git/graph evidence; Analysis carries the
// LLM-inferred summary produced by Bob.
type impactResponse struct {
	Context  ImpactContext  `json:"context"`
	Analysis ImpactAnalysis `json:"analysis"`
}

// bobResult is the envelope that `bob run --format json` writes to stdout.
// Bob always produces this wrapper regardless of whether the task succeeded.
//
// Fields:
//   - Type        — always "result"
//   - Status      — "success" | "error" | "aborted"
//   - Stats       — token/cost accounting (ignored here, preserved for future use)
//   - LastMessage — the final assistant text; this is where the JSON payload lives
type bobResult struct {
	Type        string          `json:"type"`
	Status      string          `json:"status"`
	Stats       json.RawMessage `json:"stats,omitempty"`
	LastMessage string          `json:"last_message"`
}

// ImpactAnalyzer is the interface satisfied by BobRunner (and any test double).
// Analyse receives the prompt text built from ImpactContext and returns the
// parsed analysis or a typed error.
type ImpactAnalyzer interface {
	Analyse(ctx context.Context, prompt string) (ImpactAnalysis, error)
}

// FlowExplanation is Bob's plain-English walkthrough of a selection of files.
type FlowExplanation struct {
	Summary string   `json:"summary"`
	Flow    []string `json:"flow"`
}

// FlowExplainer is satisfied by BobRunner (and any test double). Explain
// receives the selection context prompt and returns Bob's explanation.
type FlowExplainer interface {
	Explain(ctx context.Context, prompt string) (FlowExplanation, error)
}

// Sentinel errors returned by BobRunner.Analyse.
var (
	// ErrBobNotFound means the bob executable was not found on PATH.
	ErrBobNotFound = errors.New("bob executable not found")
	// ErrBobNotAuthenticated means bob exited with an authentication failure.
	ErrBobNotAuthenticated = errors.New("bob is not authenticated")
	// ErrBobFailed means bob exited non-zero for a reason other than auth.
	ErrBobFailed = errors.New("bob exited with an error")
	// ErrBobTimeout means the bob process exceeded the allowed wall-clock time.
	ErrBobTimeout = errors.New("bob timed out")
	// ErrBobUnsuccessful means bob exited zero but the result envelope's status
	// field is not "success" (e.g. "error" or "aborted" due to cost/turn cap).
	ErrBobUnsuccessful = errors.New("bob task did not succeed")
	// ErrBobBadOutput means the envelope or the ImpactAnalysis payload inside it
	// could not be parsed or is structurally incomplete.
	ErrBobBadOutput = errors.New("bob returned unexpected output")
)

// BobRunner is the shell-backed ImpactAnalyzer. It forks `bob run` with the
// prompt on stdin and reads a JSON ImpactAnalysis from stdout.
//
// Authentication is inherited from the user's existing Bob session or the
// BOB_API_KEY environment variable already present in the process environment.
// BobRunner never reads, stores, or forwards secrets itself.
type BobRunner struct {
	// Workspace is the repository root passed to bob via --workspace.
	Workspace string
	// bobPath is the resolved path to the bob executable; set by newBobRunner.
	bobPath string
}

// newBobRunner resolves the bob executable on PATH and returns a ready
// BobRunner, or ErrBobNotFound if bob is not installed.
func newBobRunner(workspace string) (*BobRunner, error) {
	path, err := exec.LookPath("bob")
	if err != nil {
		return nil, fmt.Errorf("%w", ErrBobNotFound)
	}
	return &BobRunner{Workspace: workspace, bobPath: path}, nil
}

// Analyse sends prompt to bob run and parses the JSON response.
//
// Exact invocation:
//
//	bob run \
//	  --mode ask \
//	  --format json \
//	  --max-turns 3 \
//	  --max-cost 1 \
//	  --disable-mcp \
//	  --workspace <root> \
//	  -
//
// The trailing "-" tells bob to read the prompt from stdin.
// stdout captures the bob result envelope; stderr is left unredirected so
// bob's spinner/progress remains visible to the operator in the terminal.
//
// Parsing pipeline:
//  1. Unmarshal stdout as bobResult envelope.
//  2. Check envelope.Status == "success".
//  3. Extract envelope.LastMessage.
//  4. Unmarshal LastMessage as ImpactAnalysis.
//  5. Validate that ImpactAnalysis.Summary is non-empty.
func (r *BobRunner) Analyse(ctx context.Context, prompt string) (ImpactAnalysis, error) {
	payload, err := r.runJSON(ctx, buildImpactPrompt(prompt))
	if err != nil {
		return ImpactAnalysis{}, err
	}

	var analysis ImpactAnalysis
	if err := json.Unmarshal([]byte(payload), &analysis); err != nil {
		return ImpactAnalysis{}, fmt.Errorf("%w: cannot parse ImpactAnalysis from last_message: %w", ErrBobBadOutput, err)
	}

	// Step 5: structural validation — summary is the minimum required field.
	if err := validateImpactAnalysis(analysis); err != nil {
		return ImpactAnalysis{}, err
	}
	return analysis, nil
}

// runJSON sends prompt to bob run and returns the assistant's final message
// with any markdown code fence removed, ready to be unmarshalled.
func (r *BobRunner) runJSON(ctx context.Context, prompt string) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, bobRunTimeout)
	defer cancel()

	//nolint:gosec // bobPath is resolved via exec.LookPath; args are constants or trusted workspace path.
	cmd := exec.CommandContext(runCtx, r.bobPath,
		"run",
		"--mode", "ask",
		"--format", "json",
		"--max-turns", bobMaxTurns,
		"--max-cost", bobMaxCost,
		"--disable-mcp",
		"--workspace", r.Workspace,
		"-",
	)
	cmd.Stdin = bytes.NewBufferString(prompt)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	// stderr intentionally not redirected: bob writes spinner/progress there.

	if err := cmd.Run(); err != nil {
		if runCtx.Err() != nil {
			return "", fmt.Errorf("%w: %w", ErrBobTimeout, runCtx.Err())
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// Bob emits exit code 3 for authentication failures.
			if exitErr.ExitCode() == 3 {
				return "", fmt.Errorf("%w", ErrBobNotAuthenticated)
			}
		}
		return "", fmt.Errorf("%w: %w", ErrBobFailed, err)
	}

	// Step 1: parse the outer result envelope.
	var result bobResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return "", fmt.Errorf("%w: cannot parse result envelope: %w", ErrBobBadOutput, err)
	}

	// Step 2: check the task status.
	if result.Status != "success" {
		return "", fmt.Errorf("%w: status %q", ErrBobUnsuccessful, result.Status)
	}

	// Step 3: extract the assistant's reply. Bob is instructed to output only a
	// JSON object, but strip any accidental markdown code-fence wrapper.
	payload := strings.TrimSpace(result.LastMessage)
	payload = strings.TrimPrefix(payload, "```json")
	payload = strings.TrimPrefix(payload, "```")
	payload = strings.TrimSuffix(payload, "```")
	payload = strings.TrimSpace(payload)

	return payload, nil
}

// impactPromptFromContext formats the deterministic ImpactContext evidence
// into a plain-text prompt body. buildImpactPrompt then appends the schema
// instruction before the text is sent to Bob.
func impactPromptFromContext(ic ImpactContext) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Analyse the impact of a change to %q.\n\n", ic.ChangedFile)
	fmt.Fprintf(&b, "Change status: %s\n", ic.Status)
	if ic.DiffAvailable {
		truncated := ""
		if ic.DiffTruncated {
			truncated = " (truncated)"
		}
		fmt.Fprintf(&b, "Diff%s:\n```\n%s\n```\n\n", truncated, ic.Diff)
	} else {
		fmt.Fprintf(&b, "Diff: not available\n\n")
	}
	writePromptList(&b, "Callers (files that import this file)", ic.Callers)
	writePromptList(&b, "Dependencies (files this file imports)", ic.Dependencies)
	writePromptList(&b, "Related tests", ic.RelatedTests)
	return b.String()
}

func writePromptList(b *strings.Builder, label string, items []string) {
	if len(items) == 0 {
		fmt.Fprintf(b, "%s: none\n", label)
		return
	}
	fmt.Fprintf(b, "%s:\n", label)
	for _, item := range items {
		fmt.Fprintf(b, "  - %s\n", item)
	}
}

// buildImpactPrompt wraps the caller's context with an explicit schema
// instruction so Bob knows it must return only a JSON object.
func buildImpactPrompt(context string) string {
	return context + `

Respond with ONLY a valid JSON object — no prose, no markdown fences — matching this exact schema:
{
  "summary":       "<one-sentence plain-English description of the change>",
  "affectedAreas": ["<file or component impacted>", ...],
  "reasoning":     ["<reason why each area is affected>", ...],
  "relevantTests": ["<test file or test name that should be run>", ...]
}
All four fields are required. Arrays may be empty but must be present.`
}

// validateImpactAnalysis returns ErrBobBadOutput when the analysis is
// structurally incomplete. Summary is the only required non-empty field;
// the array fields may legitimately be empty slices.
func validateImpactAnalysis(a ImpactAnalysis) error {
	if strings.TrimSpace(a.Summary) == "" {
		return fmt.Errorf("%w: summary field is empty", ErrBobBadOutput)
	}
	if a.AffectedAreas == nil || a.Reasoning == nil || a.RelevantTests == nil {
		return fmt.Errorf("%w: array fields must be present (not null)", ErrBobBadOutput)
	}
	return nil
}

// Explain asks Bob for a short plain-English explanation of how the selected
// files work together.
func (r *BobRunner) Explain(ctx context.Context, prompt string) (FlowExplanation, error) {
	payload, err := r.runJSON(ctx, buildExplainPrompt(prompt))
	if err != nil {
		return FlowExplanation{}, err
	}
	var explanation FlowExplanation
	if err := json.Unmarshal([]byte(payload), &explanation); err != nil {
		return FlowExplanation{}, fmt.Errorf("%w: cannot parse FlowExplanation from last_message: %w", ErrBobBadOutput, err)
	}
	if err := validateFlowExplanation(explanation); err != nil {
		return FlowExplanation{}, err
	}
	return explanation, nil
}

// buildExplainPrompt wraps the selection context with the explanation schema.
func buildExplainPrompt(context string) string {
	return context + `

Explain how these files work together for a developer who is new to the code.
Use plain English, avoid jargon, and only describe behaviour you can see in the files above.

Respond with ONLY a valid JSON object — no prose, no markdown fences — matching this exact schema:
{
  "summary": "<2-4 sentence plain-English overview of what this part of the code does>",
  "flow":    ["<step 1: where it starts and what happens>", "<step 2>", ...]
}
Keep "flow" to at most 6 short steps, in the order the code runs.`
}

func validateFlowExplanation(e FlowExplanation) error {
	if strings.TrimSpace(e.Summary) == "" {
		return fmt.Errorf("%w: summary field is empty", ErrBobBadOutput)
	}
	if e.Flow == nil {
		return fmt.Errorf("%w: flow field must be present (not null)", ErrBobBadOutput)
	}
	return nil
}
