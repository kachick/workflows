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
	"regexp"
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

var dependabotBumpRegex = regexp.MustCompile(`^Bump (?P<pkg>.+) from (?P<old>[^ ]+) to (?P<new>[^ ]+)(?: in .+)?$`)

type PullRequest struct {
	Number            int               `json:"number"`
	Title             string            `json:"title"`
	Body              string            `json:"body"`
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
	AllowedBots  []string
	MergeMethod  string
	LLMFinalVeto bool
	GeminiAPIKey string
	GeminiModel  string
}

type CommandRunner interface {
	FetchOpenPRs(ctx context.Context) ([]PullRequest, error)
	FetchPRDiff(ctx context.Context, number int) (string, error)
	MergePR(ctx context.Context, number int, method string) error
}

type DefaultCommandRunner struct{}

func (r *DefaultCommandRunner) FetchOpenPRs(ctx context.Context) ([]PullRequest, error) {
	cmd := exec.CommandContext(ctx, "gh", "pr", "list",
		"--state", "open",
		"--json", "number,title,body,author,isDraft,mergeable,statusCheckRollup",
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
	fs.StringVar(&cfg.GeminiModel, "gemini-model", "gemini-3.8-flash", "Gemini model name for LLM final veto")

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
	if env := os.Getenv("GEMINI_MODEL"); env != "" && cfg.GeminiModel == "gemini-3.8-flash" {
		cfg.GeminiModel = env
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
1. Critical Breaking Changes: The PR description (release notes/changelog) or diff explicitly indicates an unannounced breaking change, removed major API, or severe incompatibility that will break the consumer.
2. Malicious Code or Supply Chain Attack: Diff introduces unexpected obfuscated code, suspicious network requests to external domains, exfiltration of environment variables/secrets, or suspicious dependency swapping (e.g. typosquatting).

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

func execute(ctx context.Context, cfg *Config, runner CommandRunner, reviewer LLMReviewer) error {
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
		if cfg.LLMFinalVeto {
			if cfg.GeminiAPIKey == "" {
				fmt.Printf("Warning: llm-final-veto is enabled, but GEMINI_API_KEY is not set. Skipping merge for PR #%d to avoid unreviewed merges.\n", pr.Number)
				continue
			}

			fmt.Printf("Running LLM final veto review for PR #%d (%s)...\n", pr.Number, pr.Title)
			diff, err := runner.FetchPRDiff(ctx, pr.Number)
			if err != nil {
				fmt.Printf("Warning: failed to fetch diff for PR #%d (%v). Skipping merge for next run.\n", pr.Number, err)
				continue
			}

			veto, reason, err := reviewer.ReviewPR(ctx, pr, diff)
			if err != nil {
				fmt.Printf("Warning: LLM veto review failed for PR #%d (%v). Skipping merge for next run.\n", pr.Number, err)
				continue
			}

			if veto {
				fmt.Printf("PR #%d (%s): VETOED by LLM review: %s. Skipping merge.\n", pr.Number, pr.Title, reason)
				continue
			}

			fmt.Printf("PR #%d (%s): LLM review passed: %s. Proceeding to merge.\n", pr.Number, pr.Title, reason)
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

	var reviewer LLMReviewer
	if cfg.LLMFinalVeto {
		reviewer = NewGeminiReviewer(cfg.GeminiAPIKey, cfg.GeminiModel)
	}

	return execute(ctx, cfg, &DefaultCommandRunner{}, reviewer)
}
