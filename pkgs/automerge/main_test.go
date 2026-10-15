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
	if !slices.Equal(cfg.AllowedBots, defaultAllowedBots) {
		t.Errorf("expected default allowed bots, got %v", cfg.AllowedBots)
	}
}

func TestParseConfigFlags(t *testing.T) {
	args := []string{
		"--merge-method=rebase",
		"--allowed-bots=custom-bot,another-bot",
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
		{title: "Bump cachix/cachix-action from 14 to 15", disallowed: false},
		{title: "Bump cachix/install-nix-action from v26 to v27", disallowed: false},
		{title: "Bump the rust group with 2 updates", disallowed: true},
		{title: "Bump the dev-dependencies group in /sub/dir with 3 updates", disallowed: true},
		{title: "Bump foo from 0.1.0 to 0.2.0", disallowed: true},
		{title: "Bump foo from 0.1.0 to 0.1.1", disallowed: false},
		{title: "Bump foo from v0.1.0 to v0.2.0", disallowed: true},
		{title: "Bump foo from v0.1.0 to v0.1.1", disallowed: false},
		{title: "Bump foo from 0.1 to 0.2", disallowed: true},
		{title: "Bump foo from 0.1 to 0.1.1", disallowed: false},
		{title: "Bump foo from 0.9.0 to 1.0.0", disallowed: true},
		{title: "Bump golang.org/x/sys from 0.48.0 to 0.49.0", disallowed: true},
		{title: "Update dependencies", disallowed: true},
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

func TestExecuteNoOpenPRs(t *testing.T) {
	runner := &mockRunner{prs: []PullRequest{}}
	cfg := &Config{AllowedBots: defaultAllowedBots, MergeMethod: "squash"}
	if err := execute(context.Background(), cfg, runner); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCheckDependabotBump(t *testing.T) {
	tests := []struct {
		title      string
		disallowed bool
		wantReason string
	}{
		{
			title:      "Bump actions/checkout from 4.0.0 to 4.1.0",
			disallowed: false,
			wantReason: "non-breaking update",
		},
		{
			title:      "Bump actions/checkout from 3.0.0 to 4.0.0",
			disallowed: true,
			wantReason: "major update (3.0.0 -> 4.0.0)",
		},
		{
			title:      "Bump golang.org/x/sys from 0.48.0 to 0.49.0",
			disallowed: true,
			wantReason: "0.x minor update (0.48.0 -> 0.49.0)",
		},
		{
			title:      "Bump cachix/cachix-action from 14 to 15",
			disallowed: false,
			wantReason: "allowed cachix update",
		},
		{
			title:      "Bump the rust group with 2 updates",
			disallowed: true,
			wantReason: "unrecognized title format or group update",
		},
	}

	for _, tc := range tests {
		disallowed, reason := checkDependabotBump(tc.title)
		if disallowed != tc.disallowed {
			t.Errorf("checkDependabotBump(%q) disallowed = %v, want %v", tc.title, disallowed, tc.disallowed)
		}
		if reason != tc.wantReason {
			t.Errorf("checkDependabotBump(%q) reason = %q, want %q", tc.title, reason, tc.wantReason)
		}
	}
}

func TestCheckEligibility(t *testing.T) {
	validChecks := []StatusCheckItem{
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

	ok, reason := checkEligibility(basePR, defaultAllowedBots)
	if !ok || reason != "eligible for merge" {
		t.Fatalf("expected eligible for merge, got ok=%v, reason=%q", ok, reason)
	}

	tests := []struct {
		name       string
		modify     func(pr *PullRequest)
		wantReason string
	}{
		{
			name: "unallowed author",
			modify: func(pr *PullRequest) {
				pr.Author.Login = "attacker[bot]"
			},
			wantReason: `author "attacker[bot]" is not in allowed bots`,
		},
		{
			name: "draft PR",
			modify: func(pr *PullRequest) {
				pr.IsDraft = true
			},
			wantReason: "PR is a draft",
		},
		{
			name: "conflicting PR",
			modify: func(pr *PullRequest) {
				pr.Mergeable = "CONFLICTING"
			},
			wantReason: `mergeable status is "CONFLICTING"`,
		},
		{
			name: "empty checks",
			modify: func(pr *PullRequest) {
				pr.StatusCheckRollup = []StatusCheckItem{}
			},
			wantReason: "no status checks found",
		},
		{
			name: "in progress check",
			modify: func(pr *PullRequest) {
				pr.StatusCheckRollup = []StatusCheckItem{
					{Status: "IN_PROGRESS", Conclusion: ""},
				}
			},
			wantReason: "status check is not completed (status: IN_PROGRESS)",
		},
		{
			name: "failed check",
			modify: func(pr *PullRequest) {
				pr.StatusCheckRollup = []StatusCheckItem{
					{Status: "COMPLETED", Conclusion: "FAILURE"},
				}
			},
			wantReason: `status check concluded with "FAILURE"`,
		},
		{
			name: "dependabot 0.x minor update",
			modify: func(pr *PullRequest) {
				pr.Author.Login = "dependabot[bot]"
				pr.Title = "Bump golang.org/x/sys from 0.48.0 to 0.49.0"
			},
			wantReason: "disallowed dependabot bump: 0.x minor update (0.48.0 -> 0.49.0)",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pr := basePR
			tc.modify(&pr)
			ok, reason := checkEligibility(pr, defaultAllowedBots)
			if ok {
				t.Fatalf("expected PR to be ineligible")
			}
			if reason != tc.wantReason {
				t.Errorf("checkEligibility() reason = %q, want %q", reason, tc.wantReason)
			}
		})
	}
}
