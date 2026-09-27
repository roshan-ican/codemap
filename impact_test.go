package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func impactTestSnapshot() mapSnapshot {
	changed := &mapChangeResponse{Status: changeModified, Additions: 1, Deletions: 1}
	return mapSnapshot{
		Response: mapResponse{
			Nodes: []mapNodeResponse{
				{ID: "src/user_service.ts", Change: changed},
				{ID: "src/auth_service.ts"},
				{ID: "src/login_controller.ts"},
				{ID: "src/db.ts"},
				{ID: "src/user_service.test.ts"},
				{ID: "src/auth_service.spec.ts"},
				{ID: "tests/login_flow.ts"},
				{ID: "src/unrelated.ts"},
				{ID: "src/unrelated.test.ts"},
				{ID: "src/clean.ts"},
			},
			Edges: []mapEdgeResponse{
				{From: "src/auth_service.ts", To: "src/user_service.ts", Kind: EdgeKindImports},
				{From: "src/login_controller.ts", To: "src/auth_service.ts", Kind: EdgeKindImports},
				{From: "src/user_service.ts", To: "src/db.ts", Kind: EdgeKindImports},
				{From: "src/auth_service.spec.ts", To: "src/auth_service.ts", Kind: EdgeKindImports},
				{From: "tests/login_flow.ts", To: "src/user_service.ts", Kind: EdgeKindImports},
				{From: "src/unrelated.test.ts", To: "src/unrelated.ts", Kind: EdgeKindImports},
			},
		},
		Diffs: map[NodeID]string{
			"src/user_service.ts": "@@ -3 +3 @@\n-  return null;\n+  throw new Error('missing');\n",
		},
	}
}

func TestBuildImpactContextCollectsDiffAndRelationships(t *testing.T) {
	got, err := buildImpactContext(impactTestSnapshot(), "src/user_service.ts")
	if err != nil {
		t.Fatal(err)
	}
	want := ImpactContext{
		ChangedFile:   "src/user_service.ts",
		Status:        changeModified,
		Diff:          "@@ -3 +3 @@\n-  return null;\n+  throw new Error('missing');\n",
		DiffAvailable: true,
		Callers:       []string{"src/auth_service.ts"},
		Dependencies:  []string{"src/db.ts"},
		RelatedTests: []string{
			"src/auth_service.spec.ts",
			"src/user_service.test.ts",
			"tests/login_flow.ts",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("impact context =\n%#v\nwant\n%#v", got, want)
	}
}

func TestBuildImpactContextHandlesMissingDiff(t *testing.T) {
	snapshot := impactTestSnapshot()
	snapshot.Diffs = nil
	got, err := buildImpactContext(snapshot, "src/user_service.ts")
	if err != nil {
		t.Fatal(err)
	}
	if got.DiffAvailable || got.Diff != "" || got.DiffTruncated {
		t.Fatalf("missing diff = %#v, want unavailable empty diff", got)
	}
	if len(got.Callers) != 1 {
		t.Fatalf("callers = %v, want relationships even without a diff", got.Callers)
	}
}

func TestBuildImpactContextReportsTruncatedDiff(t *testing.T) {
	snapshot := impactTestSnapshot()
	snapshot.Diffs["src/user_service.ts"] = "+a\n" + diffTruncationNote
	got, err := buildImpactContext(snapshot, "src/user_service.ts")
	if err != nil {
		t.Fatal(err)
	}
	if !got.DiffAvailable || !got.DiffTruncated {
		t.Fatalf("truncated diff = %#v, want available and truncated", got)
	}
}

func TestBuildImpactContextRejectsUnknownAndUnchangedFiles(t *testing.T) {
	snapshot := impactTestSnapshot()
	for _, id := range []NodeID{"src/missing.ts", "src/clean.ts"} {
		if _, err := buildImpactContext(snapshot, id); err == nil {
			t.Fatalf("buildImpactContext(%q) returned no error", id)
		}
	}
}

func TestBuildImpactContextUsesEmptyListsNotNil(t *testing.T) {
	snapshot := mapSnapshot{Response: mapResponse{Nodes: []mapNodeResponse{
		{ID: "lonely.go", Change: &mapChangeResponse{Status: changeAdded}},
	}}}
	got, err := buildImpactContext(snapshot, "lonely.go")
	if err != nil {
		t.Fatal(err)
	}
	if got.Callers == nil || got.Dependencies == nil || got.RelatedTests == nil {
		t.Fatalf("lists must be non-nil for stable JSON: %#v", got)
	}
}

func TestRelatedTestsUseGoNamingConvention(t *testing.T) {
	snapshot := mapSnapshot{Response: mapResponse{Nodes: []mapNodeResponse{
		{ID: "pkg/store.go", Change: &mapChangeResponse{Status: changeModified}},
		{ID: "pkg/store_test.go"},
		{ID: "pkg/other_test.go"},
	}}}
	got, err := buildImpactContext(snapshot, "pkg/store.go")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.RelatedTests, []string{"pkg/store_test.go"}) {
		t.Fatalf("related tests = %v, want pkg/store_test.go only", got.RelatedTests)
	}
}

