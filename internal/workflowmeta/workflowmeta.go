package workflowmeta

import (
	"fmt"
	"os"
)

// Metadata holds information about the GitHub Actions workflow run and definition.
type Metadata struct {
	ServerURL          string
	Repository         string
	RunID              string
	WorkflowRepository string
	WorkflowSHA        string
	WorkflowFilePath   string
}

// Get reads workflow metadata from GitHub Actions default and passed environment variables.
func Get() Metadata {
	serverURL := os.Getenv("GITHUB_SERVER_URL")
	if serverURL == "" {
		serverURL = "https://github.com"
	}
	return Metadata{
		ServerURL:          serverURL,
		Repository:         os.Getenv("GITHUB_REPOSITORY"),
		RunID:              os.Getenv("GITHUB_RUN_ID"),
		WorkflowRepository: os.Getenv("WORKFLOW_REPOSITORY"),
		WorkflowSHA:        os.Getenv("WORKFLOW_SHA"),
		WorkflowFilePath:   os.Getenv("WORKFLOW_FILE_PATH"),
	}
}

// IsComplete checks whether all required fields for workflow provenance are present.
func (m Metadata) IsComplete() bool {
	return m.ServerURL != "" &&
		m.Repository != "" &&
		m.RunID != "" &&
		m.WorkflowRepository != "" &&
		m.WorkflowSHA != "" &&
		m.WorkflowFilePath != ""
}

// RunURL returns the link to the GitHub Actions run.
func (m Metadata) RunURL() string {
	return fmt.Sprintf("%s/%s/actions/runs/%s", m.ServerURL, m.Repository, m.RunID)
}

// WorkflowPermalink returns the link to the workflow definition file at the exact commit SHA.
func (m Metadata) WorkflowPermalink() string {
	return fmt.Sprintf("%s/%s/blob/%s/%s", m.ServerURL, m.WorkflowRepository, m.WorkflowSHA, m.WorkflowFilePath)
}

// BlobPermalink returns a GitHub permalink to the specified repository-relative file path.
// When workflow metadata is incomplete (such as in local runs), it falls back to the default repo and main branch.
func (m Metadata) BlobPermalink(relativePath string) string {
	serverURL := m.ServerURL
	if serverURL == "" {
		serverURL = "https://github.com"
	}
	repo := m.WorkflowRepository
	if repo == "" {
		repo = "kachick/workflows"
	}
	rev := m.WorkflowSHA
	if rev == "" {
		rev = "main"
	}
	return fmt.Sprintf("%s/%s/blob/%s/%s", serverURL, repo, rev, relativePath)
}

// ProvenanceText formats a Markdown sentence with run and definition links if metadata is complete.
// If metadata is incomplete, it returns a plain sentence without links.
func (m Metadata) ProvenanceText(action string) string {
	if m.IsComplete() {
		return fmt.Sprintf("%s by [workflow run](%s) (defined at [%s](%s)).",
			action, m.RunURL(), m.WorkflowRepository, m.WorkflowPermalink())
	}
	return fmt.Sprintf("%s.", action)
}

// ExpectedOwner is the only GitHub owner supported by these personal workflows.
const ExpectedOwner = "kachick"

// VerifyEnv checks that the GitHub Actions environment is safe and owned by ExpectedOwner.
// Outside GitHub Actions (when GITHUB_ACTIONS != "true"), it returns nil so local runs and tests work.
//
// Why not check pull_request_target in workflow YAML `if:` conditions:
//  1. GitHub Actions has no way to share `if:` expressions across workflow files.
//  2. Putting `pull_request_target` in every workflow YAML makes humans and LLMs wonder at first glance
//     if the workflow actually uses that unsafe trigger, adding noise to every file.
func VerifyEnv() error {
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		return nil
	}

	if event := os.Getenv("GITHUB_EVENT_NAME"); event == "pull_request_target" {
		return fmt.Errorf("refusing to run on unsafe event %q", event)
	}

	if owner := os.Getenv("GITHUB_REPOSITORY_OWNER"); owner != ExpectedOwner {
		return fmt.Errorf("unsupported repository owner %q (only %q is supported)", owner, ExpectedOwner)
	}

	return nil
}
