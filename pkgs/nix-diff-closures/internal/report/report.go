package report

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Marker is used to identify comments created by this action.
const Marker = "<!-- nix-diff-closures-report -->"

// DefaultMaxSize is the maximum character limit before falling back to artifacts.
// GitHub comment limit is 65536 bytes. We keep a safety margin for markdown formatting.
const DefaultMaxSize = 45000

// DefaultHeadSize is the target size for truncated preview text.
const DefaultHeadSize = 40000

// TargetResult contains the diff status for a single target derivation.
type TargetResult struct {
	Target   string
	BaseDrv  string
	HeadDrv  string
	DiffText string
	NoChange bool
	Error    string
}

// Report holds the formatted output and diff metadata.
type Report struct {
	CommentMarkdown string
	FullDiffText    string
	HasDiff         bool
	IsTruncated     bool
}

// Build generates the comment markdown and full diff text from target results.
func Build(results []TargetResult, maxSize int, headSize int, serverURL string, repo string, runID string) Report {
	if maxSize <= 0 {
		maxSize = DefaultMaxSize
	}
	if headSize <= 0 {
		headSize = DefaultHeadSize
	}

	var fullDiffBuilder strings.Builder
	hasAnyDiff := false

	for _, res := range results {
		if res.DiffText != "" {
			hasAnyDiff = true
			fullDiffBuilder.WriteString(fmt.Sprintf("=== Target: %s ===\n", res.Target))
			fullDiffBuilder.WriteString(res.DiffText)
			if !strings.HasSuffix(res.DiffText, "\n") {
				fullDiffBuilder.WriteString("\n")
			}
			fullDiffBuilder.WriteString("\n")
		}
	}

	fullDiff := fullDiffBuilder.String()
	if !hasAnyDiff {
		return Report{
			CommentMarkdown: "",
			FullDiffText:    "",
			HasDiff:         false,
			IsTruncated:     false,
		}
	}

	isTruncated := len(fullDiff) > maxSize

	var md strings.Builder
	md.WriteString("### ❄️ Nix Store Diff (flake.lock update)\n")
	md.WriteString(Marker + "\n\n")

	if isTruncated {
		artifactURL := fmt.Sprintf("%s/%s/actions/runs/%s#artifacts", serverURL, repo, runID)
		md.WriteString("> [!NOTE]\n")
		md.WriteString(fmt.Sprintf("> The diff output is large (%d bytes) and has been truncated to fit GitHub comment size limits.\n", len(fullDiff)))
		if serverURL != "" && repo != "" && runID != "" {
			md.WriteString(fmt.Sprintf("> You can view the full diff in the [Job Artifacts](%s).\n", artifactURL))
		} else {
			md.WriteString("> You can view the full diff in the Job Artifacts.\n")
		}
		md.WriteString("\n")
	}

	// Format each target
	for _, res := range results {
		if res.Error != "" {
			md.WriteString(fmt.Sprintf("<details open>\n<summary><b>%s</b> (error)</summary>\n\n", res.Target))
			md.WriteString(fmt.Sprintf("Error: %s\n", res.Error))
			md.WriteString("</details>\n\n")
			continue
		}

		if res.NoChange || res.DiffText == "" {
			md.WriteString(fmt.Sprintf("<details>\n<summary><b>%s</b> (unchanged)</summary>\n\n", res.Target))
			md.WriteString("No closure differences found.\n")
			md.WriteString("</details>\n\n")
			continue
		}

		md.WriteString(fmt.Sprintf("<details open>\n<summary><b>%s</b> (changed)</summary>\n\n", res.Target))
		md.WriteString("```text\n")

		displayText := res.DiffText
		if isTruncated {
			displayText = TruncateUTF8Safe(res.DiffText, headSize/len(results))
		}

		md.WriteString(strings.TrimRight(displayText, "\n"))
		md.WriteString("\n```\n")

		if isTruncated && len(displayText) < len(res.DiffText) {
			md.WriteString("... (truncated)\n")
		}

		md.WriteString("</details>\n\n")
	}

	return Report{
		CommentMarkdown: md.String(),
		FullDiffText:    fullDiff,
		HasDiff:         true,
		IsTruncated:     isTruncated,
	}
}

// TruncateUTF8Safe cuts text safely at rune boundaries and prefers line boundaries.
func TruncateUTF8Safe(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}

	if maxBytes <= 0 {
		return ""
	}

	// First pass: cut safely by valid UTF-8 runes up to maxBytes
	byteCount := 0
	lastRuneEnd := 0
	lastNewlineEnd := -1

	for byteCount < len(s) {
		r, size := utf8.DecodeRuneInString(s[byteCount:])
		if byteCount+size > maxBytes {
			break
		}
		byteCount += size
		lastRuneEnd = byteCount
		if r == '\n' {
			lastNewlineEnd = byteCount
		}
	}

	// Prefer ending at a newline to avoid tearing code lines apart
	if lastNewlineEnd > 0 && lastNewlineEnd >= lastRuneEnd/2 {
		return s[:lastNewlineEnd]
	}

	return s[:lastRuneEnd]
}
