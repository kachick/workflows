package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var defaultAllowedBots = []string{
	"app/dependabot",
	"dependabot[bot]",
	"app/renovate",
	"renovate[bot]",
	"app/samuebot",
	"samuebot[bot]",
}

var (
	dependabotBumpRegex   = regexp.MustCompile(`^Bump (?P<pkg>.+) from (?P<old>[^ ]+) to (?P<new>[^ ]+)(?: in .+)?$`)
	renovateTableRowRegex = regexp.MustCompile(`(?m)^\|\s*(?:\[(?P<pkg>[^\]]+)\]\([^)]+\)|(?P<rawpkg>[^|]+?))\s*\|.*?` + "`(?P<old>[^`]+)`" + `\s*(?:→|->)\s*` + "`(?P<new>[^`]+)`")
)

const (
	vetoLabelName        = "automerge:vetoed"
	vetoLabelDescription = "Vetoed by automerge workflow"
	vetoLabelColor       = "d73a4a"
)

type PullRequest struct {
	Number            int               `json:"number"`
	Title             string            `json:"title"`
	Body              string            `json:"body"`
	Author            Author            `json:"author"`
	IsDraft           bool              `json:"isDraft"`
	Mergeable         string            `json:"mergeable"`
	StatusCheckRollup []StatusCheckItem `json:"statusCheckRollup"`
	Labels            []Label           `json:"labels"`
}

type Label struct {
	Name string `json:"name"`
}

type Author struct {
	Login string `json:"login"`
}

type StatusCheckItem struct {
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

type Config struct {
	AllowedBots      []string
	MergeMethod      string
	LLMFinalVeto     bool
	GeminiAPIKey     string
	GeminiModel      string
	EnableOSVScanner bool
}

type CommandRunner interface {
	FetchOpenPRs(ctx context.Context) ([]PullRequest, error)
	FetchPRDiff(ctx context.Context, number int) (string, error)
	FetchBaseFile(ctx context.Context, path string) ([]byte, error)
	FetchPRFile(ctx context.Context, number int, path string) ([]byte, error)
	EnsureLabel(ctx context.Context, name, description, color string) error
	AddLabelToPR(ctx context.Context, number int, label string) error
	MergePR(ctx context.Context, number int, method string) error
}

type DefaultCommandRunner struct{}

func (r *DefaultCommandRunner) FetchOpenPRs(ctx context.Context) ([]PullRequest, error) {
	cmd := exec.CommandContext(ctx, "gh", "pr", "list",
		"--state", "open",
		"--json", "number,title,body,author,isDraft,mergeable,statusCheckRollup,labels",
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

func (r *DefaultCommandRunner) FetchPRDiff(ctx context.Context, number int) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", "pr", "diff", strconv.Itoa(number))
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to fetch pr diff: %w", err)
	}

	return stdout.String(), nil
}

func (r *DefaultCommandRunner) FetchBaseFile(ctx context.Context, path string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", "show", "HEAD:"+path)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		// File did not exist in base branch
		return nil, nil
	}
	return stdout.Bytes(), nil
}

func (r *DefaultCommandRunner) FetchPRFile(ctx context.Context, number int, path string) ([]byte, error) {
	fetchCmd := exec.CommandContext(ctx, "git", "fetch", "origin", fmt.Sprintf("pull/%d/head", number))
	if err := fetchCmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to git fetch pr %d: %w", number, err)
	}

	showCmd := exec.CommandContext(ctx, "git", "show", "FETCH_HEAD:"+path)
	var stdout bytes.Buffer
	showCmd.Stdout = &stdout
	if err := showCmd.Run(); err != nil {
		// File does not exist in PR branch
		return nil, nil
	}
	return stdout.Bytes(), nil
}

