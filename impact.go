package main

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
)

// ErrFileNotFound is returned by buildImpactContext when the requested ID is
// not present in the snapshot's node list.
var ErrFileNotFound = errors.New("file not found in snapshot")

// ErrFileUnchanged is returned by buildImpactContext when the file is present
// in the snapshot but carries no change record.
var ErrFileUnchanged = errors.New("file has no changes to analyze")

// ImpactContext is the deterministic evidence codemap gathers for one changed
// file. Every field comes from Git or the dependency graph; nothing is inferred.
type ImpactContext struct {
	ChangedFile   string       `json:"changedFile"`
	Status        changeStatus `json:"status"`
	Diff          string       `json:"diff"`
	DiffAvailable bool         `json:"diffAvailable"`
	DiffTruncated bool         `json:"diffTruncated"`
	Callers       []string     `json:"callers"`
	Dependencies  []string     `json:"dependencies"`
	RelatedTests  []string     `json:"relatedTests"`
}

var (
	testFileSuffixPattern = regexp.MustCompile(`\.(test|spec)\.[^./]+$`)
	testFolderPattern     = regexp.MustCompile(`(^|/)(__tests__|tests?|specs?)(/|$)`)
)

func buildImpactContext(snapshot mapSnapshot, id NodeID) (ImpactContext, error) {
	nodes := mapNodesByID(snapshot.Response.Nodes)
	node, exists := nodes[id]
	if !exists {
		return ImpactContext{}, fmt.Errorf("%w: %q", ErrFileNotFound, id)
	}
	if node.Change == nil {
		return ImpactContext{}, fmt.Errorf("%w: %q", ErrFileUnchanged, id)
	}

	incoming, outgoing := mapEdgesByNode(snapshot.Response.Edges)
	diff, diffAvailable := snapshot.Diffs[id]
	context := ImpactContext{
		ChangedFile:   string(id),
		Status:        node.Change.Status,
		Diff:          diff,
		DiffAvailable: diffAvailable && diff != "",
		DiffTruncated: strings.HasSuffix(diff, diffTruncationNote),
		Callers:       nonTestNodeIDs(incoming[id]),
		Dependencies:  nonTestNodeIDs(outgoing[id]),
		RelatedTests:  relatedTestFiles(id, incoming, outgoing, nodes),
	}
	return context, nil
}

// relatedTestFiles returns test files that are directly connected to the
// changed file, directly connected to one of its callers, or named after it by
// convention (foo_test.go, foo.test.ts, foo.spec.ts, __tests__/foo.ts).
func relatedTestFiles(
	id NodeID,
	incoming, outgoing map[NodeID][]NodeID,
	nodes map[NodeID]mapNodeResponse,
) []string {
	found := make(map[NodeID]bool)
	addTests := func(candidates []NodeID) {
		for _, candidate := range candidates {
			if candidate != id && isTestFile(candidate) {
				found[candidate] = true
			}
		}
	}

	addTests(incoming[id])
	addTests(outgoing[id])
	for _, caller := range incoming[id] {
		if !isTestFile(caller) {
			addTests(incoming[caller])
		}
	}
	for _, candidate := range conventionalTestPaths(id) {
		if _, exists := nodes[candidate]; exists {
			addTests([]NodeID{candidate})
		}
	}

	tests := make([]string, 0, len(found))
	for test := range found {
		tests = append(tests, string(test))
	}
	sort.Strings(tests)
	return tests
}

func conventionalTestPaths(id NodeID) []NodeID {
	file := string(id)
	dir, base := path.Split(file)
	extension := path.Ext(base)
	stem := strings.TrimSuffix(base, extension)
	if extension == ".go" {
		return []NodeID{NodeID(dir + stem + "_test.go")}
	}
	return []NodeID{
		NodeID(dir + stem + ".test" + extension),
		NodeID(dir + stem + ".spec" + extension),
		NodeID(dir + "__tests__/" + base),
		NodeID(dir + "__tests__/" + stem + ".test" + extension),
	}
}

// isTestFile mirrors isTestFile in frontend/src/App.svelte.
func isTestFile(id NodeID) bool {
	normalized := strings.ReplaceAll(string(id), `\`, "/")
	return strings.HasSuffix(normalized, "_test.go") ||
		testFileSuffixPattern.MatchString(normalized) ||
		testFolderPattern.MatchString(normalized)
}

func nonTestNodeIDs(ids []NodeID) []string {
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		if !isTestFile(id) {
			result = append(result, string(id))
		}
	}
	return result
}
