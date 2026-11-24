package main

import (
	"slices"
	"testing"

	"github.com/kachick/workflows/internal/workflowmeta"
)

func TestFilterEnv(t *testing.T) {
	env := []string{
		"PATH=/usr/bin",
		"GITHUB_TOKEN=secret-token",
		"GH_TOKEN=another-secret",
		"USER=runner",
	}

	got := filterEnv(env, "GITHUB_TOKEN=", "GH_TOKEN=")
	expected := []string{"PATH=/usr/bin", "USER=runner"}

	if !slices.Equal(got, expected) {
		t.Errorf("filterEnv() = %v, want %v", got, expected)
	}
}

func TestParseConfigDefaults(t *testing.T) {
	cfg, err := parseConfig([]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.BranchName != "bot-update-flake-lock" {
		t.Errorf("expected default branch-name 'bot-update-flake-lock', got %q", cfg.BranchName)
	}
	if cfg.PRTitle != "Bump flake.lock and related dependencies" {
		t.Errorf("expected default pr-title, got %q", cfg.PRTitle)
	}
	if cfg.PRBody != "" {
		t.Errorf("expected default pr-body '', got %q", cfg.PRBody)
	}
}

func TestParseConfigFlags(t *testing.T) {
	args := []string{
		"--branch-name=custom-branch",
		"--post-command=echo update",
		"--post-commit-title=custom title",
		"--pr-title=Custom PR",
		"--pr-body=Custom body",
	}

	cfg, err := parseConfig(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.BranchName != "custom-branch" {
		t.Errorf("expected 'custom-branch', got %q", cfg.BranchName)
	}
	if cfg.PostCommand != "echo update" {
		t.Errorf("expected 'echo update', got %q", cfg.PostCommand)
	}
	if cfg.PostCommitTitle != "custom title" {
		t.Errorf("expected 'custom title', got %q", cfg.PostCommitTitle)
	}
	if cfg.PRTitle != "Custom PR" {
		t.Errorf("expected 'Custom PR', got %q", cfg.PRTitle)
	}
	if cfg.PRBody != "Custom body" {
		t.Errorf("expected 'Custom body', got %q", cfg.PRBody)
	}
}

func TestParseConfigEnvFallback(t *testing.T) {
	t.Setenv("PR_BRANCH", "env-branch")
	t.Setenv("POST_COMMAND", "env-command")
	t.Setenv("PR_BODY", "env-body")

	cfg, err := parseConfig([]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.BranchName != "env-branch" {
		t.Errorf("expected branch from env, got %q", cfg.BranchName)
	}
	if cfg.PostCommand != "env-command" {
		t.Errorf("expected post-command from env, got %q", cfg.PostCommand)
	}
	if cfg.PRBody != "env-body" {
		t.Errorf("expected pr-body from env, got %q", cfg.PRBody)
	}
}

func TestBuildPRBody(t *testing.T) {
	completeMeta := workflowmeta.Metadata{
		ServerURL:          "https://github.com",
		Repository:         "kachick/workflows",
		RunID:              "12345678",
		WorkflowRepository: "kachick/workflows",
		WorkflowSHA:        "c4c03dd9c5377b99ca53183897baaf53114b86db",
		WorkflowFilePath:   ".github/workflows/reusable-bump-flake-lock.yml",
	}

	expectedHeader := "This PR was generated automatically by [actions/runs/12345678](https://github.com/kachick/workflows/actions/runs/12345678) (defined at [kachick/workflows@c4c03dd](https://github.com/kachick/workflows/blob/c4c03dd9c5377b99ca53183897baaf53114b86db/.github/workflows/reusable-bump-flake-lock.yml))."

	t.Run("complete metadata without custom body", func(t *testing.T) {
		body, err := buildPRBody("", nil, completeMeta, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if body != expectedHeader {
			t.Errorf("expected %q, got %q", expectedHeader, body)
		}
	})

	t.Run("complete metadata with default body string behaves as empty", func(t *testing.T) {
		body, err := buildPRBody("This PR was generated automatically.", nil, completeMeta, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if body != expectedHeader {
			t.Errorf("expected %q, got %q", expectedHeader, body)
		}
	})

	t.Run("complete metadata with custom body appends note", func(t *testing.T) {
		body, err := buildPRBody("Sync CI dependencies with nixpkgs", nil, completeMeta, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := expectedHeader + "\n\nSync CI dependencies with nixpkgs"
		if body != expected {
			t.Errorf("expected %q, got %q", expected, body)
		}
	})

	t.Run("complete metadata with flake.lock updates appends compare section", func(t *testing.T) {
		updates := []flakeInputUpdate{
			{
				Node:   "nixpkgs",
				Owner:  "NixOS",
				Repo:   "nixpkgs",
				OldRev: "6dadaf4375be35033e1c84395adbd53ff5fded96",
				NewRev: "abcdef1234567890abcdef1234567890abcdef12",
			},
		}
		body, err := buildPRBody("Sync CI dependencies with nixpkgs", updates, completeMeta, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := expectedHeader +
			"\n\nSync CI dependencies with nixpkgs" +
			"\n\n### Updated `flake.lock` inputs\n\n" +
			"- `nixpkgs` (`NixOS/nixpkgs`): [`6dadaf4...abcdef1`](https://github.com/NixOS/nixpkgs/compare/6dadaf4375be35033e1c84395adbd53ff5fded96...abcdef1234567890abcdef1234567890abcdef12)"
		if body != expected {
			t.Errorf("expected %q, got %q", expected, body)
		}
	})

	t.Run("incomplete metadata fails fast in CI environment", func(t *testing.T) {
		incomplete := completeMeta
		incomplete.WorkflowSHA = ""
		_, err := buildPRBody("", nil, incomplete, true)
		if err == nil {
			t.Fatal("expected error when workflow metadata is incomplete in CI, got nil")
		}
	})

	t.Run("incomplete metadata falls back to default in non-CI environment", func(t *testing.T) {
		incomplete := workflowmeta.Metadata{}
		body, err := buildPRBody("", nil, incomplete, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if body != "This PR was generated automatically." {
			t.Errorf("expected default body, got %q", body)
		}

		bodyWithCustom, err := buildPRBody("Custom note", nil, incomplete, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := "This PR was generated automatically.\n\nCustom note"
		if bodyWithCustom != expected {
			t.Errorf("expected %q, got %q", expected, bodyWithCustom)
		}
	})
}
