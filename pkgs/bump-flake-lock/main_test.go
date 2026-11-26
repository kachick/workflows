package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/kachick/workflows/internal/leakcheck"
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
	if len(cfg.FlakeInputs) != 0 {
		t.Errorf("expected default flake-inputs to be empty, got %v", cfg.FlakeInputs)
	}
}

func TestParseConfigFlags(t *testing.T) {
	args := []string{
		"--branch-name=custom-branch",
		"--flake-inputs=nixpkgs, home-manager",
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
	if !slices.Equal(cfg.FlakeInputs, []string{"nixpkgs", "home-manager"}) {
		t.Errorf("expected [nixpkgs home-manager], got %v", cfg.FlakeInputs)
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
	t.Setenv("FLAKE_INPUTS", "nixpkgs")
	t.Setenv("POST_COMMAND", "env-command")
	t.Setenv("PR_BODY", "env-body")

	cfg, err := parseConfig([]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.BranchName != "env-branch" {
		t.Errorf("expected branch from env, got %q", cfg.BranchName)
	}
	if !slices.Equal(cfg.FlakeInputs, []string{"nixpkgs"}) {
		t.Errorf("expected flake-inputs from env, got %v", cfg.FlakeInputs)
	}
	if cfg.PostCommand != "env-command" {
		t.Errorf("expected post-command from env, got %q", cfg.PostCommand)
	}
	if cfg.PRBody != "env-body" {
		t.Errorf("expected pr-body from env, got %q", cfg.PRBody)
	}
}

func TestParseConfigFlagPrecedenceAndValidation(t *testing.T) {
	t.Run("explicit default CLI flag overrides env var", func(t *testing.T) {
		t.Setenv("PR_BRANCH", "env-branch")
		t.Setenv("PR_TITLE", "env-title")

		cfg, err := parseConfig([]string{
			"--branch-name=bot-update-flake-lock",
			"--pr-title=Bump flake.lock and related dependencies",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.BranchName != "bot-update-flake-lock" {
			t.Errorf("expected CLI branch-name, got %q", cfg.BranchName)
		}
		if cfg.PRTitle != "Bump flake.lock and related dependencies" {
			t.Errorf("expected CLI pr-title, got %q", cfg.PRTitle)
		}
	})

	t.Run("rejects branch name starting with dash", func(t *testing.T) {
		if _, err := parseConfig([]string{"--branch-name=-bad"}); err == nil {
			t.Fatal("expected error for branch-name starting with '-', got nil")
		}
	})
}

type fakeBumpRunner struct {
	outputs map[string]string
	runErrs map[string]error
	calls   []string
}

func (f *fakeBumpRunner) Run(_ context.Context, _ []string, name string, args ...string) error {
	key := fmt.Sprintf("%s %s", name, strings.Join(args, " "))
	f.calls = append(f.calls, key)
	if err, ok := f.runErrs[key]; ok {
		return err
	}
	return nil
}

func (f *fakeBumpRunner) Output(_ context.Context, _ []string, name string, args ...string) (string, error) {
	key := fmt.Sprintf("%s %s", name, strings.Join(args, " "))
	f.calls = append(f.calls, key)
	if out, ok := f.outputs[key]; ok {
		return out, nil
	}
	return "", fmt.Errorf("no fake output for %q", key)
}

type fakeLeakChecker struct {
	err error
}

func (f fakeLeakChecker) Check(_ context.Context, _ string) error {
	return f.err
}

func TestExecuteWorkflows(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "false")

	t.Run("stops early when no commits were added and uses initial branch fallback", func(t *testing.T) {
		runner := &fakeBumpRunner{
			outputs: map[string]string{
				"git rev-parse --abbrev-ref HEAD":      "master",
				"git rev-list --count origin/master..": "0",
			},
		}
		cfg := &Config{BranchName: "bot-update-flake-lock", PRTitle: "Bump"}
		if err := execute(context.Background(), cfg, runner, fakeLeakChecker{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, c := range runner.calls {
			if strings.HasPrefix(c, "git push") || strings.HasPrefix(c, "gh ") {
				t.Errorf("should not push or invoke gh when count == 0, saw %q", c)
			}
		}
	})

	t.Run("aborts before push when leakcheck fails", func(t *testing.T) {
		runner := &fakeBumpRunner{
			outputs: map[string]string{
				"git rev-parse --abbrev-ref HEAD":    "main",
				"git rev-list --count origin/main..": "1",
				"git diff origin/main..":             "diff content",
				"git log --format=%B origin/main..":  "commit msg",
			},
		}
		cfg := &Config{BranchName: "bot-update-flake-lock", PRTitle: "Bump"}
		err := execute(context.Background(), cfg, runner, fakeLeakChecker{err: leakcheck.ErrLeakDetected})
		if !errors.Is(err, leakcheck.ErrLeakDetected) {
			t.Fatalf("expected ErrLeakDetected, got %v", err)
		}
		for _, c := range runner.calls {
			if strings.HasPrefix(c, "git push") || strings.HasPrefix(c, "gh ") {
				t.Errorf("must not push or call gh after leak check failure, saw %q", c)
			}
		}
	})

	t.Run("creates PR when none is open and edits PR when already open", func(t *testing.T) {
		for _, tc := range []struct {
			name     string
			prState  string
			wantCall string
		}{
			{"create new PR", "", "gh pr create"},
			{"edit open PR", "OPEN", "gh pr edit"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				runner := &fakeBumpRunner{
					outputs: map[string]string{
						"git rev-parse --abbrev-ref HEAD":                           "main",
						"git rev-list --count origin/main..":                        "1",
						"git diff origin/main..":                                    "diff",
						"git log --format=%B origin/main..":                         "msg",
						"gh pr view bot-update-flake-lock --json state --jq .state": tc.prState,
					},
				}
				cfg := &Config{BranchName: "bot-update-flake-lock", PRTitle: "Bump"}
				if err := execute(context.Background(), cfg, runner, fakeLeakChecker{}); err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				found := false
				for _, c := range runner.calls {
					if strings.HasPrefix(c, tc.wantCall) {
						found = true
					}
				}
				if !found {
					t.Errorf("expected call starting with %q, got calls: %v", tc.wantCall, runner.calls)
				}
			})
		}
	})
}

func TestParseFlakeInputs(t *testing.T) {
	t.Run("rejects option-like input names", func(t *testing.T) {
		if _, err := parseFlakeInputs("nixpkgs --override-input"); err == nil {
			t.Fatal("expected error for input starting with '-', got nil")
		}
	})

	t.Run("builds nix flake update arguments", func(t *testing.T) {
		allArgs := buildUpdateArgs(nil)
		if !slices.Equal(allArgs, []string{"flake", "update", "--commit-lock-file"}) {
			t.Errorf("unexpected args for full update: %v", allArgs)
		}

		subsetArgs := buildUpdateArgs([]string{"nixpkgs", "home-manager"})
		if !slices.Equal(subsetArgs, []string{"flake", "update", "--commit-lock-file", "nixpkgs", "home-manager"}) {
			t.Errorf("unexpected args for subset update: %v", subsetArgs)
		}
	})
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
