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

	"github.com/kachick/workflows/internal/leakcheck"
	"github.com/kachick/workflows/internal/workflowmeta"
	"github.com/kachick/workflows/pkgs/nix-diff-closures/internal/diff"
	"github.com/kachick/workflows/pkgs/nix-diff-closures/internal/gh"
	"github.com/kachick/workflows/pkgs/nix-diff-closures/internal/osv"
	"github.com/kachick/workflows/pkgs/nix-diff-closures/internal/report"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	if err := workflowmeta.VerifyEnv(); err != nil {
		return err
	}

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
		checkVulns     bool
		osvEndpoint    string
		failOnVuln     bool
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
	flag.BoolVar(&checkVulns, "check-vulnerabilities", true, "Check updated packages against OSV API")
	flag.StringVar(&osvEndpoint, "osv-endpoint", osv.DefaultEndpoint, "OSV API batch query endpoint")
	flag.BoolVar(&failOnVuln, "fail-on-vulnerability", false, "Exit with non-zero status if vulnerabilities are detected")
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
	if envVal := os.Getenv("CHECK_VULNERABILITIES"); envVal != "" {
		if b, err := strconv.ParseBool(envVal); err == nil {
			checkVulns = b
		}
	}
	if envVal := os.Getenv("OSV_ENDPOINT"); envVal != "" {
		osvEndpoint = envVal
	}
	if envVal := os.Getenv("FAIL_ON_VULNERABILITY"); envVal != "" {
		if b, err := strconv.ParseBool(envVal); err == nil {
			failOnVuln = b
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

	var vulns []osv.PackageVulnerabilities
	if checkVulns {
		var allPkgs []osv.Package
		seenPkgs := make(map[string]bool)
		for _, res := range results {
			if res.DiffText != "" {
				pkgs := diff.ExtractUpdatedPackages(res.DiffText)
				for _, p := range pkgs {
					key := p.Name + "@" + p.Version
					if !seenPkgs[key] {
						seenPkgs[key] = true
						allPkgs = append(allPkgs, osv.Package{
							Name:    p.Name,
							Version: p.Version,
						})
					}
				}
			}
		}

		if len(allPkgs) > 0 {
			osvClient := osv.NewClient(osvEndpoint)
			v, err := osvClient.QueryBatch(ctx, allPkgs)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to query OSV API: %v\n", err)
			} else {
				vulns = v
			}
		}
	}

	meta := workflowmeta.Get()
	isCI := os.Getenv("GITHUB_ACTIONS") == "true"

	rep, err := report.Build(results, vulns, maxSize, headSize, meta, isCI)
	if err != nil {
		return fmt.Errorf("failed to build report: %w", err)
	}

	if rep.HasDiff {
		checker := &leakcheck.BetterleaksChecker{}
		if err := leakcheck.CheckMultiple(ctx, checker, rep.CommentMarkdown, rep.FullDiffText); err != nil {
			return fmt.Errorf("pre-flight leak check failed for diff report: %w", err)
		}

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

	writeGHAOutput("has_diff", strconv.FormatBool(rep.HasDiff))
	writeGHAOutput("is_truncated", strconv.FormatBool(rep.IsTruncated))
	writeGHAOutput("has_vulnerabilities", strconv.FormatBool(len(vulns) > 0))
	writeGHAOutput("vulnerabilities_count", strconv.Itoa(len(vulns)))

	if stepSummaryOut && rep.HasDiff {
		writeStepSummary(rep.CommentMarkdown)
	}

	if !rep.HasDiff {
		fmt.Println("No closure differences found across all targets.")
		return nil
	}

	fmt.Printf("Closure differences generated (truncated: %v)\n", rep.IsTruncated)

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

		if err := client.PostComment(ctx, owner, repoName, prNumber, rep.CommentMarkdown); err != nil {
			if errors.Is(err, gh.ErrPermissionDenied) {
				fmt.Fprintln(os.Stderr, "::warning::GitHub token lacks permission to post PR comment. Diff was posted to Step Summary instead.")
				return nil
			}
			return fmt.Errorf("failed to post pull request comment: %w", err)
		}
		fmt.Printf("Successfully posted diff comment to PR #%d\n", prNumber)
	}

	if failOnVuln && len(vulns) > 0 {
		return fmt.Errorf("detected %d package(s) with known vulnerabilities in closure diff", len(vulns))
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
