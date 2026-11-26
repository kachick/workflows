package workflowlint

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	workflowFilePattern   = regexp.MustCompile(`^reusable-(.+)\.ya?ml$`)
	anyWorkflowPattern    = regexp.MustCompile(`\.ya?ml$`)
	markdownLinkPattern   = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)
	tableDelimiterPattern = regexp.MustCompile(`^\|[\s:|-]+\|$`)
	usesNixInstallerLine  = regexp.MustCompile(`^(?:-\s+)?uses:\s+NixOS/nix-installer-action@`)
)

var (
	ErrWorkflowDirNotFound = errors.New("workflow directory not found")
	ErrReadmeNotFound      = errors.New("README.md not found")
	ErrSectionNotFound     = errors.New("Available Workflows section not found in documentation")
)

// ExtractWorkflowName returns the workflow identifier from a filename.
// For example, "reusable-automerge.yml" returns ("automerge", true).
// Non-reusable workflow filenames return ("", false).
func ExtractWorkflowName(filename string) (string, bool) {
	matches := workflowFilePattern.FindStringSubmatch(filename)
	if len(matches) < 2 {
		return "", false
	}
	return matches[1], true
}

// FindReusableWorkflows scans workflowDir and returns sorted names of reusable workflows.
func FindReusableWorkflows(workflowDir string) ([]string, error) {
	entries, err := os.ReadDir(workflowDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrWorkflowDirNotFound, workflowDir)
		}
		return nil, fmt.Errorf("read workflow directory: %w", err)
	}

	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if name, ok := ExtractWorkflowName(entry.Name()); ok {
			names = append(names, name)
		}
	}

	sort.Strings(names)
	return names, nil
}

// ParseDocumentedWorkflows extracts workflow names listed in the Available Workflows table.
func ParseDocumentedWorkflows(readmeContent string) (map[string]bool, error) {
	scanner := bufio.NewScanner(strings.NewReader(readmeContent))
	inSection := false
	inTable := false
	documented := make(map[string]bool)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if strings.HasPrefix(line, "## Available Workflows") {
			inSection = true
			continue
		}

		if inSection {
			if strings.HasPrefix(line, "## ") {
				break
			}

			if !strings.HasPrefix(line, "|") {
				inTable = false
				continue
			}

			if tableDelimiterPattern.MatchString(line) && strings.Contains(line, "-") {
				inTable = true
				continue
			}

			if !inTable {
				continue
			}

			parts := strings.Split(line, "|")
			// A valid table row "| col1 | col2 |" splits into ["", " col1 ", " col2 ", ""]
			if len(parts) >= 3 {
				firstCol := strings.TrimSpace(parts[1])
				name := extractNameFromCell(firstCol)
				if name != "" {
					documented[name] = true
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan README content: %w", err)
	}

	if !inSection {
		return nil, ErrSectionNotFound
	}

	return documented, nil
}

func extractNameFromCell(cell string) string {
	matches := markdownLinkPattern.FindStringSubmatch(cell)
	if len(matches) >= 2 {
		return strings.TrimSpace(matches[1])
	}
	// Fall back to stripped plain text (e.g. `foo` -> foo)
	return strings.Trim(cell, "` ")
}

// FindMissingWorkflows compares existing reusable workflows with documented ones.
func FindMissingWorkflows(existing []string, documented map[string]bool) []string {
	var missing []string
	for _, name := range existing {
		if !documented[name] {
			missing = append(missing, name)
		}
	}
	return missing
}

// FindStaleWorkflows returns documented workflow names that no longer have a reusable workflow file.
func FindStaleWorkflows(existing []string, documented map[string]bool) []string {
	existingSet := make(map[string]bool, len(existing))
	for _, name := range existing {
		existingSet[name] = true
	}
	var stale []string
	for name := range documented {
		if !existingSet[name] {
			stale = append(stale, name)
		}
	}
	sort.Strings(stale)
	return stale
}

// Check validates that reusable workflows in repoRoot match docs/onboarding.md (or README.md).
func Check(repoRoot string) (missing []string, stale []string, err error) {
	workflowDir := filepath.Join(repoRoot, ".github", "workflows")
	docPath := filepath.Join(repoRoot, "docs", "onboarding.md")
	if _, statErr := os.Stat(docPath); os.IsNotExist(statErr) {
		docPath = filepath.Join(repoRoot, "README.md")
	}

	existing, err := FindReusableWorkflows(workflowDir)
	if err != nil {
		return nil, nil, err
	}

	docBytes, err := os.ReadFile(docPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("%w: %s", ErrReadmeNotFound, docPath)
		}
		return nil, nil, fmt.Errorf("read documentation: %w", err)
	}

	documented, err := ParseDocumentedWorkflows(string(docBytes))
	if err != nil {
		return nil, nil, err
	}

	return FindMissingWorkflows(existing, documented), FindStaleWorkflows(existing, documented), nil
}

// FindReusableWorkflowFiles scans workflowDir and returns sorted filenames of reusable workflows.
func FindReusableWorkflowFiles(workflowDir string) ([]string, error) {
	entries, err := os.ReadDir(workflowDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrWorkflowDirNotFound, workflowDir)
		}
		return nil, fmt.Errorf("read workflow directory: %w", err)
	}

	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if _, ok := ExtractWorkflowName(entry.Name()); ok {
			files = append(files, entry.Name())
		}
	}

	sort.Strings(files)
	return files, nil
}

