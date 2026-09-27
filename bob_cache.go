package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

const maxBobCacheEntries = 64

// bobCacheEntry holds one successful Bob answer. Exactly one field is set.
type bobCacheEntry struct {
	Impact  *ImpactAnalysis  `json:"impact,omitempty"`
	Explain *FlowExplanation `json:"explain,omitempty"`
}

type bobCacheFile struct {
	Order   []string                 `json:"order"`
	Entries map[string]bobCacheEntry `json:"entries"`
}

// bobCache wraps an ImpactAnalyzer and FlowExplainer and remembers successful
// answers, keyed by a hash of the exact prompt. The prompt contains the diff,
// source and graph evidence, so any code change produces a new key and a fresh
// Bob call; repeated requests for unchanged code are instant and cost nothing.
// Errors are never cached. When path is set, answers are persisted there so
// they survive a restart.
type bobCache struct {
	analyzer  ImpactAnalyzer
	explainer FlowExplainer
	path      string

	mu      sync.Mutex
	entries map[string]bobCacheEntry
	order   []string
}

func newBobCache(analyzer ImpactAnalyzer, explainer FlowExplainer, path string) *bobCache {
	cache := &bobCache{
		analyzer:  analyzer,
		explainer: explainer,
		path:      path,
		entries:   make(map[string]bobCacheEntry),
	}
	cache.load()
	return cache
}

// bobCachePath stores the cache inside .git so it is per-repository and can
// never be committed. It returns "" (memory only) when .git is not a directory.
func bobCachePath(root string) string {
	gitDir := filepath.Join(root, ".git")
	if info, err := os.Stat(gitDir); err != nil || !info.IsDir() {
		return ""
	}
	return filepath.Join(gitDir, "codemap-bob-cache.json")
}

func (cache *bobCache) Analyse(ctx context.Context, prompt string) (ImpactAnalysis, error) {
	key := bobCacheKey("impact", prompt)
	if entry, ok := cache.get(key); ok && entry.Impact != nil {
		return *entry.Impact, nil
	}
	analysis, err := cache.analyzer.Analyse(ctx, prompt)
	if err != nil {
		return ImpactAnalysis{}, err
	}
	cache.put(key, bobCacheEntry{Impact: &analysis})
	return analysis, nil
}

func (cache *bobCache) Explain(ctx context.Context, prompt string) (FlowExplanation, error) {
	key := bobCacheKey("explain", prompt)
	if entry, ok := cache.get(key); ok && entry.Explain != nil {
		return *entry.Explain, nil
	}
	explanation, err := cache.explainer.Explain(ctx, prompt)
	if err != nil {
		return FlowExplanation{}, err
	}
	cache.put(key, bobCacheEntry{Explain: &explanation})
	return explanation, nil
}

// cached reports whether a successful answer for this kind and prompt is
// already stored, so handlers can tell the UI it came from the cache.
func (cache *bobCache) cached(kind, prompt string) bool {
	_, ok := cache.get(bobCacheKey(kind, prompt))
	return ok
}

func (cache *bobCache) get(key string) (bobCacheEntry, bool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	entry, ok := cache.entries[key]
	return entry, ok
}

func (cache *bobCache) put(key string, entry bobCacheEntry) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if _, exists := cache.entries[key]; !exists {
		cache.order = append(cache.order, key)
	}
	cache.entries[key] = entry
	for len(cache.order) > maxBobCacheEntries {
		delete(cache.entries, cache.order[0])
		cache.order = cache.order[1:]
	}
	cache.saveLocked()
}

// load reads a previously saved cache; a missing or corrupt file just means an
// empty cache.
func (cache *bobCache) load() {
	if cache.path == "" {
		return
	}
	data, err := os.ReadFile(cache.path)
	if err != nil {
		return
	}
	var file bobCacheFile
	if json.Unmarshal(data, &file) != nil {
		return
	}
	for _, key := range file.Order {
		if entry, ok := file.Entries[key]; ok {
			cache.entries[key] = entry
			cache.order = append(cache.order, key)
		}
	}
}

// saveLocked writes the cache best-effort; a failed write only costs speed.
func (cache *bobCache) saveLocked() {
	if cache.path == "" {
		return
	}
	data, err := json.Marshal(bobCacheFile{Order: cache.order, Entries: cache.entries})
	if err != nil {
		return
	}
	temp := cache.path + ".tmp"
	if os.WriteFile(temp, data, 0600) == nil {
		_ = os.Rename(temp, cache.path)
	}
}

func bobCacheKey(kind, prompt string) string {
	sum := sha256.Sum256([]byte(kind + "\x00" + prompt))
	return hex.EncodeToString(sum[:])
}
