package report

import (
	"fmt"
	"html"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/kachick/workflows/internal/workflowmeta"
	"github.com/kachick/workflows/pkgs/nix-diff-closures/internal/osv"
)

// Marker is used to identify comments created by this action.
const Marker = "<!-- nix-diff-closures-report -->"

// DefaultMaxSize is the maximum byte limit before falling back to artifacts.
// GitHub comments have a 65536-byte limit; keep a safety margin for markdown formatting.
const DefaultMaxSize = 45000

// DefaultHeadSize is the target byte size for truncated preview text.
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

// VulnerabilityResult contains newly introduced and pre-existing vulnerabilities detected by OSV.
type VulnerabilityResult struct {
	Checked       bool
	NewVulns      []osv.PackageVulnerabilities
	ExistingVulns []osv.PackageVulnerabilities
}

// Report holds the formatted output and diff metadata.
type Report struct {
	CommentMarkdown         string
	StepSummaryMarkdown     string
	FullDiffText            string
	HasDiff                 bool
	IsTruncated             bool
	Vulnerabilities         []osv.PackageVulnerabilities // newly introduced (for backward compatibility)
	ExistingVulnerabilities []osv.PackageVulnerabilities
}

// Build generates the comment markdown and full diff text from target results and detected vulnerabilities.
func Build(results []TargetResult, vulns VulnerabilityResult, maxSize int, headSize int, meta workflowmeta.Metadata, isCI bool) (Report, error) {
	if maxSize <= 0 {
		maxSize = DefaultMaxSize
	}
	if headSize <= 0 {
		headSize = DefaultHeadSize
	}

	if !meta.IsComplete() && isCI {
		return Report{}, fmt.Errorf("missing required workflow metadata in CI environment: %+v", meta)
	}
	headerText := meta.ProvenanceText("This report was generated automatically")

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
			CommentMarkdown:     "",
			StepSummaryMarkdown: "",
			FullDiffText:        "",
			HasDiff:             false,
			IsTruncated:         false,
		}, nil
	}

	isTruncated := len(fullDiff) > maxSize

	var md strings.Builder
	md.WriteString("### ❄️ Nix Store Diff (flake.lock update)\n")
	md.WriteString(Marker + "\n\n")
	if headerText != "" {
		md.WriteString(headerText + "\n\n")
	}

	if len(vulns.NewVulns) > 0 {
		md.WriteString("> [!CAUTION]\n")
		md.WriteString("> **Newly Introduced Vulnerabilities Detected in Updated Closure**\n>\n")
		md.WriteString("> The following updated packages introduce new known vulnerabilities from the OSV database:\n")
		for _, pv := range vulns.NewVulns {
			var ids []string
			for _, v := range pv.Vulnerabilities {
				escapedID := html.EscapeString(v.ID)
				ids = append(ids, fmt.Sprintf("[%s](https://osv.dev/vulnerability/%s)", escapedID, url.PathEscape(v.ID)))
			}
			md.WriteString(fmt.Sprintf("> - **%s** (`%s`): %s\n", html.EscapeString(pv.Package.Name), html.EscapeString(pv.Package.Version), strings.Join(ids, ", ")))
		}
		md.WriteString(">\n> *Note: This check fails intentionally when new vulnerabilities are detected, preventing automated merges (automerge) from introducing them without human review.*\n\n")
	}

	artifactURL := fmt.Sprintf("%s#artifacts", meta.RunURL())
	if vulns.Checked && len(vulns.NewVulns) == 0 {
		md.WriteString("> [!NOTE]\n")
		md.WriteString("> **Vulnerability Scan**: No newly introduced vulnerabilities detected in updated closure.\n")
		if isTruncated {
			md.WriteString(">\n")
			md.WriteString(fmt.Sprintf("> The diff output is large (%d bytes) and has been truncated to fit GitHub comment size limits.\n", len(fullDiff)))
			if meta.ServerURL != "" && meta.Repository != "" && meta.RunID != "" {
				md.WriteString(fmt.Sprintf("> You can view the full diff in the [Job Artifacts](%s).\n", artifactURL))
			} else {
				md.WriteString("> You can view the full diff in the Job Artifacts.\n")
			}
		}
		md.WriteString("\n")
	} else if isTruncated {
		md.WriteString("> [!NOTE]\n")
		md.WriteString(fmt.Sprintf("> The diff output is large (%d bytes) and has been truncated to fit GitHub comment size limits.\n", len(fullDiff)))
		if meta.ServerURL != "" && meta.Repository != "" && meta.RunID != "" {
			md.WriteString(fmt.Sprintf("> You can view the full diff in the [Job Artifacts](%s).\n", artifactURL))
		} else {
			md.WriteString("> You can view the full diff in the Job Artifacts.\n")
		}
		md.WriteString("\n")
	}

	for _, res := range results {
		escapedTarget := html.EscapeString(res.Target)
		if res.Error != "" {
			fence := codeFenceFor(res.Error)
			md.WriteString(fmt.Sprintf("<details open>\n<summary><b>%s</b> (error)</summary>\n\n", escapedTarget))
			md.WriteString(fmt.Sprintf("%stext\nError: %s\n%s\n", fence, strings.TrimRight(res.Error, "\n"), fence))
			md.WriteString("</details>\n\n")
			continue
		}

		if res.NoChange || res.DiffText == "" {
			md.WriteString(fmt.Sprintf("<details>\n<summary><b>%s</b> (unchanged)</summary>\n\n", escapedTarget))
			md.WriteString("No closure differences found.\n")
			md.WriteString("</details>\n\n")
			continue
		}

		displayText := res.DiffText
		if isTruncated {
			displayText = TruncateUTF8Safe(res.DiffText, headSize/len(results))
		}

		fence := codeFenceFor(displayText)
		md.WriteString(fmt.Sprintf("<details open>\n<summary><b>%s</b> (changed)</summary>\n\n", escapedTarget))
		md.WriteString(fence + "text\n")
		md.WriteString(strings.TrimRight(displayText, "\n"))
		md.WriteString("\n" + fence + "\n")

		if isTruncated && len(displayText) < len(res.DiffText) {
			md.WriteString("... (truncated)\n")
		}

		md.WriteString("</details>\n\n")
	}

	commentMarkdown := md.String()
	var summary strings.Builder
	summary.WriteString(commentMarkdown)

	if len(vulns.ExistingVulns) > 0 {
		summary.WriteString("<details>\n")
		summary.WriteString("<summary><b>Existing vulnerabilities in updated packages (already present in base closure)</b></summary>\n\n")
		summary.WriteString("The following updated packages contain known vulnerabilities that were already present before this update:\n")
		for _, pv := range vulns.ExistingVulns {
			var ids []string
			for _, v := range pv.Vulnerabilities {
				escapedID := html.EscapeString(v.ID)
				ids = append(ids, fmt.Sprintf("[%s](https://osv.dev/vulnerability/%s)", escapedID, url.PathEscape(v.ID)))
			}
			summary.WriteString(fmt.Sprintf("- **%s** (`%s`): %s\n", html.EscapeString(pv.Package.Name), html.EscapeString(pv.Package.Version), strings.Join(ids, ", ")))
		}
		summary.WriteString("\n</details>\n\n")
	}

	return Report{
		CommentMarkdown:         commentMarkdown,
		StepSummaryMarkdown:     summary.String(),
		FullDiffText:            fullDiff,
		HasDiff:                 true,
		IsTruncated:             isTruncated,
		Vulnerabilities:         vulns.NewVulns,
		ExistingVulnerabilities: vulns.ExistingVulns,
	}, nil
}

func codeFenceFor(s string) string {
	fence := "```"
	for strings.Contains(s, fence) {
		fence += "`"
	}
	return fence
}

// TruncateUTF8Safe cuts text safely at rune boundaries and prefers line boundaries.
func TruncateUTF8Safe(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}

	if maxBytes <= 0 {
		return ""
	}

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
