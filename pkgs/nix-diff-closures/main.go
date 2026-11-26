package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kachick/workflows/internal/gh"
	"github.com/kachick/workflows/internal/leakcheck"
	"github.com/kachick/workflows/internal/workflowmeta"
	"github.com/kachick/workflows/pkgs/nix-diff-closures/internal/diff"
	"github.com/kachick/workflows/pkgs/nix-diff-closures/internal/osv"
	"github.com/kachick/workflows/pkgs/nix-diff-closures/internal/report"
)

type cliConfig struct {
	baseRev            string
	targets            string
	outComment         string
	outFullDiff        string
	outTree            string
	outWhyDepends      string
	outVulnerabilities string
	maxSize            int
	headSize           int
	prNumber           int
	repo               string
	token              string
	postComment        bool
	repoDir            string
	stepSummaryOut     bool
	checkVulns         bool
	osvEndpoint        string
	failOnVuln         bool
}

func parseConfig(args []string) (cliConfig, error) {
	var cfg cliConfig
	fs := flag.NewFlagSet("nix-diff-closures", flag.ContinueOnError)
	fs.StringVar(&cfg.baseRev, "base-rev", "", "Base git revision (e.g. PR base SHA)")
	fs.StringVar(&cfg.targets, "targets", "", "Targets to diff (comma or newline separated, e.g. devShells.x86_64-linux.default)")
	fs.StringVar(&cfg.outComment, "output-comment", "comment.md", "Path to save comment markdown")
	fs.StringVar(&cfg.outFullDiff, "output-full-diff", "diff.txt", "Path to save full diff text")
	fs.StringVar(&cfg.outTree, "output-tree", "", "Path to save target dependency tree text")
	fs.StringVar(&cfg.outWhyDepends, "output-why-depends", "", "Path to save why-depends text")
	fs.StringVar(&cfg.outVulnerabilities, "output-vulnerabilities", "", "Path to save vulnerability report text")
	fs.IntVar(&cfg.maxSize, "max-size", report.DefaultMaxSize, "Maximum bytes before truncating comment")
	fs.IntVar(&cfg.headSize, "head-size", report.DefaultHeadSize, "Target bytes for truncated preview")
	fs.IntVar(&cfg.prNumber, "pr", 0, "Pull request number")
	fs.StringVar(&cfg.repo, "repo", "", "Repository in owner/repo format")
	fs.StringVar(&cfg.token, "github-token", "", "GitHub token for commenting")
	fs.BoolVar(&cfg.postComment, "post-comment", true, "Whether to post comment on GitHub PR")
	fs.StringVar(&cfg.repoDir, "repo-dir", ".", "Repository root directory")
	fs.BoolVar(&cfg.stepSummaryOut, "write-step-summary", true, "Whether to append to $GITHUB_STEP_SUMMARY")
	fs.BoolVar(&cfg.checkVulns, "check-vulnerabilities", true, "Check updated packages against OSV API")
	fs.StringVar(&cfg.osvEndpoint, "osv-endpoint", osv.DefaultEndpoint, "OSV API batch query endpoint")
	fs.BoolVar(&cfg.failOnVuln, "fail-on-vulnerability", false, "Exit with non-zero status if vulnerabilities are detected")

	if err := fs.Parse(args); err != nil {
		return cfg, err
	}

	explicit := make(map[string]bool)
	fs.Visit(func(f *flag.Flag) {
		explicit[f.Name] = true
	})

	// Environment variable fallbacks for GitHub Actions (only when not explicitly set via CLI flags).
	if !explicit["base-rev"] && cfg.baseRev == "" {
		cfg.baseRev = os.Getenv("BASE_SHA")
	}
	if !explicit["targets"] && cfg.targets == "" {
		cfg.targets = os.Getenv("TARGETS")
	}
	if !explicit["pr"] && cfg.prNumber == 0 {
		if prEnv := strings.TrimSpace(os.Getenv("PR_NUMBER")); prEnv != "" {
			n, err := strconv.Atoi(prEnv)
			if err != nil {
				return cfg, fmt.Errorf("invalid PR_NUMBER %q: %w", prEnv, err)
			}
			cfg.prNumber = n
		}
	}
	if !explicit["repo"] && cfg.repo == "" {
		cfg.repo = os.Getenv("GITHUB_REPOSITORY")
	}
	if !explicit["github-token"] && cfg.token == "" {
		cfg.token = os.Getenv("GITHUB_TOKEN")
		if cfg.token == "" {
			cfg.token = os.Getenv("GH_TOKEN")
		}
	}
	if !explicit["check-vulnerabilities"] {
		if envVal := strings.TrimSpace(os.Getenv("CHECK_VULNERABILITIES")); envVal != "" {
			b, err := strconv.ParseBool(envVal)
			if err != nil {
				return cfg, fmt.Errorf("invalid CHECK_VULNERABILITIES %q: %w", envVal, err)
			}
			cfg.checkVulns = b
		}
	}
	if !explicit["osv-endpoint"] {
		if envVal := strings.TrimSpace(os.Getenv("OSV_ENDPOINT")); envVal != "" {
			cfg.osvEndpoint = envVal
		}
	}
	if !explicit["fail-on-vulnerability"] {
		if envVal := strings.TrimSpace(os.Getenv("FAIL_ON_VULNERABILITY")); envVal != "" {
			b, err := strconv.ParseBool(envVal)
			if err != nil {
				return cfg, fmt.Errorf("invalid FAIL_ON_VULNERABILITY %q: %w", envVal, err)
			}
			cfg.failOnVuln = b
		}
	}

	if cfg.baseRev == "" {
		return cfg, errors.New("base-rev is required (or set BASE_SHA)")
	}
	if strings.TrimSpace(cfg.targets) == "" {
		return cfg, errors.New("targets is required (or set TARGETS)")
	}

	return cfg, nil
}

