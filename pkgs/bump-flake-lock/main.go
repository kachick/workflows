package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/kachick/workflows/internal/leakcheck"
	"github.com/kachick/workflows/internal/workflowmeta"
)

type Config struct {
	BranchName      string
	PostCommand     string
	PostCommitTitle string
	PRTitle         string
	PRBody          string
	GitUserName     string
	GitUserEmail    string
}

func buildPRBody(customBody string, meta workflowmeta.Metadata, isCI bool) (string, error) {
	if !meta.IsComplete() && isCI {
		return "", fmt.Errorf("missing required workflow metadata in CI environment: %+v", meta)
	}
	header := meta.ProvenanceText("This PR was generated automatically")

	cleanCustom := strings.TrimSpace(customBody)
	if cleanCustom == "" || cleanCustom == "This PR was generated automatically." {
		return header, nil
	}

	return header + "\n\n" + cleanCustom, nil
}

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if err := workflowmeta.VerifyEnv(); err != nil {
		return err
	}

	cfg, err := parseConfig(args)
	if err != nil {
		return err
	}
	return execute(ctx, cfg)
}

func parseConfig(args []string) (*Config, error) {
	fs := flag.NewFlagSet("bump-flake-lock", flag.ContinueOnError)

	cfg := &Config{}
	fs.StringVar(&cfg.BranchName, "branch-name", "bot-update-flake-lock", "Branch name to push changes to")
	fs.StringVar(&cfg.PostCommand, "post-command", "", "Command to run after updating flake.lock")
	fs.StringVar(&cfg.PostCommitTitle, "post-commit-title", "", "Commit message for post-command changes")
	fs.StringVar(&cfg.PRTitle, "pr-title", "Bump flake.lock and related dependencies", "Title for the pull request")
	fs.StringVar(&cfg.PRBody, "pr-body", "", "Additional body for the pull request")
	fs.StringVar(&cfg.GitUserName, "git-user-name", "github-actions[bot]", "Git committer user name")
	fs.StringVar(&cfg.GitUserEmail, "git-user-email", "41898282+github-actions[bot]@users.noreply.github.com", "Git committer user email")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	// Environment variable fallbacks for GitHub Actions
	if env := os.Getenv("PR_BRANCH"); env != "" && cfg.BranchName == "bot-update-flake-lock" {
		cfg.BranchName = env
	}
	if env := os.Getenv("POST_COMMAND"); env != "" && cfg.PostCommand == "" {
		cfg.PostCommand = env
	}
	if env := os.Getenv("POST_COMMIT_TITLE"); env != "" && cfg.PostCommitTitle == "" {
		cfg.PostCommitTitle = env
	}
	if env := os.Getenv("PR_TITLE"); env != "" && cfg.PRTitle == "Bump flake.lock and related dependencies" {
		cfg.PRTitle = env
	}
	if env := os.Getenv("PR_BODY"); env != "" && cfg.PRBody == "" {
		cfg.PRBody = env
	}

	return cfg, nil
}

func gitEnv(cfg *Config) []string {
	return append(os.Environ(),
		"GIT_AUTHOR_NAME="+cfg.GitUserName,
		"GIT_AUTHOR_EMAIL="+cfg.GitUserEmail,
		"GIT_COMMITTER_NAME="+cfg.GitUserName,
		"GIT_COMMITTER_EMAIL="+cfg.GitUserEmail,
	)
}

