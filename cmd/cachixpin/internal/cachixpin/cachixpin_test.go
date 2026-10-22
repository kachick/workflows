package cachixpin_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kachick/workflows/cmd/cachixpin/internal/cachixpin"
)

type mockRunner struct {
	combinedOutput []byte
	combinedErr    error
	recordedCalls  [][]string
	interactiveErr error
}

func (m *mockRunner) RunCombined(_ context.Context, name string, args ...string) ([]byte, error) {
	call := append([]string{name}, args...)
	m.recordedCalls = append(m.recordedCalls, call)
	return m.combinedOutput, m.combinedErr
}

func (m *mockRunner) RunInteractive(_ context.Context, name string, args ...string) error {
	call := append([]string{name}, args...)
	m.recordedCalls = append(m.recordedCalls, call)
	return m.interactiveErr
}

func TestBuildLinkFarmExpr(t *testing.T) {
	expr := cachixpin.BuildLinkFarmExpr("/path/to/repo", "workflows")

	if !strings.Contains(expr, `toString "/path/to/repo"`) {
		t.Errorf("expression does not contain repository path: %s", expr)
	}
	if !strings.Contains(expr, `pkgs.linkFarm "workflows"`) {
		t.Errorf("expression does not contain linkFarm pin name: %s", expr)
	}
	if !strings.Contains(expr, "f.packages.${system}") {
		t.Errorf("expression does not map all flake packages: %s", expr)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		opts    cachixpin.Options
		wantErr error
	}{
		{
			name: "valid options",
			opts: cachixpin.Options{
				Cache:         "kachick",
				Pin:           "workflows",
				KeepRevisions: 10,
			},
			wantErr: nil,
		},
		{
			name: "missing cache",
			opts: cachixpin.Options{
				Cache:         "",
				Pin:           "workflows",
				KeepRevisions: 10,
			},
			wantErr: cachixpin.ErrEmptyCache,
		},
		{
			name: "missing pin",
			opts: cachixpin.Options{
				Cache:         "kachick",
				Pin:           "",
				KeepRevisions: 10,
			},
			wantErr: cachixpin.ErrEmptyPin,
		},
		{
			name: "non-positive keep-revisions",
			opts: cachixpin.Options{
				Cache:         "kachick",
				Pin:           "workflows",
				KeepRevisions: 0,
			},
			wantErr: cachixpin.ErrInvalidRevisions,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opts.Validate()
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Validate() = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestRun_Success(t *testing.T) {
	mock := &mockRunner{
		combinedOutput: []byte("building...\n/nix/store/test-hash-workflows\n"),
	}

	opts := cachixpin.Options{
		Cache:         "kachick",
		Pin:           "workflows",
		KeepRevisions: 10,
		RepoRoot:      ".",
		Runner:        mock,
	}

	if err := cachixpin.Run(context.Background(), opts); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	if len(mock.recordedCalls) != 3 {
		t.Fatalf("recordedCalls count = %d, want 3", len(mock.recordedCalls))
	}

	// 1. nix build
	nixCall := mock.recordedCalls[0]
	if nixCall[0] != "nix" || nixCall[1] != "build" {
		t.Errorf("first command is not nix build: %v", nixCall)
	}

	// 2. cachix push
	pushCall := mock.recordedCalls[1]
	expectedPush := []string{"cachix", "push", "kachick", "/nix/store/test-hash-workflows"}
	if strings.Join(pushCall, " ") != strings.Join(expectedPush, " ") {
		t.Errorf("push command mismatch: got %v, want %v", pushCall, expectedPush)
	}

	// 3. cachix pin
	pinCall := mock.recordedCalls[2]
	expectedPin := []string{"cachix", "pin", "kachick", "workflows", "/nix/store/test-hash-workflows", "--keep-revisions", "10"}
	if strings.Join(pinCall, " ") != strings.Join(expectedPin, " ") {
		t.Errorf("pin command mismatch: got %v, want %v", pinCall, expectedPin)
	}
}

func TestRun_DryRun(t *testing.T) {
	mock := &mockRunner{
		combinedOutput: []byte("/nix/store/test-hash-workflows\n"),
	}

	opts := cachixpin.Options{
		Cache:         "kachick",
		Pin:           "workflows",
		KeepRevisions: 10,
		RepoRoot:      ".",
		DryRun:        true,
		Runner:        mock,
	}

	if err := cachixpin.Run(context.Background(), opts); err != nil {
		t.Fatalf("Run() unexpected error in dry-run: %v", err)
	}

	// In dry-run mode, only nix build should be executed, no cachix calls.
	if len(mock.recordedCalls) != 1 {
		t.Fatalf("recordedCalls count in dry-run = %d, want 1", len(mock.recordedCalls))
	}
}
