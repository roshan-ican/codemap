package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// writeFakeBob writes a shell (or batch on Windows) script named "bob" into
// dir that prints body to stdout and exits with code. The body is written to a
// separate data file so quoting inside the script is never an issue. The
// caller must prepend dir to PATH before calling newBobRunner.
func writeFakeBob(t *testing.T, dir, body string, exitCode int) {
	t.Helper()
	// Write the response payload to a sidecar file so the script need only cat it.
	dataFile := filepath.Join(dir, "bob_output.txt")
	if err := os.WriteFile(dataFile, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		script := filepath.Join(dir, "bob.bat")
		content := fmt.Sprintf("@echo off\ntype \"%s\"\nexit /b %d\n", dataFile, exitCode)
		if err := os.WriteFile(script, []byte(content), 0700); err != nil {
			t.Fatal(err)
		}
		return
	}
	script := filepath.Join(dir, "bob")
	content := fmt.Sprintf("#!/bin/sh\ncat '%s'\nexit %d\n", dataFile, exitCode)
	if err := os.WriteFile(script, []byte(content), 0700); err != nil {
		t.Fatal(err)
	}
}

// prependPath prepends dir to the process PATH for the duration of the test.
func prependPath(t *testing.T, dir string) {
	t.Helper()
	old := os.Getenv("PATH")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+old)
}

