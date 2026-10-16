package workflowlint

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	workflowFilePattern = regexp.MustCompile(`^reusable-(.+)\.ya?ml$`)
	markdownLinkPattern = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)
)

var (
	ErrWorkflowDirNotFound = errors.New("workflow directory not found")
	ErrReadmeNotFound      = errors.New("README.md not found")
	ErrSectionNotFound     = errors.New("Available Workflows section not found in README.md")
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

			if strings.HasPrefix(line, "|") {
				// Skip table delimiter row (e.g. "| :--- | --- |")
				if strings.Contains(line, "---") {
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
	trimmed := strings.Trim(cell, "` ")
	return trimmed
}

// FindMissingWorkflows compares existing reusable workflows with documented ones in README.md.
// Returns a slice of workflow names that exist as files but are missing from the documentation.
func FindMissingWorkflows(existing []string, documented map[string]bool) []string {
	var missing []string
	for _, name := range existing {
		if !documented[name] {
			missing = append(missing, name)
		}
	}
	return missing
}

// Check validates that all reusable workflows in repoRoot are documented in README.md.
func Check(repoRoot string) ([]string, error) {
	workflowDir := filepath.Join(repoRoot, ".github", "workflows")
	readmePath := filepath.Join(repoRoot, "README.md")

	existing, err := FindReusableWorkflows(workflowDir)
	if err != nil {
		return nil, err
	}

	readmeBytes, err := os.ReadFile(readmePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrReadmeNotFound, readmePath)
		}
		return nil, fmt.Errorf("read README: %w", err)
	}

	documented, err := ParseDocumentedWorkflows(string(readmeBytes))
	if err != nil {
		return nil, err
	}

	return FindMissingWorkflows(existing, documented), nil
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

// ExtractNixInstallerStep extracts the normalized step definition of NixOS/nix-installer-action from workflow content.
func ExtractNixInstallerStep(workflowContent string) (string, bool) {
	scanner := bufio.NewScanner(strings.NewReader(workflowContent))
	var currentStepLines []string
	currentStepIndent := -1

	flush := func() (string, bool) {
		if len(currentStepLines) == 0 {
			return "", false
		}
		isTarget := false
		for _, l := range currentStepLines {
			if strings.Contains(l, "NixOS/nix-installer-action") {
				isTarget = true
				break
			}
		}
		if !isTarget {
			return "", false
		}

		var normalized []string
		for _, l := range currentStepLines {
			if len(l) >= currentStepIndent {
				normalized = append(normalized, l[currentStepIndent:])
			} else {
				normalized = append(normalized, strings.TrimSpace(l))
			}
		}
		result := strings.TrimRight(strings.Join(normalized, "\n"), "\r\n ")
		return result, true
	}

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimLeft(line, " ")
		indent := len(line) - len(trimmed)

		// A step in GHA YAML starts with '- '
		if strings.HasPrefix(trimmed, "- ") {
			if currentStepIndent >= 0 && indent <= currentStepIndent {
				if step, ok := flush(); ok {
					return step, true
				}
				currentStepLines = nil
			}
			currentStepIndent = indent
			currentStepLines = append(currentStepLines, line)
			continue
		}

		if currentStepIndent >= 0 {
			// If non-empty line has less or equal indent than the step item, step has ended
			if strings.TrimSpace(line) != "" && indent < currentStepIndent {
				if step, ok := flush(); ok {
					return step, true
				}
				currentStepLines = nil
				currentStepIndent = -1
				continue
			}
			currentStepLines = append(currentStepLines, line)
		}
	}

	if step, ok := flush(); ok {
		return step, true
	}

	return "", false
}

// CheckNixInstallerSteps verifies that all workflows using NixOS/nix-installer-action
// share the identical step definition (version, inputs, extra-conf, etc.).
func CheckNixInstallerSteps(repoRoot string) error {
	workflowDir := filepath.Join(repoRoot, ".github", "workflows")
	files, err := FindReusableWorkflowFiles(workflowDir)
	if err != nil {
		return err
	}

	ciNixPath := filepath.Join(workflowDir, "ci-nix.yml")
	if _, err := os.Stat(ciNixPath); err == nil {
		files = append(files, "ci-nix.yml")
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

		if step, ok := ExtractNixInstallerStep(string(content)); ok {
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

func parseReusableJobGuards(workflowContent string) []jobGuardRecord {
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

		indent := len(line) - len(strings.TrimLeft(line, " "))
		if indent == 0 {
			if strings.HasPrefix(trimmed, "jobs:") {
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

		if indent == jobIndent && strings.HasSuffix(trimmed, ":") {
			if current != nil {
				jobs = append(jobs, *current)
			}
			current = &jobGuardRecord{
				name: strings.TrimSuffix(trimmed, ":"),
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
			if strings.HasPrefix(trimmed, "if:") && strings.Contains(trimmed, "github.repository_owner == 'kachick'") {
				current.hasOwnerIf = true
			}
			if strings.HasPrefix(trimmed, "timeout-minutes:") {
				current.hasTimeout = true
			}
		}
	}

	if current != nil {
		jobs = append(jobs, *current)
	}

	return jobs
}

// CheckReusableJobGuards verifies that every job in reusable-*.yml defines both
// `if: github.repository_owner == 'kachick'` and `timeout-minutes`.
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

		jobs := parseReusableJobGuards(string(content))
		if len(jobs) == 0 {
			return fmt.Errorf("%s: no jobs found", file)
		}

		for _, job := range jobs {
			if !job.hasOwnerIf {
				return fmt.Errorf("%s: job %q is missing `if: github.repository_owner == 'kachick'` guard", file, job.name)
			}
			if !job.hasTimeout {
				return fmt.Errorf("%s: job %q is missing `timeout-minutes`", file, job.name)
			}
		}
	}

	return nil
}