func classifyVulnerabilities(beforePkgs, afterPkgs []osv.Package, batchResults []osv.PackageVulnerabilities) report.VulnerabilityResult {
	vulnRes := report.VulnerabilityResult{Checked: true}

	pkgVulns := make(map[string][]osv.Vulnerability, len(batchResults))
	for _, pv := range batchResults {
		key := pv.Package.Name + "@" + pv.Package.Version
		pkgVulns[key] = pv.Vulnerabilities
	}

	beforeVulnsByPkg := make(map[string]map[string]osv.Vulnerability)
	for _, bp := range beforePkgs {
		key := bp.Name + "@" + bp.Version
		if vList, ok := pkgVulns[key]; ok {
			if beforeVulnsByPkg[bp.Name] == nil {
				beforeVulnsByPkg[bp.Name] = make(map[string]osv.Vulnerability)
			}
			for _, v := range vList {
				beforeVulnsByPkg[bp.Name][v.ID] = v
			}
		}
	}

	afterVulnsByPkg := make(map[string]map[string]osv.Vulnerability)
	for _, ap := range afterPkgs {
		key := ap.Name + "@" + ap.Version
		if vList, ok := pkgVulns[key]; ok {
			if afterVulnsByPkg[ap.Name] == nil {
				afterVulnsByPkg[ap.Name] = make(map[string]osv.Vulnerability)
			}
			for _, v := range vList {
				afterVulnsByPkg[ap.Name][v.ID] = v
			}
		}
	}

	for _, ap := range afterPkgs {
		key := ap.Name + "@" + ap.Version
		vList, ok := pkgVulns[key]
		if !ok || len(vList) == 0 {
			continue
		}

		var newlyIntroduced []osv.Vulnerability
		var alreadyPresent []osv.Vulnerability
		beforeSet := beforeVulnsByPkg[ap.Name]

		for _, v := range vList {
			if beforeSet != nil {
				if _, exists := beforeSet[v.ID]; exists {
					alreadyPresent = append(alreadyPresent, v)
					continue
				}
			}
			newlyIntroduced = append(newlyIntroduced, v)
		}

		if len(newlyIntroduced) > 0 {
			vulnRes.NewVulns = append(vulnRes.NewVulns, osv.PackageVulnerabilities{
				Package:         ap,
				Vulnerabilities: newlyIntroduced,
			})
		}
		if len(alreadyPresent) > 0 {
			vulnRes.ExistingVulns = append(vulnRes.ExistingVulns, osv.PackageVulnerabilities{
				Package:         ap,
				Vulnerabilities: alreadyPresent,
			})
		}
	}

	for _, bp := range beforePkgs {
		key := bp.Name + "@" + bp.Version
		vList, ok := pkgVulns[key]
		if !ok || len(vList) == 0 {
			continue
		}

		var resolved []osv.Vulnerability
		afterSet := afterVulnsByPkg[bp.Name]

		for _, v := range vList {
			if afterSet == nil {
				resolved = append(resolved, v)
			} else if _, exists := afterSet[v.ID]; !exists {
				resolved = append(resolved, v)
			}
		}

		if len(resolved) > 0 {
			vulnRes.ResolvedVulns = append(vulnRes.ResolvedVulns, osv.PackageVulnerabilities{
				Package:         bp,
				Vulnerabilities: resolved,
			})
		}
	}

	return vulnRes
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if err := workflowmeta.VerifyEnv(); err != nil {
		return err
	}

	cfg, err := parseConfig(args)
	if err != nil {
		return err
	}

	absRepoDir, err := filepath.Abs(cfg.repoDir)
	if err != nil {
		return fmt.Errorf("failed to get absolute path for repo-dir: %w", err)
	}

	ctx := context.Background()
	engine := &diff.Engine{
		Exec:    &diff.DefaultCommandExecutor{Dir: absRepoDir},
		RepoDir: absRepoDir,
	}

	results, err := engine.Run(ctx, cfg.baseRev, cfg.targets)
	if err != nil {
		return fmt.Errorf("failed to compute closure diffs: %w", err)
	}

	var vulnRes report.VulnerabilityResult
	var osvScanErr error
	if cfg.checkVulns {
		vulnRes.Checked = true
		var beforePkgs []osv.Package
		var afterPkgs []osv.Package
		seenBefore := make(map[string]bool)
		seenAfter := make(map[string]bool)

		for _, res := range results {
			if res.DiffText != "" {
				bPkgs, aPkgs := diff.ExtractPackageChanges(res.DiffText)
				for _, p := range bPkgs {
					key := p.Name + "@" + p.Version
					if !seenBefore[key] {
						seenBefore[key] = true
						beforePkgs = append(beforePkgs, osv.Package{
							Name:    p.Name,
							Version: p.Version,
						})
					}
				}
				for _, p := range aPkgs {
					key := p.Name + "@" + p.Version
					if !seenAfter[key] {
						seenAfter[key] = true
						afterPkgs = append(afterPkgs, osv.Package{
							Name:    p.Name,
							Version: p.Version,
						})
					}
				}
			}
		}

		allQueryPkgs := make([]osv.Package, 0, len(beforePkgs)+len(afterPkgs))
		seenQuery := make(map[string]bool)
		for _, p := range append(beforePkgs, afterPkgs...) {
			key := p.Name + "@" + p.Version
			if !seenQuery[key] {
				seenQuery[key] = true
				allQueryPkgs = append(allQueryPkgs, p)
			}
		}

		if len(allQueryPkgs) > 0 {
			osvClient := osv.NewClient(cfg.osvEndpoint)
			batchResults, err := osvClient.QueryBatch(ctx, allQueryPkgs)
			if err != nil {
				osvScanErr = err
				vulnRes.ScanError = err.Error()
				fmt.Fprintf(os.Stderr, "Error: failed to query OSV API: %v\n", err)
			} else {
				vulnRes = classifyVulnerabilities(beforePkgs, afterPkgs, batchResults)
			}
		}
	}

	meta := workflowmeta.Get()
	isCI := os.Getenv("GITHUB_ACTIONS") == "true"

	rep, err := report.Build(results, vulnRes, cfg.maxSize, cfg.headSize, meta, isCI)
	if err != nil {
		return fmt.Errorf("failed to build report: %w", err)
	}

	hasReport := rep.HasDiff || rep.HasErrors
	if hasReport {
		checker := &leakcheck.BetterleaksChecker{}
		if err := leakcheck.CheckMultiple(ctx, checker, rep.CommentMarkdown, rep.FullDiffText, rep.FullTreeText, rep.FullWhyDependsText, rep.FullVulnerabilitiesText); err != nil {
			return fmt.Errorf("pre-flight leak check failed for diff report: %w", err)
		}

		if cfg.outComment != "" {
			if err := os.WriteFile(cfg.outComment, []byte(rep.CommentMarkdown), 0o644); err != nil {
				return fmt.Errorf("failed to write comment file: %w", err)
			}
		}
		if cfg.outFullDiff != "" && rep.FullDiffText != "" {
			if err := os.WriteFile(cfg.outFullDiff, []byte(rep.FullDiffText), 0o644); err != nil {
				return fmt.Errorf("failed to write full diff file: %w", err)
			}
		}
		if cfg.outTree != "" && rep.FullTreeText != "" {
			if err := os.WriteFile(cfg.outTree, []byte(rep.FullTreeText), 0o644); err != nil {
				return fmt.Errorf("failed to write tree file: %w", err)
			}
		}
		if cfg.outWhyDepends != "" && rep.FullWhyDependsText != "" {
			if err := os.WriteFile(cfg.outWhyDepends, []byte(rep.FullWhyDependsText), 0o644); err != nil {
				return fmt.Errorf("failed to write why-depends file: %w", err)
			}
		}
		if cfg.outVulnerabilities != "" && rep.FullVulnerabilitiesText != "" {
			if err := os.WriteFile(cfg.outVulnerabilities, []byte(rep.FullVulnerabilitiesText), 0o644); err != nil {
				return fmt.Errorf("failed to write vulnerabilities file: %w", err)
			}
		}
	}

	for _, kv := range [][2]string{
		{"has_diff", strconv.FormatBool(rep.HasDiff)},
		{"is_truncated", strconv.FormatBool(rep.IsTruncated)},
		{"has_vulnerabilities", strconv.FormatBool(len(rep.Vulnerabilities) > 0)},
		{"vulnerabilities_count", strconv.Itoa(len(rep.Vulnerabilities))},
	} {
		if err := writeGHAOutput(kv[0], kv[1]); err != nil {
			return fmt.Errorf("failed to write GITHUB_OUTPUT (%s): %w", kv[0], err)
		}
	}

	if cfg.stepSummaryOut && hasReport {
		if err := writeStepSummary(rep.StepSummaryMarkdown); err != nil {
			return fmt.Errorf("failed to write GITHUB_STEP_SUMMARY: %w", err)
		}
	}

	if cfg.checkVulns && osvScanErr == nil {
		fmt.Printf("Vulnerability scan: %d newly introduced, %d resolved, %d pre-existing package(s)\n", len(rep.Vulnerabilities), len(rep.ResolvedVulnerabilities), len(rep.ExistingVulnerabilities))
	}

	if !hasReport {
		fmt.Println("No closure differences found across all targets.")
		return nil
	}

	if rep.HasDiff {
		fmt.Printf("Closure differences generated (truncated: %v)\n", rep.IsTruncated)
	}

	if cfg.postComment {
		if cfg.token == "" || cfg.repo == "" || cfg.prNumber == 0 {
			fmt.Println("Skipping GitHub comment: missing token, repo, or PR number.")
		} else {
			parts := strings.Split(cfg.repo, "/")
			if len(parts) != 2 {
				return fmt.Errorf("invalid repository format: %q (expected owner/repo)", cfg.repo)
			}
			owner, repoName := parts[0], parts[1]

			// Minimize older diff comments using minimize-pr-comments CLI
			minimizeCmd := exec.CommandContext(ctx, "minimize-pr-comments",
				"--owner="+owner,
				"--repo="+repoName,
				fmt.Sprintf("--pr=%d", cfg.prNumber),
				"--marker="+report.Marker,
			)
			minimizeCmd.Env = os.Environ()
			minimizeCmd.Stdout = os.Stdout
			minimizeCmd.Stderr = os.Stderr
			if err := minimizeCmd.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to minimize old comments: %v\n", err)
			}

			client := gh.NewClient(cfg.token)

			// Second critical guardrail after leak checking: enforce safe length before API call to prevent 422 errors
			commentToPost := report.EnsureSafeCommentLength(rep.CommentMarkdown, meta)

			if err := client.PostComment(ctx, owner, repoName, cfg.prNumber, commentToPost); err != nil {
				if errors.Is(err, gh.ErrPermissionDenied) {
					fmt.Fprintln(os.Stderr, "::warning::GitHub token lacks permission to post PR comment. Diff was posted to Step Summary instead.")
				} else {
					return fmt.Errorf("failed to post pull request comment: %w", err)
				}
			} else {
				fmt.Printf("Successfully posted diff comment to PR #%d\n", cfg.prNumber)
			}
		}
	}

	// Fail closed after posting the report so PR comments explain the failure while CI stops automerge.
	var targetErrs []string
	for _, res := range results {
		if res.Error != "" {
			targetErrs = append(targetErrs, fmt.Sprintf("%s: %s", res.Target, res.Error))
		}
	}
	if len(targetErrs) > 0 {
		return fmt.Errorf("failed to evaluate %d target(s): %s", len(targetErrs), strings.Join(targetErrs, "; "))
	}
	if osvScanErr != nil {
		return fmt.Errorf("OSV vulnerability scan failed: %w", osvScanErr)
	}
	if cfg.failOnVuln && len(rep.Vulnerabilities) > 0 {
		return fmt.Errorf("detected %d package(s) with newly introduced vulnerabilities in closure diff", len(rep.Vulnerabilities))
	}

	return nil
}

func writeGHAOutput(key, value string) error {
	outputFile := os.Getenv("GITHUB_OUTPUT")
	if outputFile == "" {
		return nil
	}
	f, err := os.OpenFile(outputFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, writeErr := fmt.Fprintf(f, "%s=%s\n", key, value)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func writeStepSummary(markdown string) error {
	summaryFile := os.Getenv("GITHUB_STEP_SUMMARY")
	if summaryFile == "" {
		return nil
	}
	f, err := os.OpenFile(summaryFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, writeErr := fmt.Fprintln(f, markdown)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}
