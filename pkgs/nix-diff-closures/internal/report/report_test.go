package report

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/kachick/workflows/internal/workflowmeta"
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

		rep, err := Build(results, 1000, 500, completeMeta, true)
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

		rep, err := Build(results, 1000, 500, completeMeta, true)
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
		if rep.IsTruncated {
			t.Fatal("expected IsTruncated = false")
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
		rep, err := Build(results, maxSize, headSize, completeMeta, true)
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

		_, err := Build(results, 1000, 500, incomplete, true)
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

		rep, err := Build(results, 1000, 500, incomplete, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expectedFallback := "This report was generated automatically."
		if !strings.Contains(rep.CommentMarkdown, expectedFallback) {
			t.Fatalf("expected fallback text %q in comment, got: %s", expectedFallback, rep.CommentMarkdown)
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

		rep, err := Build(results, 1000, 500, completeMeta, true)
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
}
