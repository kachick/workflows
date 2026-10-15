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

// ProvenanceText formats a Markdown sentence with run and definition links if metadata is complete.
// If metadata is incomplete, it returns a plain sentence without links.
func (m Metadata) ProvenanceText(action string) string {
	if m.IsComplete() {
		return fmt.Sprintf("%s by [workflow run](%s) (defined at [%s](%s)).",
			action, m.RunURL(), m.WorkflowRepository, m.WorkflowPermalink())
	}
	return fmt.Sprintf("%s.", action)
}
