package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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
	if cfg.LLMFinalVeto {
		t.Errorf("expected default llm-final-veto false, got %v", cfg.LLMFinalVeto)
	}
	if cfg.GeminiAPIKey != "" {
		t.Errorf("expected default gemini-api-key empty, got %q", cfg.GeminiAPIKey)
	}
	if cfg.GeminiModel != "gemini-3.8-flash" {
		t.Errorf("expected default gemini-model 'gemini-3.8-flash', got %q", cfg.GeminiModel)
	}
}

func TestParseConfigFlags(t *testing.T) {
	args := []string{
		"--merge-method=rebase",
		"--allowed-bots=custom-bot,another-bot",
		"--llm-final-veto=true",
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
	if !cfg.LLMFinalVeto {
		t.Errorf("expected llm-final-veto true, got %v", cfg.LLMFinalVeto)
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
	t.Setenv("LLM_FINAL_VETO", "true")
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
	if !cfg.LLMFinalVeto {
		t.Errorf("expected llm-final-veto from env, got %v", cfg.LLMFinalVeto)
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
		{title: "Bump cachix/cachix-action from 14 to 15", disallowed: false},
		{title: "Bump cachix/install-nix-action from v26 to v27", disallowed: false},
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
	diffs      map[int]string
	fetchErr   error
	diffErrOn  map[int]error
	mergedPRs  []int
	mergeErrOn map[int]error
}

func (m *mockRunner) FetchOpenPRs(ctx context.Context) ([]PullRequest, error) {
	if m.fetchErr != nil {
		return nil, m.fetchErr
	}
	return m.prs, nil
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

func (m *mockRunner) MergePR(ctx context.Context, number int, method string) error {
	if err, ok := m.mergeErrOn[number]; ok {
		return err
	}
	m.mergedPRs = append(m.mergedPRs, number)
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

	err := execute(context.Background(), cfg, runner, nil)
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
	if err := execute(context.Background(), cfg, runner, nil); err != nil {
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
			Author:            Author{Login: "renovate[bot]"},
			Mergeable:         "MERGEABLE",
			StatusCheckRollup: validChecks,
		},
		{
			Number:            202,
			Title:             "Breaking renovate PR",
			Author:            Author{Login: "renovate[bot]"},
			Mergeable:         "MERGEABLE",
			StatusCheckRollup: validChecks,
		},
		{
			Number:            203,
			Title:             "Flaky API renovate PR",
			Author:            Author{Login: "renovate[bot]"},
			Mergeable:         "MERGEABLE",
			StatusCheckRollup: validChecks,
		},
		{
			Number:            204,
			Title:             "Diff error renovate PR",
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
		LLMFinalVeto: true,
		GeminiAPIKey: "dummy-key",
		GeminiModel:  "gemini-3.8-flash",
	}

	err := execute(context.Background(), cfg, runner, reviewer)
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
				Author:            Author{Login: "renovate[bot]"},
				Mergeable:         "MERGEABLE",
				StatusCheckRollup: validChecks,
			},
		},
	}

	cfg := &Config{
		AllowedBots:  defaultAllowedBots,
		MergeMethod:  "squash",
		LLMFinalVeto: true,
		GeminiAPIKey: "", // missing API key
	}

	err := execute(context.Background(), cfg, runner, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Merging should be skipped safely when key is missing to avoid unreviewed merges
	if len(runner.mergedPRs) != 0 {
		t.Errorf("expected 0 merged PRs when API key is missing, got %v", runner.mergedPRs)
	}
}

func TestGeminiReviewerHTTP(t *testing.T) {
	t.Run("successful approval", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("expected POST, got %s", r.Method)
			}
			if key := r.URL.Query().Get("key"); key != "test-api-key" {
				t.Errorf("expected key 'test-api-key', got %q", key)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"candidates": [
					{
						"content": {
							"parts": [
								{
									"text": "{\"veto\": false, \"reason\": \"safe patch update\"}"
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
									"text": "{\"veto\": true, \"reason\": \"breaking change detected\"}"
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
		if reason != "breaking change detected" {
			t.Errorf("expected reason 'breaking change detected', got %q", reason)
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
			disallowed: false,
			wantReason: "allowed 0.x or initial stable release",
		},
		{
			title:      "Bump foo from 0.9.0 to 1.0.0",
			disallowed: false,
			wantReason: "allowed 0.x or initial stable release",
		},
		{
			title:      "Bump foo from 0.1.0 to 2.0.0",
			disallowed: true,
			wantReason: "major update (0.1.0 -> 2.0.0)",
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

	dependabot0xPR := basePR
	dependabot0xPR.Author.Login = "dependabot[bot]"
	dependabot0xPR.Title = "Bump golang.org/x/sys from 0.48.0 to 0.49.0"
	if ok, reason := checkEligibility(dependabot0xPR, defaultAllowedBots); !ok {
		t.Fatalf("expected dependabot 0.x bump to be eligible, got reason=%q", reason)
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
			name: "dependabot major update",
			modify: func(pr *PullRequest) {
				pr.Author.Login = "dependabot[bot]"
				pr.Title = "Bump actions/checkout from 3.0.0 to 4.0.0"
			},
			wantReason: "disallowed dependabot bump: major update (3.0.0 -> 4.0.0)",
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
