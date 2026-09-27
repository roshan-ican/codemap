package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGraphHandlerReturnsJSONWithoutAbsoluteOpenTargets(t *testing.T) {
	root := t.TempDir()
	server := &mapWebServer{
		root: root,
		snapshot: mapSnapshot{
			Response: mapResponse{
				Repository: "example",
				Revision:   4,
				Nodes: []mapNodeResponse{{
					ID:       "main.go",
					Label:    "main.go",
					Language: "Go",
					Openable: true,
				}},
			},
			OpenTargets: map[NodeID]openTarget{
				"main.go": {Path: filepath.Join(root, "main.go"), Openable: true},
			},
		},
		subscribers: make(map[chan mapEvent]struct{}),
	}
	recorder := httptest.NewRecorder()
	server.handleGraph(recorder, httptest.NewRequest(http.MethodGet, "/api/graph", nil))

	if recorder.Code != http.StatusOK || !strings.HasPrefix(recorder.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("graph response = %d %q", recorder.Code, recorder.Header())
	}
	if strings.Contains(recorder.Body.String(), root) {
		t.Fatalf("graph response leaks absolute root: %s", recorder.Body.String())
	}
	var response mapResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Revision != 4 || len(response.Nodes) != 1 || response.Nodes[0].ID != "main.go" {
		t.Fatalf("decoded graph = %#v", response)
	}
}

func TestOpenHandlerValidatesBoundaryAndUsesStoredLine(t *testing.T) {
	root := t.TempDir()
	wantPath := filepath.Join(root, "main.go")
	if err := os.WriteFile(wantPath, []byte("package main\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var openedPath string
	var openedLine int
	server := &mapWebServer{
		root: root,
		opener: func(path string, line int) error {
			openedPath, openedLine = path, line
			return nil
		},
		snapshot: mapSnapshot{OpenTargets: map[NodeID]openTarget{
			"main.go": {Path: wantPath, Line: 17, Openable: true},
		}},
		subscribers: make(map[chan mapEvent]struct{}),
	}

	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7331/api/open", strings.NewReader(`{"id":"main.go"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://127.0.0.1:7331")
	recorder := httptest.NewRecorder()
	server.handleOpen(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("open response = %d: %s", recorder.Code, recorder.Body.String())
	}
	if openedPath != wantPath || openedLine != 17 {
		t.Fatalf("opened %q:%d, want %q:17", openedPath, openedLine, wantPath)
	}
}

func TestOpenHandlerRejectsUnknownCrossOriginAndWrongContentType(t *testing.T) {
	server := &mapWebServer{
		root:        t.TempDir(),
		opener:      func(string, int) error { return nil },
		snapshot:    mapSnapshot{OpenTargets: make(map[NodeID]openTarget)},
		subscribers: make(map[chan mapEvent]struct{}),
	}

	tests := []struct {
		name        string
		contentType string
		origin      string
		body        string
		want        int
	}{
		{name: "wrong content type", contentType: "text/plain", body: `{}`, want: http.StatusUnsupportedMediaType},
		{name: "cross origin", contentType: "application/json", origin: "https://example.com", body: `{"id":"main.go"}`, want: http.StatusForbidden},
		{name: "unknown", contentType: "application/json", body: `{"id":"missing.go"}`, want: http.StatusNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7331/api/open", strings.NewReader(test.body))
			request.Header.Set("Content-Type", test.contentType)
			if test.origin != "" {
				request.Header.Set("Origin", test.origin)
			}
			recorder := httptest.NewRecorder()
			server.handleOpen(recorder, request)
			if recorder.Code != test.want {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, test.want, recorder.Body.String())
			}
		})
	}
}

func TestContextHandlerReturnsSelectionPrompt(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "main.go")
	if err := os.WriteFile(sourcePath, []byte("package main\n\nfunc main() {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	server := &mapWebServer{
		root: root,
		snapshot: mapSnapshot{
			Response: mapResponse{
				Repository: "example",
				Branch:     "main",
				Nodes: []mapNodeResponse{{
					ID:          "main.go",
					Label:       "main.go",
					Language:    "Go",
					Description: "Go source file.",
					Openable:    true,
				}},
			},
			OpenTargets: map[NodeID]openTarget{
				"main.go": {Path: sourcePath, Openable: true},
			},
		},
		subscribers: make(map[chan mapEvent]struct{}),
	}

	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7331/api/context", strings.NewReader(`{"ids":["main.go"]}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://127.0.0.1:7331")
	recorder := httptest.NewRecorder()
	server.handleContext(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("context response = %d: %s", recorder.Code, recorder.Body.String())
	}
	var response selectionContextResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.FileCount != 1 || !strings.Contains(response.Prompt, "func main") {
		t.Fatalf("context response = %#v, want one prompt containing source", response)
	}
}

