package workflowlint

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractWorkflowName(t *testing.T) {
	tests := []struct {
		filename string
		wantName string
		wantOk   bool
	}{
		{"reusable-automerge.yml", "automerge", true},
		{"reusable-bump-flake-lock.yaml", "bump-flake-lock", true},
		{"reusable-nix-diff-closures.yml", "nix-diff-closures", true},
		{"lint.yml", "", false},
		{"ci-nix.yml", "", false},
		{"automerge.yml", "", false},
		{"reusable-.yml", "", false},
		{"not-reusable-foo.yml", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			gotName, gotOk := ExtractWorkflowName(tt.filename)
			if gotOk != tt.wantOk || gotName != tt.wantName {
				t.Errorf("ExtractWorkflowName(%q) = (%q, %v), want (%q, %v)",
					tt.filename, gotName, gotOk, tt.wantName, tt.wantOk)
			}
		})
	}
}

func TestParseDocumentedWorkflows(t *testing.T) {
	t.Run("extracts workflow names from markdown table", func(t *testing.T) {
		readme := `# Title

## Available Workflows

| Reusable Workflow | Description |
| :--- | --- |
| [nix-diff-closures](pkgs/nix-diff-closures/README.md) | Diff Nix closures |
| [bump-flake-lock](pkgs/bump-flake-lock/README.md) | Bump flake |
| ` + "`automerge`" + ` | Merge PRs |

## Other Section
`
		documented, err := ParseDocumentedWorkflows(readme)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expected := []string{"nix-diff-closures", "bump-flake-lock", "automerge"}
		for _, name := range expected {
			if !documented[name] {
				t.Errorf("expected workflow %q to be documented", name)
			}
		}
		if len(documented) != len(expected) {
			t.Errorf("expected %d documented workflows, got %d", len(expected), len(documented))
		}
	})

	t.Run("returns error when section is missing", func(t *testing.T) {
		readme := `# Title

Some intro text without the target section.
`
		_, err := ParseDocumentedWorkflows(readme)
		if !errors.Is(err, ErrSectionNotFound) {
			t.Errorf("expected ErrSectionNotFound, got %v", err)
		}
	})
}

func TestFindMissingWorkflows(t *testing.T) {
	existing := []string{"automerge", "bump-flake-lock", "new-workflow"}
	documented := map[string]bool{
		"automerge":       true,
		"bump-flake-lock": true,
	}

	missing := FindMissingWorkflows(existing, documented)
	if len(missing) != 1 || missing[0] != "new-workflow" {
		t.Errorf("expected [new-workflow], got %v", missing)
	}
}

func TestCheckCurrentRepository(t *testing.T) {
	// Root of the repository is two directories up from internal/workflowlint
	repoRoot := filepath.Join("..", "..")

	missing, err := Check(repoRoot)
	if err != nil {
		t.Fatalf("Check(repoRoot) returned unexpected error: %v", err)
	}
	if len(missing) != 0 {
		t.Errorf("expected no missing workflows in current repository, got %v", missing)
	}
}

func TestCheckDetectsUndocumentedWorkflow(t *testing.T) {
	tempDir := t.TempDir()

	workflowDir := filepath.Join(tempDir, ".github", "workflows")
	if err := os.MkdirAll(workflowDir, 0o755); err != nil {
		t.Fatalf("failed to create workflow dir: %v", err)
	}

	// Create reusable workflow files
	files := []string{
		"reusable-documented.yml",
		"reusable-undocumented.yml",
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(workflowDir, f), []byte("name: test\n"), 0o600); err != nil {
			t.Fatalf("failed to write workflow file: %v", err)
		}
	}

	readmeContent := `# Title

## Available Workflows

| Reusable Workflow | Description |
| :--- | --- |
| [documented](pkgs/documented/README.md) | Some description |

## Next Section
`
	if err := os.WriteFile(filepath.Join(tempDir, "README.md"), []byte(readmeContent), 0o600); err != nil {
		t.Fatalf("failed to write README.md: %v", err)
	}

	missing, err := Check(tempDir)
	if err != nil {
		t.Fatalf("unexpected error from Check: %v", err)
	}

	if len(missing) != 1 || missing[0] != "undocumented" {
		t.Errorf("expected [undocumented], got %v", missing)
	}
}