func mustMarshal(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// goodAnalysis is the ImpactAnalysis the fake bob will report on the happy path.
var goodAnalysis = ImpactAnalysis{
	Summary:       "minor behavioural change in Foo",
	AffectedAreas: []string{"pkg/service.go"},
	Reasoning:     []string{"Foo is called by main; changed return type"},
	RelevantTests: []string{"pkg/service_test.go"},
}

// bobEnvelope wraps an ImpactAnalysis in the real bob --format json envelope.
// status should be "success", "error", or "aborted".
func bobEnvelope(t *testing.T, analysis ImpactAnalysis, status string) string {
	t.Helper()
	result := bobResult{
		Type:        "result",
		Status:      status,
		LastMessage: mustMarshal(analysis),
	}
	return mustMarshal(result)
}

// TestBobRunnerSuccessPath verifies the full happy path: envelope parsed,
// last_message decoded into ImpactAnalysis with correct values.
func TestBobRunnerSuccessPath(t *testing.T) {
	dir := t.TempDir()
	writeFakeBob(t, dir, bobEnvelope(t, goodAnalysis, "success"), 0)
	prependPath(t, dir)

	runner, err := newBobRunner(t.TempDir())
	if err != nil {
		t.Fatalf("newBobRunner: %v", err)
	}
	got, err := runner.Analyse(context.Background(), "prompt text")
	if err != nil {
		t.Fatalf("Analyse: %v", err)
	}
	if got.Summary != goodAnalysis.Summary {
		t.Fatalf("summary = %q, want %q", got.Summary, goodAnalysis.Summary)
	}
	if len(got.AffectedAreas) != 1 || got.AffectedAreas[0] != "pkg/service.go" {
		t.Fatalf("affectedAreas = %v", got.AffectedAreas)
	}
	if len(got.RelevantTests) != 1 || got.RelevantTests[0] != "pkg/service_test.go" {
		t.Fatalf("relevantTests = %v", got.RelevantTests)
	}
}

// TestBobRunnerMarkdownFenceStripped verifies that last_message wrapped in
// a markdown code fence is still decoded correctly.
func TestBobRunnerMarkdownFenceStripped(t *testing.T) {
	fenced := "```json\n" + mustMarshal(goodAnalysis) + "\n```"
	result := bobResult{Type: "result", Status: "success", LastMessage: fenced}
	dir := t.TempDir()
	writeFakeBob(t, dir, mustMarshal(result), 0)
	prependPath(t, dir)

	runner, err := newBobRunner(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	got, err := runner.Analyse(context.Background(), "prompt")
	if err != nil {
		t.Fatalf("Analyse with fenced output: %v", err)
	}
	if got.Summary != goodAnalysis.Summary {
		t.Fatalf("summary = %q", got.Summary)
	}
}

// TestBobRunnerNotFound verifies ErrBobNotFound when bob is absent from PATH.
func TestBobRunnerNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	_, err := newBobRunner(t.TempDir())
	if !errors.Is(err, ErrBobNotFound) {
		t.Fatalf("err = %v, want ErrBobNotFound", err)
	}
}

// TestBobRunnerAuthFailure verifies ErrBobNotAuthenticated on exit code 3.
func TestBobRunnerAuthFailure(t *testing.T) {
	dir := t.TempDir()
	writeFakeBob(t, dir, "", 3)
	prependPath(t, dir)

	runner, err := newBobRunner(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = runner.Analyse(context.Background(), "prompt")
	if !errors.Is(err, ErrBobNotAuthenticated) {
		t.Fatalf("err = %v, want ErrBobNotAuthenticated", err)
	}
}

// TestBobRunnerNonZeroExit verifies ErrBobFailed on a non-auth non-zero exit.
func TestBobRunnerNonZeroExit(t *testing.T) {
	dir := t.TempDir()
	writeFakeBob(t, dir, "", 1)
	prependPath(t, dir)

	runner, err := newBobRunner(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = runner.Analyse(context.Background(), "prompt")
	if !errors.Is(err, ErrBobFailed) {
		t.Fatalf("err = %v, want ErrBobFailed", err)
	}
}

// TestBobRunnerUnsuccessfulStatus verifies ErrBobUnsuccessful when the envelope
// status is not "success" even though the process exits zero.
func TestBobRunnerUnsuccessfulStatus(t *testing.T) {
	for _, status := range []string{"error", "aborted"} {
		t.Run(status, func(t *testing.T) {
			dir := t.TempDir()
			// A well-formed envelope, but status != "success".
			result := bobResult{Type: "result", Status: status, LastMessage: "{}"}
			writeFakeBob(t, dir, mustMarshal(result), 0)
			prependPath(t, dir)

			runner, err := newBobRunner(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			_, err = runner.Analyse(context.Background(), "prompt")
			if !errors.Is(err, ErrBobUnsuccessful) {
				t.Fatalf("status=%q: err = %v, want ErrBobUnsuccessful", status, err)
			}
		})
	}
}

// TestBobRunnerEnvelopeUnparseable verifies ErrBobBadOutput when stdout is not
// the expected JSON envelope (e.g. raw prose or partial output).
func TestBobRunnerEnvelopeUnparseable(t *testing.T) {
	dir := t.TempDir()
	writeFakeBob(t, dir, "plain text that is not an envelope", 0)
	prependPath(t, dir)

	runner, err := newBobRunner(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = runner.Analyse(context.Background(), "prompt")
	if !errors.Is(err, ErrBobBadOutput) {
		t.Fatalf("err = %v, want ErrBobBadOutput", err)
	}
}

// TestBobRunnerLastMessageNotJSON verifies ErrBobBadOutput when the envelope
// parses correctly but last_message is not valid ImpactAnalysis JSON.
func TestBobRunnerLastMessageNotJSON(t *testing.T) {
	dir := t.TempDir()
	result := bobResult{Type: "result", Status: "success", LastMessage: "I cannot help with that."}
	writeFakeBob(t, dir, mustMarshal(result), 0)
	prependPath(t, dir)

	runner, err := newBobRunner(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = runner.Analyse(context.Background(), "prompt")
	if !errors.Is(err, ErrBobBadOutput) {
		t.Fatalf("err = %v, want ErrBobBadOutput", err)
	}
}

// TestBobRunnerIncompleteAnalysis verifies ErrBobBadOutput when last_message is
// JSON but the ImpactAnalysis summary field is missing/empty.
func TestBobRunnerIncompleteAnalysis(t *testing.T) {
	incomplete := ImpactAnalysis{
		Summary:       "",
		AffectedAreas: []string{"main.go"},
		Reasoning:     []string{"reason"},
		RelevantTests: []string{},
	}
	dir := t.TempDir()
	writeFakeBob(t, dir, bobEnvelope(t, incomplete, "success"), 0)
	prependPath(t, dir)

	runner, err := newBobRunner(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = runner.Analyse(context.Background(), "prompt")
	if !errors.Is(err, ErrBobBadOutput) {
		t.Fatalf("err = %v, want ErrBobBadOutput", err)
	}
}

// TestBobRunnerNullArraysRejected verifies ErrBobBadOutput when last_message
// has a non-empty summary but omits one of the required array fields (null in JSON).
func TestBobRunnerNullArraysRejected(t *testing.T) {
	// Manually craft JSON where affectedAreas is omitted (decodes to nil slice).
	raw := `{"summary":"ok","reasoning":["r"],"relevantTests":["t"]}`
	result := bobResult{Type: "result", Status: "success", LastMessage: raw}
	dir := t.TempDir()
	writeFakeBob(t, dir, mustMarshal(result), 0)
	prependPath(t, dir)

	runner, err := newBobRunner(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = runner.Analyse(context.Background(), "prompt")
	if !errors.Is(err, ErrBobBadOutput) {
		t.Fatalf("err = %v, want ErrBobBadOutput", err)
	}
}

// TestBobRunnerTimeout verifies ErrBobTimeout when context is already cancelled.
func TestBobRunnerTimeout(t *testing.T) {
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		script := filepath.Join(dir, "bob.bat")
		if err := os.WriteFile(script, []byte("@echo off\ntimeout /t 30 >nul\n"), 0700); err != nil {
			t.Fatal(err)
		}
	} else {
		script := filepath.Join(dir, "bob")
		if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 30\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	prependPath(t, dir)

	runner, err := newBobRunner(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately before calling Analyse
	_, err = runner.Analyse(ctx, "prompt")
	if !errors.Is(err, ErrBobTimeout) {
		t.Fatalf("err = %v, want ErrBobTimeout", err)
	}
}

// TestImpactAnalyzerInterfaceSatisfied is a compile-time check that BobRunner
// implements ImpactAnalyzer.
func TestImpactAnalyzerInterfaceSatisfied(t *testing.T) {
	var _ ImpactAnalyzer = (*BobRunner)(nil)
}

func TestBobRunnerExplainSuccess(t *testing.T) {
	want := FlowExplanation{Summary: "The server builds a map and serves it.", Flow: []string{"main starts the server", "the UI fetches the graph"}}
	dir := t.TempDir()
	writeFakeBob(t, dir, mustMarshal(bobResult{Type: "result", Status: "success", LastMessage: mustMarshal(want)}), 0)
	prependPath(t, dir)

	runner, err := newBobRunner(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	got, err := runner.Explain(context.Background(), "selection prompt")
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if got.Summary != want.Summary || len(got.Flow) != 2 {
		t.Fatalf("explanation = %#v, want %#v", got, want)
	}
}

func TestBobRunnerExplainRejectsIncompleteOutput(t *testing.T) {
	for name, message := range map[string]string{
		"empty summary": `{"summary":"","flow":[]}`,
		"null flow":     `{"summary":"ok"}`,
		"not json":      "Here is the flow...",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeFakeBob(t, dir, mustMarshal(bobResult{Type: "result", Status: "success", LastMessage: message}), 0)
			prependPath(t, dir)
			runner, err := newBobRunner(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := runner.Explain(context.Background(), "prompt"); !errors.Is(err, ErrBobBadOutput) {
				t.Fatalf("Explain error = %v, want ErrBobBadOutput", err)
			}
		})
	}
}

func TestFlowExplainerInterfaceSatisfied(t *testing.T) {
	var _ FlowExplainer = (*BobRunner)(nil)
}

func TestBuildExplainPromptKeepsContextAndSchema(t *testing.T) {
	prompt := buildExplainPrompt("# Understand 2 selected files")
	for _, want := range []string{"# Understand 2 selected files", `"summary"`, `"flow"`, "plain English"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}
