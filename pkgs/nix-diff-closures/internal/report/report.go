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
	Target         string
	BaseDrv        string
	HeadDrv        string
	DiffText       string
	TreeText       string
	WhyDependsText string
	NoChange       bool
	Error          string
}

// VulnerabilityResult contains newly introduced, resolved, and pre-existing vulnerabilities detected by OSV.
type VulnerabilityResult struct {
	Checked       bool
	NewVulns      []osv.PackageVulnerabilities
	ResolvedVulns []osv.PackageVulnerabilities
	ExistingVulns []osv.PackageVulnerabilities
}

// Report holds the formatted output and diff metadata.
type Report struct {
	CommentMarkdown         string
	StepSummaryMarkdown     string
	FullDiffText            string
	FullTreeText            string
	FullWhyDependsText      string
	FullVulnerabilitiesText string
	HasDiff                 bool
	IsTruncated             bool
	Vulnerabilities         []osv.PackageVulnerabilities // newly introduced (for backward compatibility)
	ResolvedVulnerabilities []osv.PackageVulnerabilities
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
	var fullTreeBuilder strings.Builder
	var fullWhyDependsBuilder strings.Builder
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
		if res.TreeText != "" {
			fullTreeBuilder.WriteString(fmt.Sprintf("=== Target: %s ===\n", res.Target))
			fullTreeBuilder.WriteString(res.TreeText)
			if !strings.HasSuffix(res.TreeText, "\n") {
				fullTreeBuilder.WriteString("\n")
			}
			fullTreeBuilder.WriteString("\n")
		}
		if res.WhyDependsText != "" {
			fullWhyDependsBuilder.WriteString(fmt.Sprintf("=== Target: %s ===\n", res.Target))
			fullWhyDependsBuilder.WriteString(res.WhyDependsText)
			if !strings.HasSuffix(res.WhyDependsText, "\n") {
				fullWhyDependsBuilder.WriteString("\n")
			}
			fullWhyDependsBuilder.WriteString("\n")
		}
	}

	fullDiff := fullDiffBuilder.String()
	fullTree := fullTreeBuilder.String()
	fullWhyDepends := fullWhyDependsBuilder.String()
	if !hasAnyDiff {
		return Report{
			CommentMarkdown:     "",
			StepSummaryMarkdown: "",
			FullDiffText:        "",
			FullTreeText:        "",
			FullWhyDependsText:  "",
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

	if len(vulns.ResolvedVulns) > 0 {
		md.WriteString("> [!TIP]\n")
		md.WriteString("> **Resolved Vulnerabilities in Updated Closure**\n>\n")
		md.WriteString("> The following vulnerabilities were fixed or removed by this update:\n")
		for _, pv := range vulns.ResolvedVulns {
			var ids []string
			for _, v := range pv.Vulnerabilities {
				escapedID := html.EscapeString(v.ID)
				ids = append(ids, fmt.Sprintf("[%s](https://osv.dev/vulnerability/%s)", escapedID, url.PathEscape(v.ID)))
			}
			md.WriteString(fmt.Sprintf("> - **%s** (`%s`): %s\n", html.EscapeString(pv.Package.Name), html.EscapeString(pv.Package.Version), strings.Join(ids, ", ")))
		}
		md.WriteString("\n")
	}

	artifactURL := fmt.Sprintf("%s#artifacts", meta.RunURL())
	md.WriteString("> [!NOTE]\n")
	if vulns.Checked && len(vulns.NewVulns) == 0 {
		md.WriteString("> **Vulnerability Scan**: No newly introduced vulnerabilities detected in updated closure.\n")
	}
	if isTruncated {
		md.WriteString(">\n")
		md.WriteString(fmt.Sprintf("> The diff output is large (%d bytes) and has been truncated to fit GitHub comment size limits.\n", len(fullDiff)))
	}
	md.WriteString(">\n")
	if meta.ServerURL != "" && meta.Repository != "" && meta.RunID != "" {
		md.WriteString(fmt.Sprintf("> Detailed full diff, dependency trees (`tree.txt`, `why-depends.txt`), and vulnerability details (`vulnerabilities.txt`) are available in the [Job Artifacts](%s).\n", artifactURL))
	} else {
		md.WriteString("> Detailed full diff, dependency trees (`tree.txt`, `why-depends.txt`), and vulnerability details (`vulnerabilities.txt`) are available in the Job Artifacts.\n")
	}
	md.WriteString("\n")

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
		summary.WriteString(fmt.Sprintf("<summary><b>Existing vulnerabilities summary (%d package(s) still affected)</b></summary>\n\n", len(vulns.ExistingVulns)))
		summary.WriteString(fmt.Sprintf("The updated closure still contains known vulnerabilities that were already present before this update (%d package(s) affected). Detailed CVE lists are available in the Job Artifacts (`vulnerabilities.txt`).\n", len(vulns.ExistingVulns)))
		summary.WriteString("\n</details>\n\n")
	}

	return Report{
		CommentMarkdown:         commentMarkdown,
		StepSummaryMarkdown:     summary.String(),
		FullDiffText:            fullDiff,
		FullTreeText:            fullTree,
		FullWhyDependsText:      fullWhyDepends,
		FullVulnerabilitiesText: FormatVulnerabilitiesText(vulns),
		HasDiff:                 true,
		IsTruncated:             isTruncated,
		Vulnerabilities:         vulns.NewVulns,
		ResolvedVulnerabilities: vulns.ResolvedVulns,
		ExistingVulnerabilities: vulns.ExistingVulns,
	}, nil
}