func TestIsTestFileMatchesFrontendRules(t *testing.T) {
	cases := map[NodeID]bool{
		"map_api_test.go":          true,
		"src/a.test.ts":            true,
		"src/a.spec.jsx":           true,
		"src/__tests__/a.ts":       true,
		"tests/helpers.go":         true,
		"spec/a.js":                true,
		"src/attest.ts":            false,
		"src/testing/utils.ts":     false,
		"src/latest.go":            false,
		`src\__tests__\windows.ts`: true,
	}
	for id, want := range cases {
		if got := isTestFile(id); got != want {
			t.Errorf("isTestFile(%q) = %v, want %v", id, got, want)
		}
	}
}

func TestBuildImpactContextOnRealSnapshot(t *testing.T) {
	root, repo, worktree := newTestRepoWithChangedGoFile(t)
	snapshot, err := buildMapSnapshot(root, repo, worktree, mapOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := buildImpactContext(snapshot, "app.go")
	if err != nil {
		t.Fatal(err)
	}
	if !got.DiffAvailable || !strings.Contains(got.Diff, "+\tprintln(2)") {
		t.Fatalf("real diff = %q, want stored snapshot diff", got.Diff)
	}
	if !reflect.DeepEqual(got.Callers, []string{"main.go"}) {
		t.Fatalf("callers = %v, want [main.go]", got.Callers)
	}
	if !reflect.DeepEqual(got.RelatedTests, []string{"app_test.go"}) {
		t.Fatalf("related tests = %v, want [app_test.go]", got.RelatedTests)
	}
}

func newTestRepoWithChangedGoFile(t *testing.T) (string, *git.Repository, *git.Worktree) {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"app.go":      "package main\n\nfunc Run() {\n\tprintln(1)\n}\n",
		"main.go":     "package main\n\nfunc main() {\n\tRun()\n}\n",
		"app_test.go": "package main\n\nimport \"testing\"\n\nfunc TestRun(t *testing.T) {}\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	repo, err := git.PlainInit(root, false)
	if err != nil {
		t.Fatal(err)
	}
	worktree, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	for name := range files {
		if _, err := worktree.Add(name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := worktree.Commit("initial", &git.CommitOptions{
		Author: &object.Signature{Name: "tester", Email: "tester@example.com", When: time.Now()},
	}); err != nil {
		t.Fatal(err)
	}
	changed := "package main\n\nfunc Run() {\n\tprintln(2)\n}\n"
	if err := os.WriteFile(filepath.Join(root, "app.go"), []byte(changed), 0600); err != nil {
		t.Fatal(err)
	}
	return root, repo, worktree
}
