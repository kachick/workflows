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
			// Stop scanning when the next section begins.
			if strings.HasPrefix(line, "## ") {
				break
			}

			// Table rows start with '|'
			if strings.HasPrefix(line, "|") {
				// Skip table delimiter row (e.g. "| :--- | --- |")
				if strings.Contains(line, "---") {
					inTable = true
					continue
				}

				// Skip header row before delimiter
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
