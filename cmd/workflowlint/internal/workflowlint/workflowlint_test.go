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
		{"ci.yml", "", false},
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
	t.Run("extracts workflow names from markdown table including rows with dashes", func(t *testing.T) {
		readme := `# Title

## Available Workflows

| Reusable Workflow | Description |
| :--- | --- |
| [nix-diff-closures](pkgs/nix-diff-closures/README.md) | Diff Nix closures --- even with triple dashes |
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

func TestCheckCurrentRepository(t *testing.T) {
	// Root of the repository is four directories up from cmd/workflowlint/internal/workflowlint
	repoRoot := filepath.Join("..", "..", "..", "..")

	missing, stale, err := Check(repoRoot)
	if err != nil {
		t.Fatalf("Check(repoRoot) returned unexpected error: %v", err)
	}
	if len(missing) != 0 {
		t.Errorf("expected no missing workflows in current repository, got %v", missing)
	}
	if len(stale) != 0 {
		t.Errorf("expected no stale workflows in current repository, got %v", stale)
	}
}

func TestCheckDetectsUndocumentedAndStaleWorkflows(t *testing.T) {
	tempDir := t.TempDir()

	workflowDir := filepath.Join(tempDir, ".github", "workflows")
	if err := os.MkdirAll(workflowDir, 0o755); err != nil {
		t.Fatalf("failed to create workflow dir: %v", err)
	}

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
| [removed](pkgs/removed/README.md) | No longer exists |

## Next Section
`
	if err := os.WriteFile(filepath.Join(tempDir, "README.md"), []byte(readmeContent), 0o600); err != nil {
		t.Fatalf("failed to write README.md: %v", err)
	}

	missing, stale, err := Check(tempDir)
	if err != nil {
		t.Fatalf("unexpected error from Check: %v", err)
	}

	if len(missing) != 1 || missing[0] != "undocumented" {
		t.Errorf("expected missing [undocumented], got %v", missing)
	}
	if len(stale) != 1 || stale[0] != "removed" {
		t.Errorf("expected stale [removed], got %v", stale)
	}
}

func TestExtractNixInstallerSteps(t *testing.T) {
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

	t.Run("extracts multiple steps across jobs and ignores comments or nested dashes", func(t *testing.T) {
		workflow := `
jobs:
  first:
    steps:
      - name: Run script with nested dash
        run: |
          echo "- NixOS/nix-installer-action@v0"
          - item
      - name: Install Nix
        uses: NixOS/nix-installer-action@v1
  second:
    steps:
      - name: Install Nix
        uses: NixOS/nix-installer-action@v2
`
		steps, err := ExtractNixInstallerSteps(workflow)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(steps) != 2 {
			t.Fatalf("expected 2 steps, got %d: %#v", len(steps), steps)
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

	reusable := `
jobs:
  test:
    steps:
      - uses: NixOS/nix-installer-action@v1
        with:
          extra-conf: access-tokens = a
`
	ci := `
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
	if err := os.WriteFile(filepath.Join(workflowDir, "ci.yml"), []byte(ci), 0o600); err != nil {
		t.Fatalf("failed to write ci.yml: %v", err)
	}

	if err := CheckNixInstallerSteps(tempDir); err == nil {
		t.Fatal("expected error due to ci.yml mismatch, got nil")
	}
}

func TestCheckReusableJobGuardsCurrentRepository(t *testing.T) {
	repoRoot := filepath.Join("..", "..", "..", "..")
	if err := CheckReusableJobGuards(repoRoot); err != nil {
		t.Fatalf("CheckReusableJobGuards(repoRoot) failed: %v", err)
	}
}

func TestCheckReusableJobGuardsValidation(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr bool
	}{
		{
			name: "valid job with inline comment and combined && guard",
			content: `
jobs:
  build: # main job
    if: github.repository_owner == 'kachick' && inputs.enabled
    timeout-minutes: 10 # 10 min limit
    steps:
      - run: echo hi
`,
			wantErr: false,
		},
		{
			name: "missing owner guard",
			content: `
jobs:
  build:
    runs-on: ubuntu-26.04
    timeout-minutes: 10
    steps:
      - run: echo hi
`,
			wantErr: true,
		},
		{
			name: "bypassable owner guard with logical OR",
			content: `
jobs:
  build:
    if: true || github.repository_owner == 'kachick'
    timeout-minutes: 10
    steps:
      - run: echo hi
`,
			wantErr: true,
		},
		{
			name: "owner guard only in comment",
			content: `
jobs:
  build:
    if: true # github.repository_owner == 'kachick'
    timeout-minutes: 10
    steps:
      - run: echo hi
`,
			wantErr: true,
		},
		{
			name: "missing timeout-minutes",
			content: `
jobs:
  build:
    if: github.repository_owner == 'kachick'
    runs-on: ubuntu-26.04
    steps:
      - run: echo hi
`,
			wantErr: true,
		},
		{
			name: "non-positive timeout-minutes",
			content: `
jobs:
  build:
    if: github.repository_owner == 'kachick'
    timeout-minutes: 0
    steps:
      - run: echo hi
`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			workflowDir := filepath.Join(tempDir, ".github", "workflows")
			if err := os.MkdirAll(workflowDir, 0o755); err != nil {
				t.Fatalf("failed to create workflow dir: %v", err)
			}
			if err := os.WriteFile(filepath.Join(workflowDir, "reusable-test.yml"), []byte(tt.content), 0o600); err != nil {
				t.Fatalf("failed to write workflow: %v", err)
			}

			err := CheckReusableJobGuards(tempDir)
			if tt.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