func findAllWorkflowFiles(workflowDir string) ([]string, error) {
	entries, err := os.ReadDir(workflowDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrWorkflowDirNotFound, workflowDir)
		}
		return nil, fmt.Errorf("read workflow directory: %w", err)
	}

	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if anyWorkflowPattern.MatchString(entry.Name()) {
			files = append(files, entry.Name())
		}
	}

	sort.Strings(files)
	return files, nil
}

// ExtractNixInstallerSteps extracts all normalized step definitions of NixOS/nix-installer-action from workflow content.
func ExtractNixInstallerSteps(workflowContent string) ([]string, error) {
	scanner := bufio.NewScanner(strings.NewReader(workflowContent))
	var currentStepLines []string
	currentStepIndent := -1
	var results []string

	flush := func() {
		if len(currentStepLines) == 0 {
			return
		}
		isTarget := false
		for _, l := range currentStepLines {
			if usesNixInstallerLine.MatchString(strings.TrimSpace(l)) {
				isTarget = true
				break
			}
		}
		if !isTarget {
			return
		}

		normalized := make([]string, 0, len(currentStepLines))
		for _, l := range currentStepLines {
			if len(l) >= currentStepIndent {
				normalized = append(normalized, l[currentStepIndent:])
			} else {
				normalized = append(normalized, strings.TrimSpace(l))
			}
		}
		results = append(results, strings.TrimRight(strings.Join(normalized, "\n"), "\r\n "))
	}

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimLeft(line, " ")
		indent := len(line) - len(trimmed)

		// A step in GHA YAML starts with '- '. Nested '- ' lines inside a step (e.g. scripts or lists)
		// have greater indentation and belong to the current step without changing currentStepIndent.
		if strings.HasPrefix(trimmed, "- ") {
			if currentStepIndent >= 0 {
				if indent > currentStepIndent {
					currentStepLines = append(currentStepLines, line)
					continue
				}
				flush()
				currentStepLines = nil
			}
			currentStepIndent = indent
			currentStepLines = append(currentStepLines, line)
			continue
		}

		if currentStepIndent >= 0 {
			if strings.TrimSpace(line) != "" && indent < currentStepIndent {
				flush()
				currentStepLines = nil
				currentStepIndent = -1
				continue
			}
			currentStepLines = append(currentStepLines, line)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan workflow content: %w", err)
	}

	flush()
	return results, nil
}

// ExtractNixInstallerStep extracts the first normalized step definition of NixOS/nix-installer-action.
func ExtractNixInstallerStep(workflowContent string) (string, bool) {
	steps, err := ExtractNixInstallerSteps(workflowContent)
	if err != nil || len(steps) == 0 {
		return "", false
	}
	return steps[0], true
}

// CheckNixInstallerSteps verifies that all workflows using NixOS/nix-installer-action
// share the identical step definition (version, inputs, extra-conf, etc.).
func CheckNixInstallerSteps(repoRoot string) error {
	workflowDir := filepath.Join(repoRoot, ".github", "workflows")
	files, err := findAllWorkflowFiles(workflowDir)
	if err != nil {
		return err
	}

	type stepRecord struct {
		filename string
		step     string
	}
	var steps []stepRecord

	for _, file := range files {
		path := filepath.Join(workflowDir, file)
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", file, err)
		}

		found, err := ExtractNixInstallerSteps(string(content))
		if err != nil {
			return fmt.Errorf("extract nix-installer steps from %s: %w", file, err)
		}
		for _, step := range found {
			steps = append(steps, stepRecord{
				filename: file,
				step:     step,
			})
		}
	}

	if len(steps) <= 1 {
		return nil
	}

	base := steps[0]
	for _, current := range steps[1:] {
		if current.step != base.step {
			return fmt.Errorf("nix-installer action step mismatch between %s and %s:\n--- %s\n%s\n--- %s\n%s",
				base.filename, current.filename,
				base.filename, base.step,
				current.filename, current.step)
		}
	}

	return nil
}

