package workflowmeta

import (
	"testing"
)

func TestMetadataIsComplete(t *testing.T) {
	complete := Metadata{
		ServerURL:          "https://github.com",
		Repository:         "kachick/workflows",
		RunID:              "12345",
		WorkflowRepository: "kachick/workflows",
		WorkflowSHA:        "abcdef123",
		WorkflowFilePath:   ".github/workflows/reusable-automerge.yml",
	}

	if !complete.IsComplete() {
		t.Errorf("expected complete metadata to be complete")
	}

	tests := []struct {
		name   string
		modify func(m *Metadata)
	}{
		{"missing ServerURL", func(m *Metadata) { m.ServerURL = "" }},
		{"missing Repository", func(m *Metadata) { m.Repository = "" }},
		{"missing RunID", func(m *Metadata) { m.RunID = "" }},
		{"missing WorkflowRepository", func(m *Metadata) { m.WorkflowRepository = "" }},
		{"missing WorkflowSHA", func(m *Metadata) { m.WorkflowSHA = "" }},
		{"missing WorkflowFilePath", func(m *Metadata) { m.WorkflowFilePath = "" }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := complete
			tc.modify(&m)
			if m.IsComplete() {
				t.Errorf("expected incomplete metadata for %s", tc.name)
			}
		})
	}
}

func TestMetadataProvenanceText(t *testing.T) {
	t.Run("complete metadata generates provenance links", func(t *testing.T) {
		meta := Metadata{
			ServerURL:          "https://github.com",
			Repository:         "kachick/workflows",
			RunID:              "12345678",
			WorkflowRepository: "kachick/workflows",
			WorkflowSHA:        "abcdef123456",
			WorkflowFilePath:   ".github/workflows/reusable-automerge.yml",
		}

		expected := "Merged automatically by [workflow run](https://github.com/kachick/workflows/actions/runs/12345678) (defined at [kachick/workflows](https://github.com/kachick/workflows/blob/abcdef123456/.github/workflows/reusable-automerge.yml))."
		got := meta.ProvenanceText("Merged automatically")
		if got != expected {
			t.Errorf("got %q, want %q", got, expected)
		}
	})

	t.Run("incomplete metadata falls back to simple sentence", func(t *testing.T) {
		meta := Metadata{
			ServerURL: "https://github.com",
		}

		expected := "Merged automatically."
		got := meta.ProvenanceText("Merged automatically")
		if got != expected {
			t.Errorf("got %q, want %q", got, expected)
		}
	})
}

func TestGet(t *testing.T) {
	t.Setenv("GITHUB_SERVER_URL", "https://custom.github.com")
	t.Setenv("GITHUB_REPOSITORY", "kachick/workflows")
	t.Setenv("GITHUB_RUN_ID", "1111")
	t.Setenv("WORKFLOW_REPOSITORY", "kachick/workflows")
	t.Setenv("WORKFLOW_SHA", "sha222")
	t.Setenv("WORKFLOW_FILE_PATH", ".github/workflows/ci.yml")

	meta := Get()
	if meta.ServerURL != "https://custom.github.com" {
		t.Errorf("expected ServerURL from env, got %q", meta.ServerURL)
	}
	if meta.RunID != "1111" {
		t.Errorf("expected RunID from env, got %q", meta.RunID)
	}
	if !meta.IsComplete() {
		t.Errorf("expected Get() to return complete metadata with env vars")
	}
}
