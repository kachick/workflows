package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kachick/workflows/internal/gh"
	"github.com/kachick/workflows/internal/workflowmeta"
)

type noopChecker struct{}

func (noopChecker) Check(_ context.Context, _ string) error { return nil }

func TestParseConfigValidation(t *testing.T) {
	t.Run("rejects keep-latest <= 0 in prune mode", func(t *testing.T) {
		t.Setenv("GITHUB_TOKEN", "tok")
		if _, err := parseConfig([]string{"--mode=prune", "--owner=kachick", "--package=home", "--keep-latest=0"}); err == nil {
			t.Fatal("expected error for keep-latest=0")
		}
	})

	t.Run("rejects non-integer PR_NUMBER in staging mode", func(t *testing.T) {
		t.Setenv("GITHUB_TOKEN", "tok")
		t.Setenv("PR_NUMBER", "12.3")
		if _, err := parseConfig([]string{"--mode=staging", "--owner=kachick", "--package=home", "--repo=kachick/home"}); err == nil {
			t.Fatal("expected error for float PR_NUMBER")
		}
	})

	t.Run("rejects path traversal in package name", func(t *testing.T) {
		t.Setenv("GITHUB_TOKEN", "tok")
		if _, err := parseConfig([]string{"--mode=prune", "--owner=kachick", "--package=../other"}); err == nil {
			t.Fatal("expected error for path traversal in package name")
		}
	})
}

func TestSelectPruneTargets(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	versions := []packageVersion{
		{ID: 30, CreatedAt: base.Add(3 * time.Hour)},
		{ID: 10, CreatedAt: base.Add(1 * time.Hour)},
		{ID: 20, CreatedAt: base.Add(2 * time.Hour)},
		{ID: 40, CreatedAt: base.Add(4 * time.Hour)},
	}

	got := selectPruneTargets(versions, 2, 100)
	want := []int64{10, 20}
	if !slices.Equal(got, want) {
		t.Fatalf("selectPruneTargets() = %v, want %v", got, want)
	}

	capped := selectPruneTargets(versions, 1, 2)
	if !slices.Equal(capped, []int64{10, 20}) {
		t.Fatalf("capped selectPruneTargets() = %v, want [10 20]", capped)
	}
}

func TestSelectStagingTargetsDeduplicatesMultiTagVersions(t *testing.T) {
	var v1, v2, v3 packageVersion
	v1.ID = 101
	v1.Metadata.Container.Tags = []string{"pr-42-amd64", "pr-42-latest"}
	v2.ID = 102
	v2.Metadata.Container.Tags = []string{"pr-420-amd64", "main"}
	v3.ID = 103
	v3.Metadata.Container.Tags = nil

	got := selectStagingTargets([]packageVersion{v1, v2, v3}, 42)
	want := []int64{101}
	if !slices.Equal(got, want) {
		t.Fatalf("selectStagingTargets() = %v, want %v", got, want)
	}
}

func TestExecuteStagingFlow(t *testing.T) {
	var deletedPaths []string
	var postedBody string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.EscapedPath() == "/users/kachick/packages/container/my%2Fapp/versions":
			var v packageVersion
			v.ID = 555
			v.Metadata.Container.Tags = []string{"pr-7-amd64", "pr-7-arm64"}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]packageVersion{v})
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/users/kachick/packages/container/"):
			deletedPaths = append(deletedPaths, r.URL.EscapedPath())
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/repos/kachick/home/issues/7/comments":
			var payload map[string]string
			_ = json.NewDecoder(r.Body).Decode(&payload)
			postedBody = payload["body"]
			w.WriteHeader(http.StatusCreated)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	cfg := config{
		mode:        "staging",
		owner:       "kachick",
		packageName: "my/app",
		prNumber:    7,
		repo:        "kachick/home",
		postComment: true,
		token:       "tok",
	}
	pkgClient := &ghcrClient{Token: "tok", APIBaseURL: ts.URL, HTTPClient: ts.Client()}
	commentClient := &gh.Client{Token: "tok", APIBaseURL: ts.URL, HTTPClient: ts.Client()}
	var stdout bytes.Buffer

	if err := execute(context.Background(), cfg, pkgClient, commentClient, noopChecker{}, workflowmeta.Metadata{}, &stdout); err != nil {
		t.Fatalf("execute() error: %v", err)
	}

	if len(deletedPaths) != 1 || deletedPaths[0] != "/users/kachick/packages/container/my%2Fapp/versions/555" {
		t.Fatalf("unexpected deletedPaths: %v", deletedPaths)
	}
	if !strings.Contains(postedBody, "555") {
		t.Fatalf("expected deleted version ID in comment body, got %q", postedBody)
	}
}
