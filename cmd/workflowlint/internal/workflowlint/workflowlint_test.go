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
	// Root of the repository is four directories up from cmd/workflowlint/internal/workflowlint
	repoRoot := filepath.Join("..", "..", "..", "..")

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

func TestExtractNixInstallerStep(t *testing.T) {
	t.Run("extracts step when uses is first", func(t *testing.T) {
		workflow := `
jobs:
  test:
    steps:
      - uses: actions/checkout@v7
      - uses: NixOS/nix-installer-action@v1
        with:
          extra-conf: |
            access-tokens = github.com=token
      - run: nix flake check
`
		step, ok := ExtractNixInstallerStep(workflow)
		if !ok {
			t.Fatal("expected step to be found")
		}
		expected := `- uses: NixOS/nix-installer-action@v1
  with:
    extra-conf: |
      access-tokens = github.com=token`
		if step != expected {
			t.Errorf("ExtractNixInstallerStep() =\n%q\nwant:\n%q", step, expected)
		}
	})

	t.Run("extracts step when name is first", func(t *testing.T) {
		workflow := `
jobs:
  test:
    steps:
      - name: Install Nix
        uses: NixOS/nix-installer-action@v1
      - run: nix flake check
`
		step, ok := ExtractNixInstallerStep(workflow)
		if !ok {
			t.Fatal("expected step to be found")
		}
		expected := `- name: Install Nix
  uses: NixOS/nix-installer-action@v1`
		if step != expected {
			t.Errorf("ExtractNixInstallerStep() =\n%q\nwant:\n%q", step, expected)
		}
	})

	t.Run("returns false when nix-installer-action is not used", func(t *testing.T) {
		workflow := `
jobs:
  test:
    steps:
      - uses: actions/checkout@v7
      - run: echo hello
`
		step, ok := ExtractNixInstallerStep(workflow)
		if ok || step != "" {
			t.Errorf("expected false, got (%q, %v)", step, ok)
		}
	})
}

func TestCheckNixInstallerStepsCurrentRepository(t *testing.T) {
	repoRoot := filepath.Join("..", "..", "..", "..")
	if err := CheckNixInstallerSteps(repoRoot); err != nil {
		t.Fatalf("CheckNixInstallerSteps(repoRoot) failed: %v", err)
	}
}

