package report

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/kachick/workflows/internal/workflowmeta"
	"github.com/kachick/workflows/pkgs/nix-diff-closures/internal/osv"
)

func TestTruncateUTF8Safe(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxBytes int
		check    func(t *testing.T, output string)
	}{
		{
			name:     "within limit",
			input:    "hello world\n",
			maxBytes: 20,
			check: func(t *testing.T, output string) {
				if output != "hello world\n" {
					t.Fatalf("unexpected: %q", output)
				}
			},
		},
		{
			name:     "multibyte unicode characters does not corrupt",
			input:    "kdeconnect: 20.08.2 → ∅, -6597.8 KiB\n", // '→' and '∅' are 3 bytes each
			maxBytes: 24,                                       // cuts right around '→'
			check: func(t *testing.T, output string) {
				if !utf8.ValidString(output) {
					t.Fatalf("output is not valid UTF-8: %q", output)
				}
			},
		},
		{
			name:     "prefers newline boundary",
			input:    "line 1\nline 2\nline 3\n",
			maxBytes: 15,
			check: func(t *testing.T, output string) {
				expected := "line 1\nline 2\n"
				if output != expected {
					t.Fatalf("expected %q, got %q", expected, output)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateUTF8Safe(tt.input, tt.maxBytes)
			tt.check(t, got)
		})
	}
}