func TestShouldWatchCreatedDirectorySkipsRepositoryInternalsAndBuildOutput(t *testing.T) {
	root := filepath.Clean(`C:\repo`)
	tests := []struct {
		path string
		want bool
	}{
		{path: filepath.Join(root, "internal", "newpackage"), want: true},
		{path: filepath.Join(root, ".git", "objects", "ab"), want: false},
		{path: filepath.Join(root, "frontend", "node_modules", "package"), want: false},
		{path: filepath.Join(root, "frontend", "dist", "assets"), want: false},
		{path: filepath.Join(filepath.Dir(root), "outside"), want: false},
	}
	for _, test := range tests {
		if got := shouldWatchCreatedDirectory(root, test.path); got != test.want {
			t.Errorf("shouldWatchCreatedDirectory(%q) = %t, want %t", test.path, got, test.want)
		}
	}
}

// fakeAnalyzer is a controllable ImpactAnalyzer for handler tests.
type fakeAnalyzer struct {
	analysis ImpactAnalysis
	err      error
}

func (f *fakeAnalyzer) Analyse(_ context.Context, _ string) (ImpactAnalysis, error) {
	return f.analysis, f.err
}

func impactHandlerSnapshot() mapSnapshot {
	changed := &mapChangeResponse{Status: changeModified, Additions: 2, Deletions: 1}
	return mapSnapshot{
		Response: mapResponse{
			Nodes: []mapNodeResponse{
				{ID: "src/service.go", Change: changed},
				{ID: "src/caller.go"},
				{ID: "src/service_test.go"},
				{ID: "src/clean.go"},
			},
			Edges: []mapEdgeResponse{
				{From: "src/caller.go", To: "src/service.go", Kind: EdgeKindImports},
				{From: "src/service_test.go", To: "src/service.go", Kind: EdgeKindImports},
			},
		},
		Diffs: map[NodeID]string{
			"src/service.go": "@@ -1 +1,2 @@\n func Foo() {}\n+func Bar() {}\n",
		},
	}
}

// goodFakeAnalysis is the ImpactAnalysis the fake analyzer returns on success.
var goodFakeAnalysis = ImpactAnalysis{
	Summary:       "added Bar function alongside Foo",
	AffectedAreas: []string{"src/caller.go"},
	Reasoning:     []string{"caller imports service; new export may affect callers"},
	RelevantTests: []string{"src/service_test.go"},
}

func TestImpactHandlerReturnsCombinedResponse(t *testing.T) {
	server := &mapWebServer{
		root:        t.TempDir(),
		snapshot:    impactHandlerSnapshot(),
		analyzer:    &fakeAnalyzer{analysis: goodFakeAnalysis},
		subscribers: make(map[chan mapEvent]struct{}),
	}
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7331/api/impact", strings.NewReader(`{"id":"src/service.go"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://127.0.0.1:7331")
	recorder := httptest.NewRecorder()
	server.handleImpact(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("impact response = %d: %s", recorder.Code, recorder.Body.String())
	}
	if !strings.HasPrefix(recorder.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("unexpected Content-Type: %s", recorder.Header().Get("Content-Type"))
	}
	var resp impactResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	// Deterministic context fields.
	if resp.Context.ChangedFile != "src/service.go" {
		t.Fatalf("context.changedFile = %q", resp.Context.ChangedFile)
	}
	if !resp.Context.DiffAvailable {
		t.Fatalf("context.diffAvailable = false")
	}
	if len(resp.Context.Callers) != 1 || resp.Context.Callers[0] != "src/caller.go" {
		t.Fatalf("context.callers = %v", resp.Context.Callers)
	}
	if len(resp.Context.RelatedTests) != 1 || resp.Context.RelatedTests[0] != "src/service_test.go" {
		t.Fatalf("context.relatedTests = %v", resp.Context.RelatedTests)
	}
	// Bob analysis fields.
	if resp.Analysis.Summary != goodFakeAnalysis.Summary {
		t.Fatalf("analysis.summary = %q, want %q", resp.Analysis.Summary, goodFakeAnalysis.Summary)
	}
	if len(resp.Analysis.AffectedAreas) != 1 {
		t.Fatalf("analysis.affectedAreas = %v", resp.Analysis.AffectedAreas)
	}
}