func TestCheckNixInstallerStepsDetectsMismatch(t *testing.T) {
	tempDir := t.TempDir()
	workflowDir := filepath.Join(tempDir, ".github", "workflows")
	if err := os.MkdirAll(workflowDir, 0o755); err != nil {
		t.Fatalf("failed to create workflow dir: %v", err)
	}

	workflowA := `
jobs:
  test:
    steps:
      - uses: NixOS/nix-installer-action@v1
        with:
          extra-conf: access-tokens = a
`
	workflowB := `
jobs:
  test:
    steps:
      - uses: NixOS/nix-installer-action@v1
        with:
          extra-conf: access-tokens = b
`

	if err := os.WriteFile(filepath.Join(workflowDir, "reusable-a.yml"), []byte(workflowA), 0o600); err != nil {
		t.Fatalf("failed to write workflow A: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workflowDir, "reusable-b.yml"), []byte(workflowB), 0o600); err != nil {
		t.Fatalf("failed to write workflow B: %v", err)
	}

	err := CheckNixInstallerSteps(tempDir)
	if err == nil {
		t.Fatal("expected error due to mismatch, got nil")
	}
}

func TestCheckNixInstallerStepsIgnoresWorkflowsWithoutNix(t *testing.T) {
	tempDir := t.TempDir()
	workflowDir := filepath.Join(tempDir, ".github", "workflows")
	if err := os.MkdirAll(workflowDir, 0o755); err != nil {
		t.Fatalf("failed to create workflow dir: %v", err)
	}

	workflowA := `
jobs:
  test:
    steps:
      - uses: NixOS/nix-installer-action@v1
`
	workflowB := `
jobs:
  test:
    steps:
      - uses: NixOS/nix-installer-action@v1
`
	workflowNonNix := `
jobs:
  test:
    steps:
      - uses: actions/checkout@v7
`

	if err := os.WriteFile(filepath.Join(workflowDir, "reusable-a.yml"), []byte(workflowA), 0o600); err != nil {
		t.Fatalf("failed to write workflow A: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workflowDir, "reusable-b.yml"), []byte(workflowB), 0o600); err != nil {
		t.Fatalf("failed to write workflow B: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workflowDir, "reusable-non-nix.yml"), []byte(workflowNonNix), 0o600); err != nil {
		t.Fatalf("failed to write non-nix workflow: %v", err)
	}

	if err := CheckNixInstallerSteps(tempDir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCheckNixInstallerStepsDetectsCINixMismatch(t *testing.T) {
	tempDir := t.TempDir()
	workflowDir := filepath.Join(tempDir, ".github", "workflows")
	if err := os.MkdirAll(workflowDir, 0o755); err != nil {
		t.Fatalf("failed to create workflow dir: %v", err)
	}

	reusable := `
jobs:
  test:
    steps:
      - uses: NixOS/nix-installer-action@v1
        with:
          extra-conf: access-tokens = a
`
	ciNix := `
jobs:
  tasks:
    steps:
      - uses: NixOS/nix-installer-action@v1
        with:
          extra-conf: access-tokens = b
`
	if err := os.WriteFile(filepath.Join(workflowDir, "reusable-a.yml"), []byte(reusable), 0o600); err != nil {
		t.Fatalf("failed to write reusable workflow: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workflowDir, "ci-nix.yml"), []byte(ciNix), 0o600); err != nil {
		t.Fatalf("failed to write ci-nix.yml: %v", err)
	}

	if err := CheckNixInstallerSteps(tempDir); err == nil {
		t.Fatal("expected error due to ci-nix.yml mismatch, got nil")
	}
}

func TestCheckReusableJobGuardsCurrentRepository(t *testing.T) {
	repoRoot := filepath.Join("..", "..", "..", "..")
	if err := CheckReusableJobGuards(repoRoot); err != nil {
		t.Fatalf("CheckReusableJobGuards(repoRoot) failed: %v", err)
	}
}

func TestCheckReusableJobGuardsDetectsMissingFields(t *testing.T) {
	t.Run("missing owner guard", func(t *testing.T) {
		tempDir := t.TempDir()
		workflowDir := filepath.Join(tempDir, ".github", "workflows")
		if err := os.MkdirAll(workflowDir, 0o755); err != nil {
			t.Fatalf("failed to create workflow dir: %v", err)
		}

		content := `
jobs:
  build:
    runs-on: ubuntu-26.04
    timeout-minutes: 10
    steps:
      - run: echo hi
`
		if err := os.WriteFile(filepath.Join(workflowDir, "reusable-test.yml"), []byte(content), 0o600); err != nil {
			t.Fatalf("failed to write workflow: %v", err)
		}

		if err := CheckReusableJobGuards(tempDir); err == nil {
			t.Fatal("expected error when owner guard is missing, got nil")
		}
	})

	t.Run("missing timeout-minutes", func(t *testing.T) {
		tempDir := t.TempDir()
		workflowDir := filepath.Join(tempDir, ".github", "workflows")
		if err := os.MkdirAll(workflowDir, 0o755); err != nil {
			t.Fatalf("failed to create workflow dir: %v", err)
		}

		content := `
jobs:
  build:
    if: github.repository_owner == 'kachick'
    runs-on: ubuntu-26.04
    steps:
      - run: echo hi
`
		if err := os.WriteFile(filepath.Join(workflowDir, "reusable-test.yml"), []byte(content), 0o600); err != nil {
			t.Fatalf("failed to write workflow: %v", err)
		}

		if err := CheckReusableJobGuards(tempDir); err == nil {
			t.Fatal("expected error when timeout-minutes is missing, got nil")
		}
	})
}
