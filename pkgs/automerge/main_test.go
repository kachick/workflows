package main

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func TestParseConfigDefaults(t *testing.T) {
	cfg, err := parseConfig([]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.MergeMethod != "squash" {
		t.Errorf("expected default merge-method 'squash', got %q", cfg.MergeMethod)
	}
	if cfg.DryRun != false {
		t.Errorf("expected default dry-run false, got %v", cfg.DryRun)
	}
	if !slices.Equal(cfg.AllowedBots, defaultAllowedBots) {
		t.Errorf("expected default allowed bots, got %v", cfg.AllowedBots)
	}
}

func TestParseConfigFlags(t *testing.T) {
	args := []string{
		"--merge-method=rebase",
		"--allowed-bots=custom-bot,another-bot",
		"--dry-run=true",
	}

	cfg, err := parseConfig(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.MergeMethod != "rebase" {
		t.Errorf("expected 'rebase', got %q", cfg.MergeMethod)
	}
	if cfg.DryRun != true {
		t.Errorf("expected dry-run true, got %v", cfg.DryRun)
	}
	expectedBots := []string{"custom-bot", "another-bot"}
	if !slices.Equal(cfg.AllowedBots, expectedBots) {
		t.Errorf("expected %v, got %v", expectedBots, cfg.AllowedBots)
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
	t.Setenv("DRY_RUN", "true")

	cfg, err := parseConfig([]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.MergeMethod != "merge" {
		t.Errorf("expected merge-method from env, got %q", cfg.MergeMethod)
	}
	if cfg.DryRun != true {
		t.Errorf("expected dry-run from env, got %v", cfg.DryRun)
	}
	expectedBots := []string{"env-bot"}
	if !slices.Equal(cfg.AllowedBots, expectedBots) {
		t.Errorf("expected %v, got %v", expectedBots, cfg.AllowedBots)
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
		{title: "Bump cachix/cachix-action from 14 to 15", disallowed: false},
		{title: "Bump cachix/install-nix-action from v26 to v27", disallowed: false},
		{title: "Update dependencies", disallowed: false},
	}

	for _, tc := range tests {
		got := isDisallowedDependabotMajor(tc.title)
		if got != tc.disallowed {
			t.Errorf("isDisallowedDependabotMajor(%q) = %v, want %v", tc.title, got, tc.disallowed)
		}
	}
}

func TestIsEligible(t *testing.T) {
	validChecks := []StatusCheckItem{
		{Status: "COMPLETED", Conclusion: "SUCCESS"},
		{Status: "COMPLETED", Conclusion: "SUCCESS"},
	}

	basePR := PullRequest{
		Number:            1,
		Title:             "Update dependency",
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

	// Skipped checks (strict policy: only SUCCESS is allowed)
	skippedPR := basePR
	skippedPR.StatusCheckRollup = []StatusCheckItem{
		{Status: "COMPLETED", Conclusion: "SKIPPED"},
	}
	if isEligible(skippedPR, defaultAllowedBots) {
		t.Errorf("PR with skipped checks should not be eligible under strict policy")
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

	// Dependabot cachix major bump (eligible)
	dependabotCachixPR := basePR
	dependabotCachixPR.Author.Login = "dependabot[bot]"
	dependabotCachixPR.Title = "Bump cachix/cachix-action from 14 to 15"
	if !isEligible(dependabotCachixPR, defaultAllowedBots) {
		t.Errorf("Dependabot cachix major bump should be eligible")
	}
}

type mockRunner struct {
	prs        []PullRequest
	fetchErr   error
	mergedPRs  []int
	mergeErrOn map[int]error
}

func (m *mockRunner) FetchOpenPRs(ctx context.Context) ([]PullRequest, error) {
	if m.fetchErr != nil {
		return nil, m.fetchErr
	}
	return m.prs, nil
}

func (m *mockRunner) MergePR(ctx context.Context, number int, method string) error {
	if err, ok := m.mergeErrOn[number]; ok {
		return err
	}
	m.mergedPRs = append(m.mergedPRs, number)
	return nil
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
				Author:            Author{Login: "renovate[bot]"},
				Mergeable:         "MERGEABLE",
				StatusCheckRollup: validChecks,
			},
			{
				Number:            102,
				Title:             "Draft PR",
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
		DryRun:      false,
	}

	err := execute(context.Background(), cfg, runner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 101 should fail, 102 is skipped (draft), 103 should succeed
	expectedMerged := []int{103}
	if !slices.Equal(runner.mergedPRs, expectedMerged) {
		t.Errorf("expected merged PRs %v, got %v", expectedMerged, runner.mergedPRs)
	}
}

func TestExecuteDryRun(t *testing.T) {
	validChecks := []StatusCheckItem{
		{Status: "COMPLETED", Conclusion: "SUCCESS"},
	}

	runner := &mockRunner{
		prs: []PullRequest{
			{
				Number:            201,
				Title:             "Renovate PR",
				Author:            Author{Login: "renovate[bot]"},
				Mergeable:         "MERGEABLE",
				StatusCheckRollup: validChecks,
			},
		},
	}

	cfg := &Config{
		AllowedBots: defaultAllowedBots,
		MergeMethod: "squash",
		DryRun:      true,
	}

	err := execute(context.Background(), cfg, runner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(runner.mergedPRs) > 0 {
		t.Errorf("expected no PRs merged in dry-run, got %v", runner.mergedPRs)
	}
}
