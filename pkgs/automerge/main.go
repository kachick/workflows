package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

var defaultAllowedBots = []string{
	"app/dependabot",
	"dependabot[bot]",
	"app/renovate",
	"renovate[bot]",
	"app/samuebot",
	"samuebot[bot]",
}

var dependabotBumpRegex = regexp.MustCompile(`^Bump (?P<pkg>.+) from (?P<old>[^ ]+) to (?P<new>[^ ]+)(?: in .+)?$`)

type PullRequest struct {
	Number            int               `json:"number"`
	Title             string            `json:"title"`
	Author            Author            `json:"author"`
	IsDraft           bool              `json:"isDraft"`
	Mergeable         string            `json:"mergeable"`
	StatusCheckRollup []StatusCheckItem `json:"statusCheckRollup"`
}

type Author struct {
	Login string `json:"login"`
}

type StatusCheckItem struct {
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

type Config struct {
	AllowedBots []string
	MergeMethod string
}

type CommandRunner interface {
	FetchOpenPRs(ctx context.Context) ([]PullRequest, error)
	MergePR(ctx context.Context, number int, method string) error
}

type DefaultCommandRunner struct{}

func (r *DefaultCommandRunner) FetchOpenPRs(ctx context.Context) ([]PullRequest, error) {
	cmd := exec.CommandContext(ctx, "gh", "pr", "list",
		"--state", "open",
		"--json", "number,title,author,isDraft,mergeable,statusCheckRollup",
	)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to fetch open prs: %w", err)
	}

	var prs []PullRequest
	if err := json.Unmarshal(stdout.Bytes(), &prs); err != nil {
		return nil, fmt.Errorf("failed to parse pr json: %w", err)
	}

	return prs, nil
}