type jobGuardRecord struct {
	name       string
	hasOwnerIf bool
	hasTimeout bool
}

// stripYAMLInlineComment removes a trailing YAML comment (" #...") outside single/double quotes.
func stripYAMLInlineComment(s string) string {
	inSingle := false
	inDouble := false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble && (i == 0 || s[i-1] == ' ' || s[i-1] == '\t') {
				return strings.TrimSpace(s[:i])
			}
		}
	}
	return strings.TrimSpace(s)
}

// isStrictOwnerGuard checks that the `if:` expression requires `github.repository_owner == 'kachick'`
// without allowing top-level `||` bypasses.
func isStrictOwnerGuard(ifExpr string) bool {
	expr := strings.TrimSpace(ifExpr)
	expr = strings.TrimPrefix(expr, "${{")
	expr = strings.TrimSuffix(expr, "}}")
	expr = strings.TrimSpace(expr)
	if strings.Contains(expr, "||") {
		return false
	}
	for _, clause := range strings.Split(expr, "&&") {
		c := strings.TrimSpace(clause)
		c = strings.Trim(c, "()")
		c = strings.TrimSpace(c)
		if c == "github.repository_owner == 'kachick'" {
			return true
		}
	}
	return false
}

func parseReusableJobGuards(workflowContent string) ([]jobGuardRecord, error) {
	scanner := bufio.NewScanner(strings.NewReader(workflowContent))
	inJobs := false
	jobIndent := -1
	propIndent := -1
	var jobs []jobGuardRecord
	var current *jobGuardRecord

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		noComment := stripYAMLInlineComment(trimmed)
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if indent == 0 {
			if noComment == "jobs:" {
				inJobs = true
				continue
			}
			if inJobs {
				break
			}
			continue
		}

		if !inJobs {
			continue
		}

		if jobIndent < 0 {
			jobIndent = indent
		}

		if indent == jobIndent && strings.HasSuffix(noComment, ":") {
			if current != nil {
				jobs = append(jobs, *current)
			}
			current = &jobGuardRecord{
				name: strings.TrimSuffix(noComment, ":"),
			}
			propIndent = -1
			continue
		}

		if current == nil || indent <= jobIndent {
			continue
		}

		if propIndent < 0 {
			propIndent = indent
		}

		if indent == propIndent {
			if val, ok := strings.CutPrefix(noComment, "if:"); ok {
				if isStrictOwnerGuard(val) {
					current.hasOwnerIf = true
				}
			}
			if val, ok := strings.CutPrefix(noComment, "timeout-minutes:"); ok {
				if n, err := strconv.Atoi(strings.TrimSpace(val)); err == nil && n > 0 {
					current.hasTimeout = true
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan workflow content: %w", err)
	}

	if current != nil {
		jobs = append(jobs, *current)
	}

	return jobs, nil
}

// CheckReusableJobGuards verifies that every job in reusable-*.yml defines both
// `if: github.repository_owner == 'kachick'` and a positive integer `timeout-minutes`.
func CheckReusableJobGuards(repoRoot string) error {
	workflowDir := filepath.Join(repoRoot, ".github", "workflows")
	files, err := FindReusableWorkflowFiles(workflowDir)
	if err != nil {
		return err
	}

	for _, file := range files {
		path := filepath.Join(workflowDir, file)
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", file, err)
		}

		jobs, err := parseReusableJobGuards(string(content))
		if err != nil {
			return fmt.Errorf("parse %s: %w", file, err)
		}
		if len(jobs) == 0 {
			return fmt.Errorf("%s: no jobs found", file)
		}

		for _, job := range jobs {
			if !job.hasOwnerIf {
				return fmt.Errorf("%s: job %q is missing `if: github.repository_owner == 'kachick'` guard", file, job.name)
			}
			if !job.hasTimeout {
				return fmt.Errorf("%s: job %q is missing positive `timeout-minutes`", file, job.name)
			}
		}
	}

	return nil
}
