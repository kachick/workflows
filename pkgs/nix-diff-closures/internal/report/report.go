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

// GitHubCommentLimit is the maximum character limit allowed by GitHub IssueComment API.
const GitHubCommentLimit = 65536

// SafeCommentLimit is the hard ceiling for comment length before posting to GitHub.
const SafeCommentLimit = 60000

// MaxVulnsPerPackage is the maximum number of CVE links displayed per package in PR comments.
// Remaining CVEs are summarized with a link to job artifacts (vulnerabilities.txt).
const MaxVulnsPerPackage = 5

// MaxTargetErrorSize is the maximum byte size for target error output in PR comments.
const MaxTargetErrorSize = 2000

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
	targetHeadSize := headSize / len(results)

	// Attempt 1: Standard layout with per-package CVE link limits
	commentMarkdown := buildCommentMarkdown(results, vulns, isTruncated, len(fullDiff), targetHeadSize, meta, headerText, false)

	// If markdown exceeds maxSize, degrade gracefully by summarizing vulnerabilities without individual links
	if utf8.RuneCountInString(commentMarkdown) > maxSize || len(commentMarkdown) > maxSize {
		isTruncated = true
		commentMarkdown = buildCommentMarkdown(results, vulns, isTruncated, len(fullDiff), targetHeadSize, meta, headerText, true)
	}

	// If still exceeding the safe ceiling, halve target preview sizes
	if utf8.RuneCountInString(commentMarkdown) > SafeCommentLimit || len(commentMarkdown) > SafeCommentLimit {
		targetHeadSize = targetHeadSize / 2
		commentMarkdown = buildCommentMarkdown(results, vulns, isTruncated, len(fullDiff), targetHeadSize, meta, headerText, true)
	}

	// Final hard check to guarantee safety against GitHub API 422 errors
	commentMarkdown = EnsureSafeCommentLength(commentMarkdown, meta)

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

func formatVulnItems(pvs []osv.PackageVulnerabilities, maxPerPkg int, summarizeAll bool) string {
	var sb strings.Builder
	for _, pv := range pvs {
		if summarizeAll {
			sb.WriteString(fmt.Sprintf("> - **%s** (`%s`): %d known vulnerabilities (see `vulnerabilities.txt` artifact)\n",
				html.EscapeString(pv.Package.Name), html.EscapeString(pv.Package.Version), len(pv.Vulnerabilities)))
			continue
		}

		total := len(pv.Vulnerabilities)
		limit := total
		if maxPerPkg > 0 && limit > maxPerPkg {
			limit = maxPerPkg
		}

		var ids []string
		for _, v := range pv.Vulnerabilities[:limit] {
			escapedID := html.EscapeString(v.ID)
			ids = append(ids, fmt.Sprintf("[%s](https://osv.dev/vulnerability/%s)", escapedID, url.PathEscape(v.ID)))
		}

		line := strings.Join(ids, ", ")
		if limit < total {
			line += fmt.Sprintf(", and %d more (see `vulnerabilities.txt` artifact)", total-limit)
		}
		sb.WriteString(fmt.Sprintf("> - **%s** (`%s`): %s\n", html.EscapeString(pv.Package.Name), html.EscapeString(pv.Package.Version), line))
	}
	return sb.String()
}

func buildCommentMarkdown(results []TargetResult, vulns VulnerabilityResult, isTruncated bool, fullDiffLen int, targetHeadSize int, meta workflowmeta.Metadata, headerText string, summarizeVulns bool) string {
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
		md.WriteString(formatVulnItems(vulns.NewVulns, MaxVulnsPerPackage, summarizeVulns))
		md.WriteString(">\n> *Note: This check fails intentionally when new vulnerabilities are detected, preventing automated merges (automerge) from introducing them without human review.*\n\n")
	}

	if len(vulns.ResolvedVulns) > 0 {
		md.WriteString("> [!TIP]\n")
		md.WriteString("> **Resolved Vulnerabilities in Updated Closure**\n>\n")
		md.WriteString("> The following vulnerabilities were fixed or removed by this update:\n")
		md.WriteString(formatVulnItems(vulns.ResolvedVulns, MaxVulnsPerPackage, summarizeVulns))
		md.WriteString("\n")
	}

	artifactURL := fmt.Sprintf("%s#artifacts", meta.RunURL())
	md.WriteString("> [!NOTE]\n")
	if vulns.Checked && len(vulns.NewVulns) == 0 {
		md.WriteString("> **Vulnerability Scan**: No newly introduced vulnerabilities detected in updated closure.\n")
	}
	if isTruncated {
		md.WriteString(">\n")
		md.WriteString(fmt.Sprintf("> The diff output is large (%d bytes) and has been truncated to fit GitHub comment size limits.\n", fullDiffLen))
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
			errText := res.Error
			if len(errText) > MaxTargetErrorSize {
				errText = TruncateUTF8Safe(errText, MaxTargetErrorSize) + "\n... (truncated error, see CI log)"
			}
			fence := codeFenceFor(errText)
			md.WriteString(fmt.Sprintf("<details open>\n<summary><b>%s</b> (error)</summary>\n\n", escapedTarget))
			md.WriteString(fmt.Sprintf("%stext\nError: %s\n%s\n", fence, strings.TrimRight(errText, "\n"), fence))
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
			displayText = TruncateUTF8Safe(res.DiffText, targetHeadSize)
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

	return md.String()
}

// EnsureSafeCommentLength guarantees that comment markdown stays within GitHub API limits.
//
// Why not rely only on report.Build:
// Callers or unexpected inputs could still produce strings over 65536 characters.
// Enforcing this guardrail right before GitHub API calls prevents 422 Unprocessable Entity errors.
func EnsureSafeCommentLength(comment string, meta workflowmeta.Metadata) string {
	if utf8.RuneCountInString(comment) <= SafeCommentLimit && len(comment) <= SafeCommentLimit {
		return comment
	}

	var sb strings.Builder
	sb.WriteString("### ❄️ Nix Store Diff (flake.lock update)\n")
	sb.WriteString(Marker + "\n\n")
	if meta.IsComplete() {
		sb.WriteString(meta.ProvenanceText("This report was generated automatically") + "\n\n")
	}

	artifactURL := fmt.Sprintf("%s#artifacts", meta.RunURL())
	sb.WriteString("> [!WARNING]\n")
	sb.WriteString("> **Comment was truncated because it exceeded GitHub size limits.**\n>\n")
	if meta.ServerURL != "" && meta.Repository != "" && meta.RunID != "" {
		sb.WriteString(fmt.Sprintf("> Full diff, dependency trees, and vulnerability reports are available in the [Job Artifacts](%s).\n\n", artifactURL))
	} else {
		sb.WriteString("> Full diff, dependency trees, and vulnerability reports are available in the Job Artifacts.\n\n")
	}

	headerLen := utf8.RuneCountInString(sb.String())
	remainingLimit := SafeCommentLimit - headerLen - 200
	if remainingLimit > 1000 {
		preview := TruncateUTF8Safe(comment, remainingLimit)
		fence := codeFenceFor(preview)
		sb.WriteString("<details open>\n<summary><b>Truncated Preview</b></summary>\n\n")
		sb.WriteString(fence + "text\n")
		sb.WriteString(strings.TrimRight(preview, "\n"))
		sb.WriteString("\n" + fence + "\n")
		sb.WriteString("... (truncated)\n")
		sb.WriteString("</details>\n")
	}

	return sb.String()
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