func (r *DefaultCommandRunner) MergePR(ctx context.Context, number int, method string) error {
	cmd := exec.CommandContext(ctx, "gh", "pr", "merge",
		"--"+method,
		"--delete-branch",
		strconv.Itoa(number),
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func parseAllowedBots(input string) []string {
	if strings.TrimSpace(input) == "" {
		return defaultAllowedBots
	}

	var bots []string
	for _, b := range strings.Split(input, ",") {
		trimmed := strings.TrimSpace(b)
		if trimmed != "" {
			bots = append(bots, trimmed)
		}
	}

	if len(bots) == 0 {
		return defaultAllowedBots
	}
	return bots
}

func parseConfig(args []string) (*Config, error) {
	fs := flag.NewFlagSet("automerge", flag.ContinueOnError)

	var allowedBotsRaw string
	cfg := &Config{}

	fs.StringVar(&allowedBotsRaw, "allowed-bots", "", "Comma-separated list of allowed bot logins")
	fs.StringVar(&cfg.MergeMethod, "merge-method", "squash", "Merge method (squash, merge, or rebase)")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	if env := os.Getenv("ALLOWED_BOTS"); env != "" && allowedBotsRaw == "" {
		allowedBotsRaw = env
	}
	if env := os.Getenv("MERGE_METHOD"); env != "" && cfg.MergeMethod == "squash" {
		cfg.MergeMethod = env
	}

	validMethods := map[string]bool{"squash": true, "merge": true, "rebase": true}
	if !validMethods[cfg.MergeMethod] {
		return nil, fmt.Errorf("invalid merge-method: %q (must be squash, merge, or rebase)", cfg.MergeMethod)
	}

	cfg.AllowedBots = parseAllowedBots(allowedBotsRaw)
	return cfg, nil
}

func extractVersionParts(v string) (major string, minor string) {
	cleaned := strings.TrimPrefix(v, "v")
	parts := strings.Split(cleaned, ".")
	major = parts[0]
	if len(parts) > 1 {
		minor = parts[1]
	}
	return major, minor
}

func checkDependabotBump(title string) (disallowed bool, reason string) {
	matches := dependabotBumpRegex.FindStringSubmatch(title)
	if matches == nil {
		// Disallow group updates and unrecognized titles because their versions cannot be checked from the title.
		return true, "unrecognized title format or group update"
	}

	pkgIndex := dependabotBumpRegex.SubexpIndex("pkg")
	oldIndex := dependabotBumpRegex.SubexpIndex("old")
	newIndex := dependabotBumpRegex.SubexpIndex("new")

	pkg := matches[pkgIndex]
	oldVer := matches[oldIndex]
	newVer := matches[newIndex]

	// Allow major updates only for cachix because cachix releases major versions frequently.
	if strings.Contains(pkg, "cachix") {
		return false, "allowed cachix update"
	}

	oldMajor, oldMinor := extractVersionParts(oldVer)
	newMajor, newMinor := extractVersionParts(newVer)

	if oldMajor != newMajor {
		return true, fmt.Sprintf("major update (%s -> %s)", oldVer, newVer)
	}

	// SemVer treats minor updates in 0.x releases as breaking changes (e.g. 0.1.0 -> 0.2.0).
	if oldMajor == "0" && oldMinor != newMinor {
		return true, fmt.Sprintf("0.x minor update (%s -> %s)", oldVer, newVer)
	}

	return false, "non-breaking update"
}

func isDisallowedDependabotMajor(title string) bool {
	disallowed, _ := checkDependabotBump(title)
	return disallowed
}

func isAllowedBot(login string, allowedBots []string) bool {
	for _, bot := range allowedBots {
		if login == bot {
			return true
		}
	}
	return false
}

func checkEligibility(pr PullRequest, allowedBots []string) (bool, string) {
	if !isAllowedBot(pr.Author.Login, allowedBots) {
		return false, fmt.Sprintf("author %q is not in allowed bots", pr.Author.Login)
	}
	if pr.IsDraft {
		return false, "PR is a draft"
	}
	if pr.Mergeable != "MERGEABLE" {
		return false, fmt.Sprintf("mergeable status is %q", pr.Mergeable)
	}
	if len(pr.StatusCheckRollup) == 0 {
		return false, "no status checks found"
	}

	// Accept SUCCESS, SKIPPED, and NEUTRAL check conclusions once completed.
	for _, check := range pr.StatusCheckRollup {
		if check.Status != "COMPLETED" {
			return false, fmt.Sprintf("status check is not completed (status: %s)", check.Status)
		}
		if check.Conclusion != "SUCCESS" && check.Conclusion != "SKIPPED" && check.Conclusion != "NEUTRAL" {
			return false, fmt.Sprintf("status check concluded with %q", check.Conclusion)
		}
	}

	if strings.Contains(pr.Author.Login, "dependabot") {
		if disallowed, reason := checkDependabotBump(pr.Title); disallowed {
			return false, fmt.Sprintf("disallowed dependabot bump: %s", reason)
		}
	}

	return true, "eligible for merge"
}

func isEligible(pr PullRequest, allowedBots []string) bool {
	eligible, _ := checkEligibility(pr, allowedBots)
	return eligible
}

func filterEligible(prs []PullRequest, allowedBots []string) []PullRequest {
	var eligible []PullRequest
	for _, pr := range prs {
		if isEligible(pr, allowedBots) {
			eligible = append(eligible, pr)
		}
	}
	return eligible
}

func execute(ctx context.Context, cfg *Config, runner CommandRunner) error {
	prs, err := runner.FetchOpenPRs(ctx)
	if err != nil {
		return err
	}

	if len(prs) == 0 {
		fmt.Println("No open pull requests found.")
		return nil
	}

	var eligible []PullRequest
	for _, pr := range prs {
		ok, reason := checkEligibility(pr, cfg.AllowedBots)
		if ok {
			fmt.Printf("PR #%d (%s): eligible\n", pr.Number, pr.Title)
			eligible = append(eligible, pr)
		} else {
			fmt.Printf("PR #%d (%s): skipped (%s)\n", pr.Number, pr.Title, reason)
		}
	}

	if len(eligible) == 0 {
		fmt.Println("No eligible bot PR found for merge.")
		return nil
	}

	for _, pr := range eligible {
		fmt.Printf("Attempting to merge PR #%d (%s)...\n", pr.Number, pr.Title)
		if err := runner.MergePR(ctx, pr.Number, cfg.MergeMethod); err != nil {
			fmt.Printf("Failed to merge PR #%d (possibly conflicting with a prior merge). Skipping for next run: %v\n", pr.Number, err)
		} else {
			fmt.Printf("Successfully merged PR #%d.\n", pr.Number)
		}
	}

	return nil
}

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	cfg, err := parseConfig(args)
	if err != nil {
		return err
	}
	return execute(ctx, cfg, &DefaultCommandRunner{})
}
