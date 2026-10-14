package main

import (
	"testing"
)

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
	if cfg.DryRun != false {
		t.Errorf("expected default dry-run false, got %v", cfg.DryRun)
	}
}

func TestParseConfigFlags(t *testing.T) {
	args := []string{
		"--branch-name=custom-branch",
		"--post-command=echo update",
		"--post-commit-title=custom title",
		"--pr-title=Custom PR",
		"--pr-body=Custom body",
		"--dry-run=true",
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
	if cfg.DryRun != true {
		t.Errorf("expected dry-run true, got %v", cfg.DryRun)
	}
}

func TestParseConfigEnvFallback(t *testing.T) {
	t.Setenv("PR_BRANCH", "env-branch")
	t.Setenv("POST_COMMAND", "env-command")
	t.Setenv("DRY_RUN", "true")

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
	if cfg.DryRun != true {
		t.Errorf("expected dry-run from env, got %v", cfg.DryRun)
	}
}
