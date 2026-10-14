package report

import (
	"strings"
	"testing"
	"unicode/utf8"
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
	t.Run("no diff across targets", func(t *testing.T) {
		results := []TargetResult{
			{
				Target:   "devShells.x86_64-linux.default",
				NoChange: true,
			},
		}

		rep := Build(results, 1000, 500, "", "", "")
		if rep.HasDiff {
			t.Fatal("expected HasDiff = false")
		}
		if rep.CommentMarkdown != "" {
			t.Fatalf("expected empty markdown, got %q", rep.CommentMarkdown)
		}
	})

	t.Run("single target with diff", func(t *testing.T) {
		diffText := "bash: 5.2 → 5.3, +10.0 KiB\n"
		results := []TargetResult{
			{
				Target:   "devShells.x86_64-linux.default",
				DiffText: diffText,
			},
		}

		rep := Build(results, 1000, 500, "https://github.com", "owner/repo", "123")
		if !rep.HasDiff {
			t.Fatal("expected HasDiff = true")
		}
		if !strings.Contains(rep.CommentMarkdown, Marker) {
			t.Fatal("marker not found in comment")
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
		rep := Build(results, maxSize, headSize, "https://github.com", "owner/repo", "123")
		if !rep.HasDiff {
			t.Fatal("expected HasDiff = true")
		}
		if !rep.IsTruncated {
			t.Fatal("expected IsTruncated = true")
		}
		if !strings.Contains(rep.CommentMarkdown, "[Job Artifacts]") {
			t.Fatal("artifact note expected")
		}
		if !strings.Contains(rep.CommentMarkdown, "<summary><b>packages.x86_64-linux.default</b> (unchanged)</summary>") {
			t.Fatal("unchanged target summary expected")
		}
	})
}
