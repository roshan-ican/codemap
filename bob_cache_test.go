package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

type countingBob struct {
	impactCalls  int
	explainCalls int
	err          error
}

func (c *countingBob) Analyse(_ context.Context, prompt string) (ImpactAnalysis, error) {
	c.impactCalls++
	if c.err != nil {
		return ImpactAnalysis{}, c.err
	}
	return ImpactAnalysis{Summary: "impact of " + prompt, AffectedAreas: []string{}, Reasoning: []string{}, RelevantTests: []string{}}, nil
}

func (c *countingBob) Explain(_ context.Context, prompt string) (FlowExplanation, error) {
	c.explainCalls++
	if c.err != nil {
		return FlowExplanation{}, c.err
	}
	return FlowExplanation{Summary: "flow of " + prompt, Flow: []string{"step"}}, nil
}

func TestBobCacheReusesAnswerForSamePrompt(t *testing.T) {
	bob := &countingBob{}
	cache := newBobCache(bob, bob, "")
	ctx := context.Background()

	first, _ := cache.Analyse(ctx, "diff A")
	if cache.cached("impact", "diff A") != true {
		t.Fatal("answer was not cached")
	}
	second, _ := cache.Analyse(ctx, "diff A")
	if bob.impactCalls != 1 || first.Summary != second.Summary {
		t.Fatalf("calls = %d, want 1 (second answer from cache)", bob.impactCalls)
	}
	if _, err := cache.Analyse(ctx, "diff B"); err != nil || bob.impactCalls != 2 {
		t.Fatalf("changed prompt must call Bob again: calls = %d, err = %v", bob.impactCalls, err)
	}
}

func TestBobCacheSeparatesImpactAndExplain(t *testing.T) {
	bob := &countingBob{}
	cache := newBobCache(bob, bob, "")
	ctx := context.Background()
	if _, err := cache.Analyse(ctx, "same prompt"); err != nil {
		t.Fatal(err)
	}
	got, err := cache.Explain(ctx, "same prompt")
	if err != nil || bob.explainCalls != 1 || got.Summary != "flow of same prompt" {
		t.Fatalf("explain must not reuse the impact answer: %#v, calls = %d, err = %v", got, bob.explainCalls, err)
	}
}

func TestBobCacheNeverStoresErrors(t *testing.T) {
	bob := &countingBob{err: ErrBobTimeout}
	cache := newBobCache(bob, bob, "")
	for i := 0; i < 2; i++ {
		if _, err := cache.Analyse(context.Background(), "p"); !errors.Is(err, ErrBobTimeout) {
			t.Fatalf("err = %v, want ErrBobTimeout", err)
		}
	}
	if bob.impactCalls != 2 || cache.cached("impact", "p") {
		t.Fatalf("errors were cached: calls = %d", bob.impactCalls)
	}
}

func TestBobCachePersistsAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	bob := &countingBob{}
	if _, err := newBobCache(bob, bob, path).Explain(context.Background(), "selection"); err != nil {
		t.Fatal(err)
	}

	restarted := &countingBob{}
	got, err := newBobCache(restarted, restarted, path).Explain(context.Background(), "selection")
	if err != nil || restarted.explainCalls != 0 || got.Summary != "flow of selection" {
		t.Fatalf("after restart: %#v, calls = %d, err = %v", got, restarted.explainCalls, err)
	}
}

func TestBobCacheEvictsOldestEntries(t *testing.T) {
	bob := &countingBob{}
	cache := newBobCache(bob, bob, "")
	for i := 0; i <= maxBobCacheEntries; i++ {
		if _, err := cache.Analyse(context.Background(), string(rune('a'+i%26))+string(rune(i))); err != nil {
			t.Fatal(err)
		}
	}
	if len(cache.entries) != maxBobCacheEntries || len(cache.order) != maxBobCacheEntries {
		t.Fatalf("entries = %d, order = %d, want %d", len(cache.entries), len(cache.order), maxBobCacheEntries)
	}
}

func TestBobCachePathLivesInsideGitDir(t *testing.T) {
	root, _, _ := newTestRepoWithChangedGoFile(t)
	if got := bobCachePath(root); got != filepath.Join(root, ".git", "codemap-bob-cache.json") {
		t.Fatalf("cache path = %q", got)
	}
	if got := bobCachePath(t.TempDir()); got != "" {
		t.Fatalf("non-repo cache path = %q, want memory only", got)
	}
}