func TestImpactHandlerNoBobReturns503(t *testing.T) {
	server := &mapWebServer{
		root:        t.TempDir(),
		snapshot:    impactHandlerSnapshot(),
		analyzer:    nil, // bob not installed
		subscribers: make(map[chan mapEvent]struct{}),
	}
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7331/api/impact", strings.NewReader(`{"id":"src/service.go"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://127.0.0.1:7331")
	recorder := httptest.NewRecorder()
	server.handleImpact(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
}

func TestImpactHandlerRejectsUnknownAndUnchangedFiles(t *testing.T) {
	server := &mapWebServer{
		root:        t.TempDir(),
		snapshot:    impactHandlerSnapshot(),
		analyzer:    nil, // never reached; validation fails first
		subscribers: make(map[chan mapEvent]struct{}),
	}
	tests := []struct {
		name string
		id   string
		want int
	}{
		{name: "unknown file", id: "src/missing.go", want: http.StatusNotFound},
		{name: "unchanged file", id: "src/clean.go", want: http.StatusUnprocessableEntity},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := `{"id":"` + test.id + `"}`
			request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7331/api/impact", strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Origin", "http://127.0.0.1:7331")
			recorder := httptest.NewRecorder()
			server.handleImpact(recorder, request)
			if recorder.Code != test.want {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, test.want, recorder.Body.String())
			}
		})
	}
}

func TestImpactHandlerRejectsCrossOriginWrongContentTypeMalformedAndUnknownFields(t *testing.T) {
	server := &mapWebServer{
		root:        t.TempDir(),
		snapshot:    impactHandlerSnapshot(),
		analyzer:    nil, // never reached; validation fails first
		subscribers: make(map[chan mapEvent]struct{}),
	}
	tests := []struct {
		name        string
		contentType string
		origin      string
		body        string
		want        int
	}{
		{name: "wrong content type", contentType: "text/plain", body: `{"id":"src/service.go"}`, want: http.StatusUnsupportedMediaType},
		{name: "cross origin", contentType: "application/json", origin: "https://evil.example.com", body: `{"id":"src/service.go"}`, want: http.StatusForbidden},
		{name: "empty id", contentType: "application/json", body: `{"id":""}`, want: http.StatusBadRequest},
		{name: "missing id", contentType: "application/json", body: `{}`, want: http.StatusBadRequest},
		{name: "unknown field", contentType: "application/json", body: `{"id":"src/service.go","extra":true}`, want: http.StatusBadRequest},
		{name: "malformed json", contentType: "application/json", body: `not json`, want: http.StatusBadRequest},
		{name: "trailing content", contentType: "application/json", body: `{"id":"src/service.go"}{}`, want: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7331/api/impact", strings.NewReader(test.body))
			request.Header.Set("Content-Type", test.contentType)
			if test.origin != "" {
				request.Header.Set("Origin", test.origin)
			}
			recorder := httptest.NewRecorder()
			server.handleImpact(recorder, request)
			if recorder.Code != test.want {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, test.want, recorder.Body.String())
			}
		})
	}
}

func TestImpactHandlerBobErrors(t *testing.T) {
	validBody := `{"id":"src/service.go"}`
	makeRequest := func() *http.Request {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7331/api/impact", strings.NewReader(validBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://127.0.0.1:7331")
		return req
	}
	tests := []struct {
		name     string
		analyErr error
		want     int
	}{
		{name: "not authenticated", analyErr: ErrBobNotAuthenticated, want: http.StatusUnauthorized},
		{name: "timeout", analyErr: ErrBobTimeout, want: http.StatusGatewayTimeout},
		{name: "unsuccessful", analyErr: ErrBobUnsuccessful, want: http.StatusBadGateway},
		{name: "bad output", analyErr: ErrBobBadOutput, want: http.StatusBadGateway},
		{name: "failed", analyErr: ErrBobFailed, want: http.StatusBadGateway},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := &mapWebServer{
				root:        t.TempDir(),
				snapshot:    impactHandlerSnapshot(),
				analyzer:    &fakeAnalyzer{err: test.analyErr},
				subscribers: make(map[chan mapEvent]struct{}),
			}
			recorder := httptest.NewRecorder()
			server.handleImpact(recorder, makeRequest())
			if recorder.Code != test.want {
				t.Fatalf("analyErr=%v: status = %d, want %d: %s", test.analyErr, recorder.Code, test.want, recorder.Body.String())
			}
		})
	}
}

