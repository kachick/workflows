package main

import (
	"bytes"
	"context"
	"errors"
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
	FlakeInputs     []string
	PostCommand     string
	PostCommitTitle string
	PRTitle         string
	PRBody          string
	GitUserName     string
	GitUserEmail    string
}

type commandRunner interface {
	Run(ctx context.Context, env []string, name string, args ...string) error
	Output(ctx context.Context, env []string, name string, args ...string) (string, error)
}

type defaultCommandRunner struct{}

func (defaultCommandRunner) Run(ctx context.Context, env []string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	if len(env) > 0 {
		cmd.Env = env
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func (defaultCommandRunner) Output(ctx context.Context, env []string, name string, args ...string) (string, error) {
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

func parseFlakeInputs(raw string) ([]string, error) {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
	for _, field := range fields {
		if strings.HasPrefix(field, "-") {
			return nil, fmt.Errorf("invalid flake input name %q: must not start with '-'", field)
		}
	}
	return fields, nil
}

func validateBranchName(branch string) error {
	trimmed := strings.TrimSpace(branch)
	if trimmed == "" {
		return errors.New("branch-name must not be empty")
	}
	if strings.HasPrefix(trimmed, "-") {
		return fmt.Errorf("invalid branch-name %q: must not start with '-'", trimmed)
	}
	if strings.ContainsAny(trimmed, " \t\r\n") {
		return fmt.Errorf("invalid branch-name %q: must not contain whitespace", trimmed)
	}
	return nil
}

func buildUpdateArgs(inputs []string) []string {
	args := []string{"flake", "update", "--commit-lock-file"}
	return append(args, inputs...)
}

func buildPRBody(customBody string, updates []flakeInputUpdate, meta workflowmeta.Metadata, isCI bool) (string, error) {
	if !meta.IsComplete() && isCI {
		return "", fmt.Errorf("missing required workflow metadata in CI environment: %+v", meta)
	}
	parts := []string{meta.ProvenanceText("This PR was generated automatically")}

	cleanCustom := strings.TrimSpace(customBody)
	if cleanCustom != "" && cleanCustom != "This PR was generated automatically." {
		parts = append(parts, cleanCustom)
	}

	if diffSection := formatFlakeLockUpdates(updates); diffSection != "" {
		parts = append(parts, diffSection)
	}

	return strings.Join(parts, "\n\n"), nil
}

func collectFlakeLockUpdates(ctx context.Context, runner commandRunner, env []string, defaultBranch string) []flakeInputUpdate {
	oldLock, err := runner.Output(ctx, env, "git", "show", fmt.Sprintf("origin/%s:flake.lock", defaultBranch))
	if err != nil {
		return nil
	}
	newLock, err := runner.Output(ctx, env, "git", "show", "HEAD:flake.lock")
	if err != nil {
		return nil
	}
	updates, err := diffFlakeLocks([]byte(oldLock), []byte(newLock))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to diff flake.lock for PR description: %v\n", err)
		return nil
	}
	return updates
}

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
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
	return execute(ctx, cfg, defaultCommandRunner{}, &leakcheck.BetterleaksChecker{})
}

func parseConfig(args []string) (*Config, error) {
	fs := flag.NewFlagSet("bump-flake-lock", flag.ContinueOnError)

	cfg := &Config{}
	var flakeInputsRaw string
	fs.StringVar(&cfg.BranchName, "branch-name", "bot-update-flake-lock", "Branch name to push changes to")
	fs.StringVar(&flakeInputsRaw, "flake-inputs", "", "Space- or comma-separated flake inputs to update (empty updates all)")
	fs.StringVar(&cfg.PostCommand, "post-command", "", "Command to run after updating flake.lock")
	fs.StringVar(&cfg.PostCommitTitle, "post-commit-title", "", "Commit message for post-command changes")
	fs.StringVar(&cfg.PRTitle, "pr-title", "Bump flake.lock and related dependencies", "Title for the pull request")
	fs.StringVar(&cfg.PRBody, "pr-body", "", "Additional body for the pull request")
	fs.StringVar(&cfg.GitUserName, "git-user-name", "github-actions[bot]", "Git committer user name")
	fs.StringVar(&cfg.GitUserEmail, "git-user-email", "41898282+github-actions[bot]@users.noreply.github.com", "Git committer user email")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	explicit := make(map[string]bool)
	fs.Visit(func(f *flag.Flag) {
		explicit[f.Name] = true
	})

	// Environment variable fallbacks for GitHub Actions (only when not explicitly set via CLI flags).
	if !explicit["branch-name"] {
		if env := os.Getenv("PR_BRANCH"); env != "" {
			cfg.BranchName = env
		}
	}
	if !explicit["flake-inputs"] {
		if env := os.Getenv("FLAKE_INPUTS"); env != "" {
			flakeInputsRaw = env
		}
	}
	if !explicit["post-command"] {
		if env := os.Getenv("POST_COMMAND"); env != "" {
			cfg.PostCommand = env
		}
	}
	if !explicit["post-commit-title"] {
		if env := os.Getenv("POST_COMMIT_TITLE"); env != "" {
			cfg.PostCommitTitle = env
		}
	}
	if !explicit["pr-title"] {
		if env := os.Getenv("PR_TITLE"); env != "" {
			cfg.PRTitle = env
		}
	}
	if !explicit["pr-body"] {
		if env := os.Getenv("PR_BODY"); env != "" {
			cfg.PRBody = env
		}
	}

	cfg.BranchName = strings.TrimSpace(cfg.BranchName)
	if err := validateBranchName(cfg.BranchName); err != nil {
		return nil, err
	}

	flakeInputs, err := parseFlakeInputs(flakeInputsRaw)
	if err != nil {
		return nil, err
	}
	cfg.FlakeInputs = flakeInputs

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

func resolveDefaultBranch(ctx context.Context, runner commandRunner, env []string, initialBranch string) string {
	if sym, err := runner.Output(ctx, env, "git", "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil && sym != "" {
		return strings.TrimPrefix(sym, "origin/")
	}
	if initialBranch != "" && initialBranch != "HEAD" {
		return initialBranch
	}
	if refName := strings.TrimSpace(os.Getenv("GITHUB_REF_NAME")); refName != "" {
		return refName
	}
	return "main"
}

func execute(ctx context.Context, cfg *Config, runner commandRunner, checker leakcheck.Checker) error {
	env := gitEnv(cfg)

	// Capture checked-out branch before switching so repositories with non-"main" default branches
	// still resolve the base branch when actions/checkout leaves refs/remotes/origin/HEAD unset.
	initialBranch, _ := runner.Output(ctx, env, "git", "rev-parse", "--abbrev-ref", "HEAD")

	if err := runner.Run(ctx, env, "git", "switch", "-C", cfg.BranchName); err != nil {
		return fmt.Errorf("failed to switch branch to %s: %w", cfg.BranchName, err)
	}

	if err := runner.Run(ctx, env, "nix", buildUpdateArgs(cfg.FlakeInputs)...); err != nil {
		return fmt.Errorf("failed to update flake.lock: %w", err)
	}

	if cfg.PostCommand != "" {
		// Avoid passing powerful GitHub tokens to external post-update commands.
		postEnv := filterEnv(env, "GITHUB_TOKEN=", "GH_TOKEN=")
		if err := runner.Run(ctx, postEnv, "sh", "-c", cfg.PostCommand); err != nil {
			return fmt.Errorf("failed to run post-command: %w", err)
		}

		status, err := runner.Output(ctx, env, "git", "status", "--porcelain")
		if err != nil {
			return fmt.Errorf("failed to check git status: %w", err)
		}
		if status != "" {
			title := cfg.PostCommitTitle
			if title == "" {
				title = cfg.PostCommand
			}
			if err := checker.Check(ctx, title); err != nil {
				return fmt.Errorf("pre-flight leak check failed for post-command commit message: %w", err)
			}
			if err := runner.Run(ctx, env, "git", "add", "-A"); err != nil {
				return fmt.Errorf("failed to stage post-command changes: %w", err)
			}
			stagedDiff, err := runner.Output(ctx, env, "git", "diff", "--cached")
			if err != nil {
				return fmt.Errorf("failed to get staged diff for post-command changes: %w", err)
			}
			if err := checker.Check(ctx, stagedDiff); err != nil {
				return fmt.Errorf("pre-flight leak check failed for post-command staged diff: %w", err)
			}
			if err := runner.Run(ctx, env, "git", "commit", "--no-gpg-sign", "-m", title); err != nil {
				return fmt.Errorf("failed to commit post-command changes: %w", err)
			}
		}
	}

	defaultBranch := resolveDefaultBranch(ctx, runner, env, initialBranch)

	countStr, err := runner.Output(ctx, env, "git", "rev-list", "--count", fmt.Sprintf("origin/%s..", defaultBranch))
	if err != nil {
		return fmt.Errorf("failed to count commits ahead of origin/%s: %w", defaultBranch, err)
	}
	count, err := strconv.Atoi(countStr)
	if err != nil {
		return fmt.Errorf("invalid commit count %q: %w", countStr, err)
	}

	var updates []flakeInputUpdate
	if count > 0 {
		updates = collectFlakeLockUpdates(ctx, runner, env, defaultBranch)
	}

	body, err := buildPRBody(cfg.PRBody, updates, workflowmeta.Get(), os.Getenv("GITHUB_ACTIONS") == "true")
	if err != nil {
		return fmt.Errorf("failed to build PR body: %w", err)
	}

	if count <= 0 {
		fmt.Println("No commits added. Nothing to push or create.")
		return nil
	}

	outgoingDiff, err := runner.Output(ctx, env, "git", "diff", fmt.Sprintf("origin/%s..", defaultBranch))
	if err != nil {
		return fmt.Errorf("failed to get outgoing diff: %w", err)
	}
	outgoingLog, err := runner.Output(ctx, env, "git", "log", "--format=%B", fmt.Sprintf("origin/%s..", defaultBranch))
	if err != nil {
		return fmt.Errorf("failed to get outgoing commit messages: %w", err)
	}

	if err := leakcheck.CheckMultiple(ctx, checker, cfg.PRTitle, body, outgoingLog, outgoingDiff); err != nil {
		return fmt.Errorf("pre-flight leak check failed for pull request payload: %w", err)
	}

	fmt.Printf("Found %d commit(s).\n", count)
	// Configure git credentials only after post-command and leak checks finish,
	// so untrusted code in post-command cannot read tokens from .git/config.
	if err := runner.Run(ctx, env, "gh", "auth", "setup-git"); err != nil {
		return fmt.Errorf("failed to configure git authentication: %w", err)
	}
	fmt.Printf("Pushing to %s...\n", cfg.BranchName)
	if err := runner.Run(ctx, env, "git", "push", "origin", cfg.BranchName, "--force"); err != nil {
		return fmt.Errorf("failed to push to %s: %w", cfg.BranchName, err)
	}

	prState, _ := runner.Output(ctx, env, "gh", "pr", "view", cfg.BranchName, "--json", "state", "--jq", ".state")
	if strings.TrimSpace(prState) != "OPEN" {
		createArgs := []string{
			"pr", "create",
			"--base", defaultBranch,
			"--head", cfg.BranchName,
			"--title", cfg.PRTitle,
			"--body", body,
		}
		if err := runner.Run(ctx, env, "gh", createArgs...); err != nil {
			return fmt.Errorf("failed to create PR: %w", err)
		}
	} else {
		editArgs := []string{
			"pr", "edit", cfg.BranchName,
			"--body", body,
		}
		if err := runner.Run(ctx, env, "gh", editArgs...); err != nil {
			return fmt.Errorf("failed to update PR description: %w", err)
		}
	}

	return nil
}