func (r *DefaultCommandRunner) EnsureLabel(ctx context.Context, name, description, color string) error {
	cmd := exec.CommandContext(ctx, "gh", "label", "create", name,
		"--description", description,
		"--color", color,
		"--force",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (r *DefaultCommandRunner) AddLabelToPR(ctx context.Context, number int, label string) error {
	cmd := exec.CommandContext(ctx, "gh", "pr", "edit", strconv.Itoa(number),
		"--add-label", label,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
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
	fs.BoolVar(&cfg.LLMFinalVeto, "llm-final-veto", false, "Enable LLM final veto review before merging")
	fs.StringVar(&cfg.GeminiAPIKey, "gemini-api-key", "", "Gemini API key for LLM final veto")
	fs.StringVar(&cfg.GeminiModel, "gemini-model", "gemini-flash-latest", "Gemini model name for LLM final veto")
	fs.BoolVar(&cfg.EnableOSVScanner, "enable-osv-scanner", true, "Enable OSV-Scanner diff check before merging")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	if env := os.Getenv("ALLOWED_BOTS"); env != "" && allowedBotsRaw == "" {
		allowedBotsRaw = env
	}
	if env := os.Getenv("MERGE_METHOD"); env != "" && cfg.MergeMethod == "squash" {
		cfg.MergeMethod = env
	}
	if env := os.Getenv("LLM_FINAL_VETO"); env != "" && !cfg.LLMFinalVeto {
		if val, err := strconv.ParseBool(env); err == nil {
			cfg.LLMFinalVeto = val
		}
	}
	if env := os.Getenv("GEMINI_API_KEY"); env != "" && cfg.GeminiAPIKey == "" {
		cfg.GeminiAPIKey = env
	}
	if env := os.Getenv("GEMINI_MODEL"); env != "" && cfg.GeminiModel == "gemini-flash-latest" {
		cfg.GeminiModel = env
	}
	if env := os.Getenv("ENABLE_OSV_SCANNER"); env != "" {
		if val, err := strconv.ParseBool(env); err == nil {
			cfg.EnableOSVScanner = val
		}
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

func checkVersionBump(pkg, oldVer, newVer string) (disallowed bool, reason string) {
	// Allow major updates only for cachix because cachix releases major versions frequently.
	if strings.Contains(pkg, "cachix") {
		return false, "allowed cachix update"
	}

	oldMajor, _ := extractVersionParts(oldVer)
	newMajor, _ := extractVersionParts(newVer)

	// SemVer treats 0.x minor bumps as breaking changes, but projects choosing 0.x
	// tools already accept rapid changes. Also allow 0.x to 1.x as the first stable release.
	if oldMajor == "0" {
		if newMajor == "0" || newMajor == "1" {
			return false, "allowed 0.x or initial stable release"
		}
		return true, fmt.Sprintf("major update (%s -> %s)", oldVer, newVer)
	}

	if oldMajor != newMajor {
		return true, fmt.Sprintf("major update (%s -> %s)", oldVer, newVer)
	}

	return false, "non-breaking update"
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

	return checkVersionBump(pkg, oldVer, newVer)
}

// checkRenovateBump parses the Renovate PR body table to extract package version changes.
// Renovate default PR titles omit the source version (e.g. "Update dependency foo to v1.2.3"),
// so inspecting the PR body table is required to distinguish breaking changes from minor updates.
func checkRenovateBump(body string) (disallowed bool, reason string) {
	content := body
	// Stop before release notes or horizontal rule to avoid matching tables inside changelogs.
	for _, marker := range []string{"\n---", "<details>", "### Release Notes"} {
		if idx := strings.Index(content, marker); idx != -1 {
			content = content[:idx]
		}
	}

	matches := renovateTableRowRegex.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return true, "unrecognized body format or no update rows found"
	}

	pkgIndex := renovateTableRowRegex.SubexpIndex("pkg")
	rawpkgIndex := renovateTableRowRegex.SubexpIndex("rawpkg")
	oldIndex := renovateTableRowRegex.SubexpIndex("old")
	newIndex := renovateTableRowRegex.SubexpIndex("new")

	lastReason := "non-breaking update"
	for _, match := range matches {
		pkg := match[pkgIndex]
		if pkg == "" {
			pkg = strings.TrimSpace(match[rawpkgIndex])
		}
		oldVer := match[oldIndex]
		newVer := match[newIndex]

		disallowed, reason := checkVersionBump(pkg, oldVer, newVer)
		if disallowed {
			return true, reason
		}
		lastReason = reason
	}

	if len(matches) == 1 {
		return false, lastReason
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
	for _, label := range pr.Labels {
		if label.Name == vetoLabelName {
			return false, fmt.Sprintf("PR has %q label", vetoLabelName)
		}
	}
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
	if strings.Contains(pr.Author.Login, "renovate") {
		if disallowed, reason := checkRenovateBump(pr.Body); disallowed {
			return false, fmt.Sprintf("disallowed renovate bump: %s", reason)
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

var diffFileRegex = regexp.MustCompile(`(?m)^diff --git a/(\S+) b/(\S+)`)

// Only include lockfiles natively extractable by `osv-scanner scan --lockfile=...`.
// Why not flake.lock: OSV-Scanner does not support Nix flake.lock.
// Why not go.sum: OSV-Scanner scans Go dependencies via go.mod; standalone go.sum fails extraction.
// Why not bun.lockb: OSV-Scanner only supports text-based bun.lock, not binary bun.lockb.
var supportedLockfileNames = map[string]bool{
	"go.mod":            true,
	"package-lock.json": true,
	"yarn.lock":         true,
	"pnpm-lock.yaml":    true,
	"bun.lock":          true,
	"Cargo.lock":        true,
	"Gemfile.lock":      true,
	"poetry.lock":       true,
	"Pipfile.lock":      true,
	"requirements.txt":  true,
	"composer.lock":     true,
	"mix.lock":          true,
}

func isSupportedLockfile(path string) bool {
	base := filepath.Base(path)
	return supportedLockfileNames[base]
}

func extractChangedLockfiles(diff string) []string {
	matches := diffFileRegex.FindAllStringSubmatch(diff, -1)
	seen := make(map[string]bool)
	var lockfiles []string

	for _, m := range matches {
		if len(m) >= 3 {
			filePath := m[2]
			if isSupportedLockfile(filePath) && !seen[filePath] {
				seen[filePath] = true
				lockfiles = append(lockfiles, filePath)
			}
		}
	}
	sort.Strings(lockfiles)
	return lockfiles
}

type VulnerabilityScanner interface {
	ScanPR(ctx context.Context, pr PullRequest, diff string) (veto bool, reason string, err error)
}

type osvScanOutput struct {
	Results []struct {
		Packages []struct {
			Vulnerabilities []struct {
				ID string `json:"id"`
			} `json:"vulnerabilities"`
		} `json:"packages"`
	} `json:"results"`
}

type OSVScanner struct {
	Runner CommandRunner
}

func NewOSVScanner(runner CommandRunner) *OSVScanner {
	return &OSVScanner{Runner: runner}
}

func (s *OSVScanner) ScanPR(ctx context.Context, pr PullRequest, diff string) (bool, string, error) {
	lockfiles := extractChangedLockfiles(diff)
	if len(lockfiles) == 0 {
		return false, "no lockfiles modified", nil
	}

	tmpDir, err := os.MkdirTemp("", "automerge-osv-*")
	if err != nil {
		return false, "", fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	for _, lockfile := range lockfiles {
		baseBytes, err := s.Runner.FetchBaseFile(ctx, lockfile)
		if err != nil {
			return false, "", fmt.Errorf("failed to fetch base file %q: %w", lockfile, err)
		}

		prBytes, err := s.Runner.FetchPRFile(ctx, pr.Number, lockfile)
		if err != nil {
			return false, "", fmt.Errorf("failed to fetch PR file %q: %w", lockfile, err)
		}

		// If the lockfile was deleted in PR, skip scanning
		if len(prBytes) == 0 {
			continue
		}

		baseIDs := make(map[string]bool)
		if len(baseBytes) > 0 {
			ids, err := scanLockfileContent(ctx, tmpDir, "base", lockfile, baseBytes)
			if err != nil {
				return false, "", fmt.Errorf("osv-scanner failed on base %q: %w", lockfile, err)
			}
			for _, id := range ids {
				baseIDs[id] = true
			}
		}

		prIDs, err := scanLockfileContent(ctx, tmpDir, "pr", lockfile, prBytes)
		if err != nil {
			return false, "", fmt.Errorf("osv-scanner failed on PR %q: %w", lockfile, err)
		}

		var newVulns []string
		for _, id := range prIDs {
			if !baseIDs[id] {
				newVulns = append(newVulns, id)
			}
		}

		if len(newVulns) > 0 {
			sort.Strings(newVulns)
			return true, fmt.Sprintf("new vulnerabilities introduced in %s: %s", lockfile, strings.Join(newVulns, ", ")), nil
		}
	}

	return false, "no new vulnerabilities introduced", nil
}

func scanLockfileContent(ctx context.Context, tmpDir, prefix, lockfilePath string, content []byte) ([]string, error) {
	targetDir := filepath.Join(tmpDir, prefix, filepath.Dir(lockfilePath))
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return nil, err
	}

	targetPath := filepath.Join(tmpDir, prefix, lockfilePath)
	if err := os.WriteFile(targetPath, content, 0o600); err != nil {
		return nil, err
	}

	args := []string{"scan", "--format=json", "--no-call-analysis=go"}
	if _, err := os.Stat("osv-scanner.toml"); err == nil {
		args = append(args, "--config=osv-scanner.toml")
	}
	args = append(args, "--lockfile="+targetPath)

	cmd := exec.CommandContext(ctx, "osv-scanner", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	// osv-scanner exits with status 1 when vulnerabilities are found, but still produces valid JSON in stdout.
	if err != nil && stdout.Len() == 0 {
		return nil, fmt.Errorf("osv-scanner command failed (%w): %s", err, strings.TrimSpace(stderr.String()))
	}

	var scanOut osvScanOutput
	if err := json.Unmarshal(stdout.Bytes(), &scanOut); err != nil {
		return nil, fmt.Errorf("failed to parse osv-scanner json output: %w", err)
	}

	idSet := make(map[string]bool)
	for _, res := range scanOut.Results {
		for _, pkg := range res.Packages {
			for _, vuln := range pkg.Vulnerabilities {
				if vuln.ID != "" {
					idSet[vuln.ID] = true
				}
			}
		}
	}

	var ids []string
	for id := range idSet {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

const (
	maxDiffLength = 20000
	maxBodyLength = 10000
)

func truncateText(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "\n...[truncated]"
}

type LLMReviewer interface {
	ReviewPR(ctx context.Context, pr PullRequest, diff string) (veto bool, reason string, err error)
}

type GeminiReviewer struct {
	APIKey     string
	Model      string
	BaseURL    string
	HTTPClient *http.Client
}

func NewGeminiReviewer(apiKey string, model string) *GeminiReviewer {
	return &GeminiReviewer{
		APIKey:  apiKey,
		Model:   model,
		BaseURL: "https://generativelanguage.googleapis.com",
		HTTPClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiSchema struct {
	Type       string                   `json:"type"`
	Properties map[string]*geminiSchema `json:"properties,omitempty"`
	Required   []string                 `json:"required,omitempty"`
}

type geminiGenerationConfig struct {
	ResponseMimeType string        `json:"responseMimeType"`
	ResponseSchema   *geminiSchema `json:"responseSchema,omitempty"`
}

type geminiRequest struct {
	Contents         []geminiContent        `json:"contents"`
	GenerationConfig geminiGenerationConfig `json:"generationConfig"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	Error *struct {
		Message string `json:"message"`
		Code    int    `json:"code"`
	} `json:"error,omitempty"`
}

type vetoDecision struct {
	Veto   bool   `json:"veto"`
	Reason string `json:"reason"`
}

func (g *GeminiReviewer) ReviewPR(ctx context.Context, pr PullRequest, diff string) (bool, string, error) {
	if g.APIKey == "" {
		return false, "", errors.New("GEMINI_API_KEY is empty")
	}

	truncatedBody := truncateText(pr.Body, maxBodyLength)
	truncatedDiff := truncateText(diff, maxDiffLength)

	prompt := fmt.Sprintf(`You are a senior security and compatibility reviewer acting as the final veto gatekeeper for an automated pull request merge bot.
The pull request is already eligible according to all automated CI tests and bot filters.

Your task: Decide whether to VETO (reject/block) the merge.
Your default decision MUST be PASS (veto: false).

STRICT VETO CRITERIA (Only set veto: true if at least one of these is unequivocally met):
1. Malicious Code or Supply Chain Attack: Diff introduces unexpected obfuscated code, suspicious network requests to external domains, exfiltration of environment variables/secrets, or suspicious dependency swapping (e.g. typosquatting).
2. Critical Breaking Changes: The PR description (release notes/changelog) or diff explicitly indicates an unannounced breaking change, removed major API, or severe incompatibility that will break the consumer.

DO NOT VETO (MUST set veto: false) for:
- Minor updates, patch updates, or backward-compatible feature additions.
- Deprecation warnings (as long as functionality is not yet completely broken).
- Code style, formatting, missing tests, or documentation nitpicks.
- Normal bot updates (e.g. Dependabot bumping a minor or patch dependency version).

Pull Request Title: %s
Author: %s

Pull Request Description / Changelog:
%s

Code Diff:
%s
`, pr.Title, pr.Author.Login, truncatedBody, truncatedDiff)

	reqBody := geminiRequest{
		Contents: []geminiContent{
			{
				Parts: []geminiPart{
					{Text: prompt},
				},
			},
		},
		GenerationConfig: geminiGenerationConfig{
			ResponseMimeType: "application/json",
			ResponseSchema: &geminiSchema{
				Type: "OBJECT",
				Properties: map[string]*geminiSchema{
					"veto":   {Type: "BOOLEAN"},
					"reason": {Type: "STRING"},
				},
				Required: []string{"veto", "reason"},
			},
		},
	}

	jsonBytes, err := json.Marshal(reqBody)
	if err != nil {
		return false, "", fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s", g.BaseURL, g.Model, g.APIKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBytes))
	if err != nil {
		return false, "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.HTTPClient.Do(req)
	if err != nil {
		return false, "", fmt.Errorf("gemini api request failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, "", fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return false, "", fmt.Errorf("gemini api returned status %d: %s", resp.StatusCode, string(respBytes))
	}

	var geminiResp geminiResponse
	if err := json.Unmarshal(respBytes, &geminiResp); err != nil {
		return false, "", fmt.Errorf("failed to parse gemini response: %w", err)
	}

	if geminiResp.Error != nil {
		return false, "", fmt.Errorf("gemini api error %d: %s", geminiResp.Error.Code, geminiResp.Error.Message)
	}

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return false, "", errors.New("gemini api returned no candidates or content parts")
	}

	responseText := geminiResp.Candidates[0].Content.Parts[0].Text
	var decision vetoDecision
	if err := json.Unmarshal([]byte(responseText), &decision); err != nil {
		return false, "", fmt.Errorf("failed to parse decision json (%q): %w", responseText, err)
	}

	return decision.Veto, decision.Reason, nil
}

func execute(ctx context.Context, cfg *Config, runner CommandRunner, reviewer LLMReviewer, vulnScanner VulnerabilityScanner) error {
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
		diff, err := runner.FetchPRDiff(ctx, pr.Number)
		if err != nil {
			fmt.Printf("Warning: failed to fetch diff for PR #%d (%v). Skipping merge for next run.\n", pr.Number, err)
			continue
		}

		if vulnScanner != nil {
			fmt.Printf("Running vulnerability diff scan for PR #%d (%s)...\n", pr.Number, pr.Title)
			veto, reason, err := vulnScanner.ScanPR(ctx, pr, diff)
			if err != nil {
				fmt.Printf("Warning: vulnerability scan failed for PR #%d (%v). Skipping merge for next run.\n", pr.Number, err)
				continue
			}

			if veto {
				fmt.Printf("PR #%d (%s): VETOED by vulnerability scan: %s. Marking with %q label and skipping merge.\n", pr.Number, pr.Title, reason, vetoLabelName)
				if err := runner.EnsureLabel(ctx, vetoLabelName, vetoLabelDescription, vetoLabelColor); err != nil {
					fmt.Printf("Warning: failed to ensure label %q: %v\n", vetoLabelName, err)
				}
				if err := runner.AddLabelToPR(ctx, pr.Number, vetoLabelName); err != nil {
					fmt.Printf("Warning: failed to add label %q to PR #%d: %v\n", vetoLabelName, pr.Number, err)
				}
				continue
			}

			fmt.Printf("PR #%d (%s): vulnerability scan passed: %s.\n", pr.Number, pr.Title, reason)
		}

		if cfg.LLMFinalVeto {
			if cfg.GeminiAPIKey == "" {
				fmt.Printf("Warning: llm-final-veto is enabled, but GEMINI_API_KEY is not set. Skipping merge for PR #%d to avoid unreviewed merges.\n", pr.Number)
				continue
			}

			fmt.Printf("Running final veto review for PR #%d (%s)...\n", pr.Number, pr.Title)
			veto, reason, err := reviewer.ReviewPR(ctx, pr, diff)
			if err != nil {
				fmt.Printf("Warning: veto review failed for PR #%d (%v). Skipping merge for next run.\n", pr.Number, err)
				continue
			}

			if veto {
				fmt.Printf("PR #%d (%s): VETOED by review: %s. Marking with %q label and skipping merge.\n", pr.Number, pr.Title, reason, vetoLabelName)
				if err := runner.EnsureLabel(ctx, vetoLabelName, vetoLabelDescription, vetoLabelColor); err != nil {
					fmt.Printf("Warning: failed to ensure label %q: %v\n", vetoLabelName, err)
				}
				if err := runner.AddLabelToPR(ctx, pr.Number, vetoLabelName); err != nil {
					fmt.Printf("Warning: failed to add label %q to PR #%d: %v\n", vetoLabelName, pr.Number, err)
				}
				continue
			}

			fmt.Printf("PR #%d (%s): review passed: %s. Proceeding to merge.\n", pr.Number, pr.Title, reason)
		}

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

	runner := &DefaultCommandRunner{}

	var vulnScanner VulnerabilityScanner
	if cfg.EnableOSVScanner {
		vulnScanner = NewOSVScanner(runner)
	}

	var reviewer LLMReviewer
	if cfg.LLMFinalVeto {
		reviewer = NewGeminiReviewer(cfg.GeminiAPIKey, cfg.GeminiModel)
	}

	return execute(ctx, cfg, runner, reviewer, vulnScanner)
}