// fakeExplainer is a controllable FlowExplainer for handler tests.
type fakeExplainer struct {
	explanation FlowExplanation
	err         error
	prompt      string
}

func (f *fakeExplainer) Explain(_ context.Context, prompt string) (FlowExplanation, error) {
	f.prompt = prompt
	return f.explanation, f.err
}

func explainRequest(body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7331/api/explain", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://127.0.0.1:7331")
	return request
}

func TestExplainHandlerSendsSelectionContextToBob(t *testing.T) {
	explainer := &fakeExplainer{explanation: FlowExplanation{Summary: "service handles requests", Flow: []string{"caller calls service"}}}
	server := &mapWebServer{
		root:        t.TempDir(),
		snapshot:    impactHandlerSnapshot(),
		explainer:   explainer,
		subscribers: make(map[chan mapEvent]struct{}),
	}
	recorder := httptest.NewRecorder()
	server.handleExplain(recorder, explainRequest(`{"ids":["src/service.go","src/caller.go"]}`))

	if recorder.Code != http.StatusOK {
		t.Fatalf("explain response = %d: %s", recorder.Code, recorder.Body.String())
	}
	var got FlowExplanation
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Summary != "service handles requests" || len(got.Flow) != 1 {
		t.Fatalf("explanation = %#v", got)
	}
	if !strings.Contains(explainer.prompt, "## src/service.go") || !strings.Contains(explainer.prompt, "## src/caller.go") {
		t.Fatalf("prompt did not include the selection context:\n%s", explainer.prompt)
	}
}

func TestExplainHandlerErrors(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		explainer FlowExplainer
		want      int
	}{
		{name: "no bob", body: `{"ids":["src/service.go"]}`, explainer: nil, want: http.StatusServiceUnavailable},
		{name: "empty ids", body: `{"ids":[]}`, explainer: &fakeExplainer{}, want: http.StatusBadRequest},
		{name: "unknown field", body: `{"ids":["src/service.go"],"x":1}`, explainer: &fakeExplainer{}, want: http.StatusBadRequest},
		{name: "unknown file", body: `{"ids":["missing.go"]}`, explainer: &fakeExplainer{}, want: http.StatusBadRequest},
		{name: "not authenticated", body: `{"ids":["src/service.go"]}`, explainer: &fakeExplainer{err: ErrBobNotAuthenticated}, want: http.StatusUnauthorized},
		{name: "timeout", body: `{"ids":["src/service.go"]}`, explainer: &fakeExplainer{err: ErrBobTimeout}, want: http.StatusGatewayTimeout},
		{name: "bad output", body: `{"ids":["src/service.go"]}`, explainer: &fakeExplainer{err: ErrBobBadOutput}, want: http.StatusBadGateway},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := &mapWebServer{
				root:        t.TempDir(),
				snapshot:    impactHandlerSnapshot(),
				explainer:   test.explainer,
				subscribers: make(map[chan mapEvent]struct{}),
			}
			recorder := httptest.NewRecorder()
			server.handleExplain(recorder, explainRequest(test.body))
			if recorder.Code != test.want {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, test.want, recorder.Body.String())
			}
		})
	}
}

func TestExplainHandlerRejectsCrossOrigin(t *testing.T) {
	server := &mapWebServer{root: t.TempDir(), snapshot: impactHandlerSnapshot(), explainer: &fakeExplainer{}}
	request := explainRequest(`{"ids":["src/service.go"]}`)
	request.Header.Set("Origin", "http://evil.example")
	recorder := httptest.NewRecorder()
	server.handleExplain(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", recorder.Code)
	}
}