// FormatVulnerabilitiesText formats the complete vulnerability details for job artifacts.
func FormatVulnerabilitiesText(vulns VulnerabilityResult) string {
	if !vulns.Checked {
		return "Vulnerability check was not enabled.\n"
	}

	var sb strings.Builder
	sb.WriteString("=== Vulnerability Scan Report ===\n\n")
	sb.WriteString(fmt.Sprintf("Summary:\n  - Newly introduced: %d package(s)\n  - Resolved: %d package(s)\n  - Pre-existing (remained): %d package(s)\n\n",
		len(vulns.NewVulns), len(vulns.ResolvedVulns), len(vulns.ExistingVulns)))

	if len(vulns.NewVulns) > 0 {
		sb.WriteString("=== Newly Introduced Vulnerabilities ===\n")
		for _, pv := range vulns.NewVulns {
			var ids []string
			for _, v := range pv.Vulnerabilities {
				ids = append(ids, fmt.Sprintf("%s (https://osv.dev/vulnerability/%s)", v.ID, url.PathEscape(v.ID)))
			}
			sb.WriteString(fmt.Sprintf("- %s (%s): %s\n", pv.Package.Name, pv.Package.Version, strings.Join(ids, ", ")))
		}
		sb.WriteString("\n")
	}

	if len(vulns.ResolvedVulns) > 0 {
		sb.WriteString("=== Resolved Vulnerabilities ===\n")
		for _, pv := range vulns.ResolvedVulns {
			var ids []string
			for _, v := range pv.Vulnerabilities {
				ids = append(ids, fmt.Sprintf("%s (https://osv.dev/vulnerability/%s)", v.ID, url.PathEscape(v.ID)))
			}
			sb.WriteString(fmt.Sprintf("- %s (%s): %s\n", pv.Package.Name, pv.Package.Version, strings.Join(ids, ", ")))
		}
		sb.WriteString("\n")
	}

	if len(vulns.ExistingVulns) > 0 {
		sb.WriteString("=== Pre-existing Vulnerabilities (Remained) ===\n")
		for _, pv := range vulns.ExistingVulns {
			var ids []string
			for _, v := range pv.Vulnerabilities {
				ids = append(ids, fmt.Sprintf("%s (https://osv.dev/vulnerability/%s)", v.ID, url.PathEscape(v.ID)))
			}
			sb.WriteString(fmt.Sprintf("- %s (%s): %s\n", pv.Package.Name, pv.Package.Version, strings.Join(ids, ", ")))
		}
		sb.WriteString("\n")
	}

	if len(vulns.NewVulns) == 0 && len(vulns.ResolvedVulns) == 0 && len(vulns.ExistingVulns) == 0 {
		sb.WriteString("No known vulnerabilities found in updated packages.\n")
	}

	return sb.String()
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
