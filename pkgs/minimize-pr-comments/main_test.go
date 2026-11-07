package main

import "testing"

func TestParseRepoEnv(t *testing.T) {
	t.Setenv("GITHUB_REPOSITORY", "kachick/workflows")
	owner, repo := parseRepoEnv()
	if owner != "kachick" || repo != "workflows" {
		t.Errorf("expected kachick/workflows, got %s/%s", owner, repo)
	}
}
