package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseRepoEnv(t *testing.T) {
	t.Run("splits GITHUB_REPOSITORY", func(t *testing.T) {
		t.Setenv("GITHUB_REPOSITORY", "kachick/workflows")
		t.Setenv("GITHUB_REPOSITORY_OWNER", "ignored")
		owner, repo := parseRepoEnv()
		if owner != "kachick" || repo != "workflows" {
			t.Errorf("expected kachick/workflows, got %s/%s", owner, repo)
		}
	})

	t.Run("falls back to GITHUB_REPOSITORY_OWNER when GITHUB_REPOSITORY is malformed", func(t *testing.T) {
		t.Setenv("GITHUB_REPOSITORY", "invalid")
		t.Setenv("GITHUB_REPOSITORY_OWNER", "kachick")
		owner, repo := parseRepoEnv()
		if owner != "kachick" || repo != "" {
			t.Errorf("expected kachick/, got %s/%s", owner, repo)
		}
	})
}

func TestRunValidation(t *testing.T) {
	t.Run("rejects unsafe event in GitHub Actions", func(t *testing.T) {
		t.Setenv("GITHUB_ACTIONS", "true")
		t.Setenv("GITHUB_REPOSITORY_OWNER", "kachick")
		t.Setenv("GITHUB_EVENT_NAME", "pull_request_target")

		var stdout, stderr bytes.Buffer
		code := run([]string{"--marker", "m"}, &stdout, &stderr)
		if code != 1 || !strings.Contains(stderr.String(), "pull_request_target") {
			t.Fatalf("code=%d stderr=%q", code, stderr.String())
		}
	})

	t.Run("rejects invalid PR_NUMBER", func(t *testing.T) {
		t.Setenv("GITHUB_ACTIONS", "false")
		t.Setenv("PR_NUMBER", "not-a-number")

		var stdout, stderr bytes.Buffer
		code := run([]string{"--marker", "m"}, &stdout, &stderr)
		if code != 1 || !strings.Contains(stderr.String(), "invalid PR_NUMBER") {
			t.Fatalf("code=%d stderr=%q", code, stderr.String())
		}
	})

	t.Run("rejects missing marker", func(t *testing.T) {
		t.Setenv("GITHUB_ACTIONS", "false")
		t.Setenv("PR_NUMBER", "")

		var stdout, stderr bytes.Buffer
		code := run(nil, &stdout, &stderr)
		if code != 1 || !strings.Contains(stderr.String(), "--marker is required") {
			t.Fatalf("code=%d stderr=%q", code, stderr.String())
		}
	})

	t.Run("uses GH_TOKEN fallback when GITHUB_TOKEN is empty", func(t *testing.T) {
		t.Setenv("GITHUB_TOKEN", "")
		t.Setenv("GH_TOKEN", "fallback-token")
		if got := defaultTokenEnv(); got != "fallback-token" {
			t.Errorf("defaultTokenEnv() = %q, want fallback-token", got)
		}
	})
}
