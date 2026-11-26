package cachixpin_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kachick/workflows/pkgs/cachixpin/internal/cachixpin"
)

type mockRunner struct {
	stdout          []byte
	stderr          []byte
	captureErr      error
	recordedCalls   [][]string
	interactiveErrs []error
}

func (m *mockRunner) RunCapture(_ context.Context, name string, args ...string) ([]byte, []byte, error) {
	call := append([]string{name}, args...)
	m.recordedCalls = append(m.recordedCalls, call)
	return m.stdout, m.stderr, m.captureErr
}

func (m *mockRunner) RunInteractive(_ context.Context, name string, args ...string) error {
	call := append([]string{name}, args...)
	m.recordedCalls = append(m.recordedCalls, call)
	if len(m.interactiveErrs) == 0 {
		return nil
	}
	err := m.interactiveErrs[0]
	m.interactiveErrs = m.interactiveErrs[1:]
	return err
}

func TestBuildLinkFarmExpr(t *testing.T) {
	expr := cachixpin.BuildLinkFarmExpr("/path/to/${repo}", `work"flows`)

	if !strings.Contains(expr, `toString "/path/to/\${repo}"`) {
		t.Errorf("expression must escape Nix interpolation in path: %s", expr)
	}
	if !strings.Contains(expr, `pkgs.linkFarm "work\"flows"`) {
		t.Errorf("expression must escape quotes in pin name: %s", expr)
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
			name: "valid options trims surrounding whitespace",
			opts: cachixpin.Options{
				Cache:         " kachick ",
				Pin:           " workflows ",
				KeepRevisions: 10,
			},
			wantErr: nil,
		},
		{
			name: "missing cache",
			opts: cachixpin.Options{
				Cache:         "   ",
				Pin:           "workflows",
				KeepRevisions: 10,
			},
			wantErr: cachixpin.ErrEmptyCache,
		},
		{
			name: "missing pin",
			opts: cachixpin.Options{
				Cache:         "kachick",
				Pin:           "\t",
				KeepRevisions: 10,
			},
			wantErr: cachixpin.ErrEmptyPin,
		},
		{
			name: "zero keep-revisions",
			opts: cachixpin.Options{
				Cache:         "kachick",
				Pin:           "workflows",
				KeepRevisions: 0,
			},
			wantErr: cachixpin.ErrInvalidRevisions,
		},
		{
			name: "negative keep-revisions",
			opts: cachixpin.Options{
				Cache:         "kachick",
				Pin:           "workflows",
				KeepRevisions: -1,
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
			if err == nil && (tt.opts.Cache != "kachick" || tt.opts.Pin != "workflows") {
				t.Errorf("Validate() did not trim fields: cache=%q pin=%q", tt.opts.Cache, tt.opts.Pin)
			}
		})
	}
}

func TestRun_SuccessIgnoresStderrWarnings(t *testing.T) {
	mock := &mockRunner{
		stdout: []byte("/nix/store/test-hash-workflows\n"),
		stderr: []byte("warning: Git tree is dirty\n"),
	}

	opts := cachixpin.Options{
		Cache:         " kachick ",
		Pin:           " workflows ",
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

	if got, want := strings.Join(mock.recordedCalls[1], " "), "cachix push kachick /nix/store/test-hash-workflows"; got != want {
		t.Errorf("push command = %q, want %q", got, want)
	}
	if got, want := strings.Join(mock.recordedCalls[2], " "), "cachix pin kachick workflows /nix/store/test-hash-workflows --keep-revisions 10"; got != want {
		t.Errorf("pin command = %q, want %q", got, want)
	}
}

func TestRun_DryRun(t *testing.T) {
	mock := &mockRunner{
		stdout: []byte("/nix/store/test-hash-workflows\n"),
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

	if len(mock.recordedCalls) != 1 {
		t.Fatalf("recordedCalls count in dry-run = %d, want 1", len(mock.recordedCalls))
	}
}

func TestRun_Errors(t *testing.T) {
	baseOpts := func(m *mockRunner) cachixpin.Options {
		return cachixpin.Options{
			Cache:         "kachick",
			Pin:           "workflows",
			KeepRevisions: 10,
			RepoRoot:      ".",
			Runner:        m,
		}
	}

	t.Run("empty stdout returns ErrEmptyStorePath", func(t *testing.T) {
		mock := &mockRunner{stdout: []byte("  \n")}
		if err := cachixpin.Run(context.Background(), baseOpts(mock)); !errors.Is(err, cachixpin.ErrEmptyStorePath) {
			t.Fatalf("expected ErrEmptyStorePath, got %v", err)
		}
	})

	t.Run("non-store path returns ErrInvalidStorePath", func(t *testing.T) {
		mock := &mockRunner{stdout: []byte("warning: unexpected output\n")}
		if err := cachixpin.Run(context.Background(), baseOpts(mock)); !errors.Is(err, cachixpin.ErrInvalidStorePath) {
			t.Fatalf("expected ErrInvalidStorePath, got %v", err)
		}
	})

	t.Run("nix build failure stops before cachix", func(t *testing.T) {
		mock := &mockRunner{captureErr: errors.New("exit status 1"), stderr: []byte("eval error")}
		err := cachixpin.Run(context.Background(), baseOpts(mock))
		if err == nil || !strings.Contains(err.Error(), "eval error") {
			t.Fatalf("expected nix build error with stderr, got %v", err)
		}
		if len(mock.recordedCalls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(mock.recordedCalls))
		}
	})

	t.Run("cachix push failure stops before cachix pin", func(t *testing.T) {
		mock := &mockRunner{
			stdout:          []byte("/nix/store/test-hash-workflows\n"),
			interactiveErrs: []error{errors.New("push failed")},
		}
		if err := cachixpin.Run(context.Background(), baseOpts(mock)); err == nil {
			t.Fatal("expected error from cachix push, got nil")
		}
		if len(mock.recordedCalls) != 2 {
			t.Fatalf("expected 2 calls (build + push), got %d", len(mock.recordedCalls))
		}
	})

	t.Run("cachix pin failure returns error", func(t *testing.T) {
		mock := &mockRunner{
			stdout:          []byte("/nix/store/test-hash-workflows\n"),
			interactiveErrs: []error{nil, errors.New("pin failed")},
		}
		if err := cachixpin.Run(context.Background(), baseOpts(mock)); err == nil {
			t.Fatal("expected error from cachix pin, got nil")
		}
		if len(mock.recordedCalls) != 3 {
			t.Fatalf("expected 3 calls, got %d", len(mock.recordedCalls))
		}
	})
}