func filterEnv(env []string, droppedPrefixes ...string) []string {
	var filtered []string
	for _, e := range env {
		dropped := false
		for _, prefix := range droppedPrefixes {
			if strings.HasPrefix(e, prefix) {
				dropped = true
				break
			}
		}
		if !dropped {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

func runCmd(ctx context.Context, env []string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	if len(env) > 0 {
		cmd.Env = env
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func runCmdOutput(ctx context.Context, env []string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if len(env) > 0 {
		cmd.Env = env
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

func execute(ctx context.Context, cfg *Config) error {
	env := gitEnv(cfg)

	if err := runCmd(ctx, env, "git", "switch", "-C", cfg.BranchName); err != nil {
		return fmt.Errorf("failed to switch branch to %s: %w", cfg.BranchName, err)
	}

	if err := runCmd(ctx, env, "nix", "flake", "update", "--commit-lock-file"); err != nil {
		return fmt.Errorf("failed to update flake.lock: %w", err)
	}

	if cfg.PostCommand != "" {
		cmd := exec.CommandContext(ctx, "sh", "-c", cfg.PostCommand)
		// Avoid passing powerful GitHub tokens to external post-update commands.
		cmd.Env = filterEnv(env, "GITHUB_TOKEN=", "GH_TOKEN=")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to run post-command: %w", err)
		}

		status, err := runCmdOutput(ctx, env, "git", "status", "--porcelain")
		if err != nil {
			return fmt.Errorf("failed to check git status: %w", err)
		}
		if status != "" {
			title := cfg.PostCommitTitle
			if title == "" {
				title = cfg.PostCommand
			}
			checker := &leakcheck.BetterleaksChecker{}
			if err := checker.Check(ctx, title); err != nil {
				return fmt.Errorf("pre-flight leak check failed for post-command commit message: %w", err)
			}
			if err := runCmd(ctx, env, "git", "add", "-A"); err != nil {
				return fmt.Errorf("failed to stage post-command changes: %w", err)
			}
			stagedDiff, err := runCmdOutput(ctx, env, "git", "diff", "--cached")
			if err != nil {
				return fmt.Errorf("failed to get staged diff for post-command changes: %w", err)
			}
			if err := checker.Check(ctx, stagedDiff); err != nil {
				return fmt.Errorf("pre-flight leak check failed for post-command staged diff: %w", err)
			}
			if err := runCmd(ctx, env, "git", "commit", "--no-gpg-sign", "-m", title); err != nil {
				return fmt.Errorf("failed to commit post-command changes: %w", err)
			}
		}
	}

	defaultBranch, err := runCmdOutput(ctx, env, "git", "symbolic-ref", "--short", "refs/remotes/origin/HEAD")
	if err != nil || defaultBranch == "" {
		defaultBranch = "main"
	} else {
		defaultBranch = strings.TrimPrefix(defaultBranch, "origin/")
	}

	countStr, err := runCmdOutput(ctx, env, "git", "rev-list", "--count", fmt.Sprintf("origin/%s..", defaultBranch))
	if err != nil {
		return fmt.Errorf("failed to count commits ahead of origin/%s: %w", defaultBranch, err)
	}
	count, err := strconv.Atoi(countStr)
	if err != nil {
		return fmt.Errorf("invalid commit count %q: %w", countStr, err)
	}

	body, err := buildPRBody(cfg.PRBody, workflowmeta.Get(), os.Getenv("GITHUB_ACTIONS") == "true")
	if err != nil {
		return fmt.Errorf("failed to build PR body: %w", err)
	}

	if count <= 0 {
		fmt.Println("No commits added. Nothing to push or create.")
		return nil
	}

	outgoingDiff, err := runCmdOutput(ctx, env, "git", "diff", fmt.Sprintf("origin/%s..", defaultBranch))
	if err != nil {
		return fmt.Errorf("failed to get outgoing diff: %w", err)
	}

	checker := &leakcheck.BetterleaksChecker{}
	if err := leakcheck.CheckMultiple(ctx, checker, cfg.PRTitle, body, outgoingDiff); err != nil {
		return fmt.Errorf("pre-flight leak check failed for pull request payload: %w", err)
	}

	fmt.Printf("Found %d commit(s).\n", count)
	fmt.Printf("Pushing to %s...\n", cfg.BranchName)
	if err := runCmd(ctx, env, "git", "push", "origin", cfg.BranchName, "--force"); err != nil {
		return fmt.Errorf("failed to push to %s: %w", cfg.BranchName, err)
	}

	prState, _ := runCmdOutput(ctx, env, "gh", "pr", "view", cfg.BranchName, "--json", "state", "--jq", ".state")
	if strings.TrimSpace(prState) != "OPEN" {
		createArgs := []string{
			"pr", "create",
			"--base", defaultBranch,
			"--head", cfg.BranchName,
			"--title", cfg.PRTitle,
			"--body", body,
		}
		if err := runCmd(ctx, env, "gh", createArgs...); err != nil {
			return fmt.Errorf("failed to create PR: %w", err)
		}
	}

	return nil
}
