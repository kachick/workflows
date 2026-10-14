package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kachick/nix-diff-closures-action/internal/diff"
	"github.com/kachick/nix-diff-closures-action/internal/gh"
	"github.com/kachick/nix-diff-closures-action/internal/report"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		baseRev        string
		targets        string
		outComment     string
		outFullDiff    string
		maxSize        int
		headSize       int
		prNumber       int
		repo           string
		token          string
		postComment    bool
		repoDir        string
		stepSummaryOut bool
	)

	flag.StringVar(&baseRev, "base-rev", "", "Base git revision (e.g. PR base SHA)")
	flag.StringVar(&targets, "targets", "", "Targets to diff (comma or newline separated, e.g. devShells.x86_64-linux.default)")
	flag.StringVar(&outComment, "output-comment", "comment.md", "Path to save comment markdown")
	flag.StringVar(&outFullDiff, "output-full-diff", "diff.txt", "Path to save full diff text")
	flag.IntVar(&maxSize, "max-size", report.DefaultMaxSize, "Maximum bytes before truncating comment")
	flag.IntVar(&headSize, "head-size", report.DefaultHeadSize, "Target bytes for truncated preview")
	flag.IntVar(&prNumber, "pr", 0, "Pull request number")
	flag.StringVar(&repo, "repo", "", "Repository in owner/repo format")
	flag.StringVar(&token, "github-token", "", "GitHub token for commenting")
	flag.BoolVar(&postComment, "post-comment", true, "Whether to post comment on GitHub PR")
	flag.StringVar(&repoDir, "repo-dir", ".", "Repository root directory")
	flag.BoolVar(&stepSummaryOut, "write-step-summary", true, "Whether to append to $GITHUB_STEP_SUMMARY")
	flag.Parse()

	// Environment variable fallbacks for GitHub Actions
	if baseRev == "" {
		baseRev = os.Getenv("BASE_SHA")
	}
	if targets == "" {
		targets = os.Getenv("TARGETS")
	}
	if prNumber == 0 {
		if prEnv := os.Getenv("PR_NUMBER"); prEnv != "" {
			if n, err := strconv.Atoi(prEnv); err == nil {
				prNumber = n
			}
		}
	}
	if repo == "" {
		repo = os.Getenv("GITHUB_REPOSITORY")
	}
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
		if token == "" {
			token = os.Getenv("GH_TOKEN")
		}
	}

	if baseRev == "" {
		return errors.New("base-rev is required (or set BASE_SHA)")
	}
	if strings.TrimSpace(targets) == "" {
		return errors.New("targets is required (or set TARGETS)")
	}

	absRepoDir, err := filepath.Abs(repoDir)
	if err != nil {
		return fmt.Errorf("failed to get absolute path for repo-dir: %w", err)
	}

	ctx := context.Background()
	engine := &diff.Engine{
		Exec:    &diff.DefaultCommandExecutor{Dir: absRepoDir},
		RepoDir: absRepoDir,
	}

	results, err := engine.Run(ctx, baseRev, targets)
	if err != nil {
		return fmt.Errorf("failed to compute closure diffs: %w", err)
	}

	serverURL := os.Getenv("GITHUB_SERVER_URL")
	runID := os.Getenv("GITHUB_RUN_ID")

	rep := report.Build(results, maxSize, headSize, serverURL, repo, runID)

	if rep.HasDiff {
		if outComment != "" {
			if err := os.WriteFile(outComment, []byte(rep.CommentMarkdown), 0o644); err != nil {
				return fmt.Errorf("failed to write comment file: %w", err)
			}
		}
		if outFullDiff != "" {
			if err := os.WriteFile(outFullDiff, []byte(rep.FullDiffText), 0o644); err != nil {
				return fmt.Errorf("failed to write full diff file: %w", err)
			}
		}
	}

	// Write GitHub Actions outputs
	writeGHAOutput("has_diff", strconv.FormatBool(rep.HasDiff))
	writeGHAOutput("is_truncated", strconv.FormatBool(rep.IsTruncated))

	// Write Step Summary if enabled
	if stepSummaryOut && rep.HasDiff {
		writeStepSummary(rep.CommentMarkdown)
	}

	if !rep.HasDiff {
		fmt.Println("No closure differences found across all targets.")
		return nil
	}

	fmt.Printf("Closure differences generated (truncated: %v)\n", rep.IsTruncated)

	// Post comment if requested and credentials available
	if postComment {
		if token == "" || repo == "" || prNumber == 0 {
			fmt.Println("Skipping GitHub comment: missing token, repo, or PR number.")
			return nil
		}

		parts := strings.Split(repo, "/")
		if len(parts) != 2 {
			return fmt.Errorf("invalid repository format: %q (expected owner/repo)", repo)
		}
		owner, repoName := parts[0], parts[1]

		client := gh.NewClient(token)

		// Minimize older comments
		oldIDs, err := client.FindOutdatedComments(ctx, owner, repoName, prNumber, report.Marker)
		if err != nil {
			if errors.Is(err, gh.ErrPermissionDenied) {
				fmt.Fprintln(os.Stderr, "::warning::GitHub token lacks permission to list comments. Continuing without comment minimize.")
			} else {
				fmt.Fprintf(os.Stderr, "Warning: failed to search old comments: %v\n", err)
			}
		} else {
			for _, nodeID := range oldIDs {
				if err := client.MinimizeComment(ctx, nodeID); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to minimize comment %s: %v\n", nodeID, err)
				}
			}
		}

		// Post new comment
		if err := client.PostComment(ctx, owner, repoName, prNumber, rep.CommentMarkdown); err != nil {
			if errors.Is(err, gh.ErrPermissionDenied) {
				fmt.Fprintln(os.Stderr, "::warning::GitHub token lacks permission to post PR comment. Diff was posted to Step Summary instead.")
				return nil
			}
			return fmt.Errorf("failed to post pull request comment: %w", err)
		}
		fmt.Printf("Successfully posted diff comment to PR #%d\n", prNumber)
	}

	return nil
}

func writeGHAOutput(key, value string) {
	outputFile := os.Getenv("GITHUB_OUTPUT")
	if outputFile == "" {
		return
	}
	f, err := os.OpenFile(outputFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = fmt.Fprintf(f, "%s=%s\n", key, value)
}

func writeStepSummary(markdown string) {
	summaryFile := os.Getenv("GITHUB_STEP_SUMMARY")
	if summaryFile == "" {
		return
	}
	f, err := os.OpenFile(summaryFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = fmt.Fprintln(f, markdown)
}