func TestBuildReport(t *testing.T) {
	completeMeta := workflowmeta.Metadata{
		ServerURL:          "https://github.com",
		Repository:         "kachick/workflows",
		RunID:              "12345678",
		WorkflowRepository: "kachick/workflows",
		WorkflowSHA:        "c4c03dd9c5377b99ca53183897baaf53114b86db",
		WorkflowFilePath:   ".github/workflows/reusable-nix-diff-closures.yml",
	}
	expectedProvenance := "This report was generated automatically by [actions/runs/12345678](https://github.com/kachick/workflows/actions/runs/12345678) (defined at [kachick/workflows@c4c03dd](https://github.com/kachick/workflows/blob/c4c03dd9c5377b99ca53183897baaf53114b86db/.github/workflows/reusable-nix-diff-closures.yml))."

	t.Run("no diff across targets", func(t *testing.T) {
		results := []TargetResult{
			{
				Target:   "devShells.x86_64-linux.default",
				NoChange: true,
			},
		}

		rep, err := Build(results, VulnerabilityResult{}, 1000, 500, completeMeta, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rep.HasDiff {
			t.Fatal("expected HasDiff = false")
		}
		if rep.CommentMarkdown != "" {
			t.Fatalf("expected empty markdown, got %q", rep.CommentMarkdown)
		}
	})

	t.Run("single target with diff and provenance header", func(t *testing.T) {
		diffText := "bash: 5.2 → 5.3, +10.0 KiB\n"
		results := []TargetResult{
			{
				Target:   "devShells.x86_64-linux.default",
				DiffText: diffText,
			},
		}

		rep, err := Build(results, VulnerabilityResult{}, 1000, 500, completeMeta, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !rep.HasDiff {
			t.Fatal("expected HasDiff = true")
		}
		if !strings.Contains(rep.CommentMarkdown, Marker) {
			t.Fatal("marker not found in comment")
		}
		if !strings.Contains(rep.CommentMarkdown, expectedProvenance) {
			t.Fatalf("expected provenance %q in comment, got: %s", expectedProvenance, rep.CommentMarkdown)
		}
		if !strings.Contains(rep.CommentMarkdown, diffText) {
			t.Fatal("diff text not found in comment")
		}
		expectedArtifactLink := "https://github.com/kachick/workflows/actions/runs/12345678#artifacts"
		if !strings.Contains(rep.CommentMarkdown, expectedArtifactLink) {
			t.Fatalf("expected artifact link %q in comment, got: %s", expectedArtifactLink, rep.CommentMarkdown)
		}
		if rep.IsTruncated {
			t.Fatal("expected IsTruncated = false")
		}
	})

	t.Run("collects full tree and why-depends texts", func(t *testing.T) {
		results := []TargetResult{
			{
				Target:         "devShells.x86_64-linux.default",
				DiffText:       "bash: 5.2 → 5.3\n",
				TreeText:       "/nix/store/111-drv\n└───/nix/store/222-bash.drv\n",
				WhyDependsText: "# why-depends for bash (5.3)\n/nix/store/111-drv -> /nix/store/222-bash.drv\n",
			},
		}

		rep, err := Build(results, VulnerabilityResult{}, 1000, 500, completeMeta, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(rep.FullTreeText, "=== Target: devShells.x86_64-linux.default ===") {
			t.Fatalf("target header missing in tree text: %s", rep.FullTreeText)
		}
		if !strings.Contains(rep.FullTreeText, "/nix/store/222-bash.drv") {
			t.Fatalf("tree content missing: %s", rep.FullTreeText)
		}
		if !strings.Contains(rep.FullWhyDependsText, "# why-depends for bash (5.3)") {
			t.Fatalf("why-depends content missing: %s", rep.FullWhyDependsText)
		}
	})

	t.Run("multiple targets with truncation", func(t *testing.T) {
		largeDiff := strings.Repeat("pkg: 1.0 → 2.0\n", 50)
		results := []TargetResult{
			{
				Target:   "devShells.x86_64-linux.default",
				DiffText: largeDiff,
			},
			{
				Target:   "packages.x86_64-linux.default",
				NoChange: true,
			},
		}

		maxSize := 100
		headSize := 50
		rep, err := Build(results, VulnerabilityResult{}, maxSize, headSize, completeMeta, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !rep.HasDiff {
			t.Fatal("expected HasDiff = true")
		}
		if !rep.IsTruncated {
			t.Fatal("expected IsTruncated = true")
		}
		expectedArtifactLink := "https://github.com/kachick/workflows/actions/runs/12345678#artifacts"
		if !strings.Contains(rep.CommentMarkdown, expectedArtifactLink) {
			t.Fatalf("expected artifact link %q, got: %s", expectedArtifactLink, rep.CommentMarkdown)
		}
		if !strings.Contains(rep.CommentMarkdown, "<summary><b>packages.x86_64-linux.default</b> (unchanged)</summary>") {
			t.Fatal("unchanged target summary expected")
		}
	})

	t.Run("incomplete metadata fails fast in CI environment", func(t *testing.T) {
		incomplete := completeMeta
		incomplete.WorkflowFilePath = ""
		results := []TargetResult{
			{
				Target:   "devShells.x86_64-linux.default",
				DiffText: "diff",
			},
		}

		_, err := Build(results, VulnerabilityResult{}, 1000, 500, incomplete, true)
		if err == nil {
			t.Fatal("expected error when workflow metadata is incomplete in CI, got nil")
		}
	})

	t.Run("incomplete metadata falls back to default outside CI", func(t *testing.T) {
		incomplete := workflowmeta.Metadata{}
		results := []TargetResult{
			{
				Target:   "devShells.x86_64-linux.default",
				DiffText: "diff",
			},
		}

		rep, err := Build(results, VulnerabilityResult{}, 1000, 500, incomplete, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expectedFallback := "This report was generated automatically."
		if !strings.Contains(rep.CommentMarkdown, expectedFallback) {
			t.Fatalf("expected fallback text %q in comment, got: %s", expectedFallback, rep.CommentMarkdown)
		}
	})

	t.Run("formats newly detected vulnerabilities with caution alert", func(t *testing.T) {
		results := []TargetResult{
			{
				Target:   "devShells.x86_64-linux.default",
				DiffText: "zod: 3.22.0 → 3.22.4\n",
			},
		}
		vulnRes := VulnerabilityResult{
			Checked: true,
			NewVulns: []osv.PackageVulnerabilities{
				{
					Package: osv.Package{Name: "zod", Version: "3.22.4"},
					Vulnerabilities: []osv.Vulnerability{
						{ID: "GHSA-m95q-7qp3-xv42"},
					},
				},
			},
		}

		rep, err := Build(results, vulnRes, DefaultMaxSize, DefaultHeadSize, completeMeta, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(rep.Vulnerabilities) != 1 {
			t.Fatalf("expected 1 vulnerability result, got %d", len(rep.Vulnerabilities))
		}
		if !strings.Contains(rep.CommentMarkdown, "> [!CAUTION]") {
			t.Fatal("expected caution alert in comment markdown")
		}
		if !strings.Contains(rep.CommentMarkdown, "**Newly Introduced Vulnerabilities Detected in Updated Closure**") {
			t.Fatal("expected newly introduced heading in caution alert")
		}
		if !strings.Contains(rep.CommentMarkdown, "[GHSA-m95q-7qp3-xv42](https://osv.dev/vulnerability/GHSA-m95q-7qp3-xv42)") {
			t.Fatalf("expected linked vulnerability ID in markdown, got:\n%s", rep.CommentMarkdown)
		}
		if !strings.Contains(rep.CommentMarkdown, "fails intentionally") {
			t.Fatal("expected explanation note that check fails intentionally")
		}
	})

	t.Run("formats no newly introduced vulnerabilities with note alert", func(t *testing.T) {
		results := []TargetResult{
			{
				Target:   "devShells.x86_64-linux.default",
				DiffText: "nix: 2.31.5.drv → 2.31.6.drv\n",
			},
		}
		vulnRes := VulnerabilityResult{
			Checked:  true,
			NewVulns: nil,
			ExistingVulns: []osv.PackageVulnerabilities{
				{
					Package: osv.Package{Name: "nix", Version: "2.31.6.drv"},
					Vulnerabilities: []osv.Vulnerability{
						{ID: "DEBIAN-CVE-2024-27297"},
					},
				},
			},
		}

		rep, err := Build(results, vulnRes, DefaultMaxSize, DefaultHeadSize, completeMeta, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(rep.Vulnerabilities) != 0 {
			t.Fatalf("expected 0 new vulnerability results, got %d", len(rep.Vulnerabilities))
		}
		if len(rep.ExistingVulnerabilities) != 1 {
			t.Fatalf("expected 1 existing vulnerability result, got %d", len(rep.ExistingVulnerabilities))
		}
		if strings.Contains(rep.CommentMarkdown, "> [!CAUTION]") {
			t.Fatal("unexpected caution alert when no new vulnerabilities introduced")
		}
		expectedNote := "> **Vulnerability Scan**: No newly introduced vulnerabilities detected in updated closure."
		if !strings.Contains(rep.CommentMarkdown, expectedNote) {
			t.Fatalf("expected note %q in comment markdown, got:\n%s", expectedNote, rep.CommentMarkdown)
		}
		// CommentMarkdown should NOT show existing vulnerabilities details
		if strings.Contains(rep.CommentMarkdown, "DEBIAN-CVE-2024-27297") {
			t.Fatal("comment markdown should not list pre-existing vulnerabilities")
		}
		// StepSummaryMarkdown should summarize existing vulnerabilities without dumping full CVE lists
		if strings.Contains(rep.StepSummaryMarkdown, "DEBIAN-CVE-2024-27297") {
			t.Fatal("step summary markdown should not list pre-existing CVE IDs directly")
		}
		if !strings.Contains(rep.StepSummaryMarkdown, "Existing vulnerabilities summary (1 package(s) still affected)") {
			t.Fatalf("expected existing vulnerabilities summary header in step summary, got:\n%s", rep.StepSummaryMarkdown)
		}
		// FullVulnerabilitiesText SHOULD contain full pre-existing vulnerability details
		if !strings.Contains(rep.FullVulnerabilitiesText, "DEBIAN-CVE-2024-27297") {
			t.Fatalf("full vulnerabilities text should list pre-existing vulnerability details, got:\n%s", rep.FullVulnerabilitiesText)
		}
	})

	t.Run("formats resolved vulnerabilities with tip alert", func(t *testing.T) {
		results := []TargetResult{
			{
				Target:   "devShells.x86_64-linux.default",
				DiffText: "bash: 5.3p15.drv → 5.3p20.drv\n",
			},
		}
		vulnRes := VulnerabilityResult{
			Checked: true,
			ResolvedVulns: []osv.PackageVulnerabilities{
				{
					Package: osv.Package{Name: "bash", Version: "5.3p15.drv"},
					Vulnerabilities: []osv.Vulnerability{
						{ID: "CVE-2026-99999"},
					},
				},
			},
		}

		rep, err := Build(results, vulnRes, DefaultMaxSize, DefaultHeadSize, completeMeta, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(rep.ResolvedVulnerabilities) != 1 {
			t.Fatalf("expected 1 resolved vulnerability result, got %d", len(rep.ResolvedVulnerabilities))
		}
		if !strings.Contains(rep.CommentMarkdown, "> [!TIP]") {
			t.Fatal("expected tip alert in comment markdown for resolved vulnerabilities")
		}
		if !strings.Contains(rep.CommentMarkdown, "**Resolved Vulnerabilities in Updated Closure**") {
			t.Fatal("expected resolved heading in tip alert")
		}
		if !strings.Contains(rep.CommentMarkdown, "[CVE-2026-99999](https://osv.dev/vulnerability/CVE-2026-99999)") {
			t.Fatalf("expected linked vulnerability ID in markdown, got:\n%s", rep.CommentMarkdown)
		}
		if !strings.Contains(rep.FullVulnerabilitiesText, "CVE-2026-99999") {
			t.Fatalf("full vulnerabilities text should contain resolved vulnerability, got:\n%s", rep.FullVulnerabilitiesText)
		}
	})

	t.Run("escapes HTML in target name and fences error output safely", func(t *testing.T) {
		results := []TargetResult{
			{
				Target:   "packages.x86_64-linux.<script>",
				DiffText: "pkg: 1.0 → 2.0\n",
			},
			{
				Target: "devShells.x86_64-linux.broken",
				Error:  "evaluation failed: unexpected </details> and ``` in trace",
			},
		}

		rep, err := Build(results, VulnerabilityResult{}, 1000, 500, completeMeta, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(rep.CommentMarkdown, "<summary><b>packages.x86_64-linux.&lt;script&gt;</b> (changed)</summary>") {
			t.Errorf("expected escaped target name in summary, got:\n%s", rep.CommentMarkdown)
		}
		if !strings.Contains(rep.CommentMarkdown, "````text\nError: evaluation failed: unexpected </details> and ``` in trace\n````") {
			t.Errorf("expected 4-backtick code fence around error containing 3 backticks, got:\n%s", rep.CommentMarkdown)
		}
	})

	t.Run("limits displayed CVE links per package and summarizes excess", func(t *testing.T) {
		results := []TargetResult{
			{
				Target:   "devShells.x86_64-linux.default",
				DiffText: "curl: 8.4.0 → 8.5.0\n",
			},
		}
		var vulns []osv.Vulnerability
		for i := 1; i <= 15; i++ {
			vulns = append(vulns, osv.Vulnerability{ID: fmt.Sprintf("CVE-2026-%05d", i)})
		}
		vulnRes := VulnerabilityResult{
			Checked: true,
			NewVulns: []osv.PackageVulnerabilities{
				{
					Package:         osv.Package{Name: "curl", Version: "8.5.0"},
					Vulnerabilities: vulns,
				},
			},
		}

		rep, err := Build(results, vulnRes, 10000, 5000, completeMeta, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Should show first 5
		for i := 1; i <= 5; i++ {
			expectedLink := fmt.Sprintf("https://osv.dev/vulnerability/CVE-2026-%05d", i)
			if !strings.Contains(rep.CommentMarkdown, expectedLink) {
				t.Fatalf("expected CVE link %q in comment", expectedLink)
			}
		}
		// Should not show 6th in comment
		unexpectedLink := "https://osv.dev/vulnerability/CVE-2026-00006"
		if strings.Contains(rep.CommentMarkdown, unexpectedLink) {
			t.Fatalf("unexpected 6th CVE link in comment: %s", rep.CommentMarkdown)
		}
		if !strings.Contains(rep.CommentMarkdown, "and 10 more (see `vulnerabilities.txt` artifact)") {
			t.Fatalf("expected excess summary in comment, got:\n%s", rep.CommentMarkdown)
		}
		// Full report should still have all 15
		for i := 1; i <= 15; i++ {
			cveID := fmt.Sprintf("CVE-2026-%05d", i)
			if !strings.Contains(rep.FullVulnerabilitiesText, cveID) {
				t.Fatalf("expected CVE %q in full text", cveID)
			}
		}
	})

	t.Run("truncates long target error output", func(t *testing.T) {
		longError := strings.Repeat("nix evaluation error trace line\n", 200) // ~6400 bytes
		results := []TargetResult{
			{
				Target:   "devShells.x86_64-linux.default",
				DiffText: "pkg: 1.0 → 2.0\n",
			},
			{
				Target: "packages.x86_64-linux.broken",
				Error:  longError,
			},
		}
		rep, err := Build(results, VulnerabilityResult{}, 10000, 5000, completeMeta, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(rep.CommentMarkdown, "... (truncated error, see CI log)") {
			t.Fatal("expected truncated error marker in comment markdown")
		}
		if len(rep.CommentMarkdown) > 5000 {
			t.Fatalf("comment markdown too large for truncated error: %d bytes", len(rep.CommentMarkdown))
		}
	})
}

func TestEnsureSafeCommentLength(t *testing.T) {
	completeMeta := workflowmeta.Metadata{
		ServerURL:          "https://github.com",
		Repository:         "kachick/workflows",
		RunID:              "12345678",
		WorkflowRepository: "kachick/workflows",
		WorkflowSHA:        "0123456789abcdef",
		WorkflowFilePath:   ".github/workflows/reusable-nix-diff-closures.yml",
	}

	t.Run("returns original comment when within safe limit", func(t *testing.T) {
		comment := "### ❄️ Nix Store Diff\n<!-- nix-diff-closures-report -->\n\nShort comment."
		got := EnsureSafeCommentLength(comment, completeMeta)
		if got != comment {
			t.Fatalf("expected unchanged comment, got: %s", got)
		}
	})

	t.Run("guarantees safe size when comment exceeds limit", func(t *testing.T) {
		giantComment := strings.Repeat("a very long comment line that exceeds limits\n", 2000) // ~90 KB
		got := EnsureSafeCommentLength(giantComment, completeMeta)

		if utf8.RuneCountInString(got) > SafeCommentLimit {
			t.Fatalf("expected runes <= %d, got %d", SafeCommentLimit, utf8.RuneCountInString(got))
		}
		if len(got) > SafeCommentLimit {
			t.Fatalf("expected bytes <= %d, got %d", SafeCommentLimit, len(got))
		}
		if !strings.Contains(got, Marker) {
			t.Fatal("marker missing in fallback comment")
		}
		if !strings.Contains(got, "> [!WARNING]") {
			t.Fatal("warning alert missing in fallback comment")
		}
		if !strings.Contains(got, "exceeded GitHub size limits") {
			t.Fatal("size limit warning text missing")
		}
		if !strings.Contains(got, "... (truncated)") {
			t.Fatal("truncated marker missing")
		}
	})
}
