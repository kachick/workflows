package cachixpin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

var (
	ErrEmptyCache       = errors.New("cache name must not be empty")
	ErrEmptyPin         = errors.New("pin name must not be empty")
	ErrInvalidRevisions = errors.New("keep-revisions must be greater than zero")
	ErrEmptyStorePath   = errors.New("nix build produced an empty store path")
)

type Runner interface {
	RunCombined(ctx context.Context, name string, args ...string) ([]byte, error)
	RunInteractive(ctx context.Context, name string, args ...string) error
}

type defaultRunner struct{}

func (defaultRunner) RunCombined(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput()
}

func (defaultRunner) RunInteractive(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

type Options struct {
	Cache         string
	Pin           string
	KeepRevisions int
	RepoRoot      string
	DryRun        bool
	Runner        Runner
}

// BuildLinkFarmExpr returns a Nix expression that collects all exposed packages
// in the flake and combines them into a single linkFarm derivation.
func BuildLinkFarmExpr(repoRoot string, pinName string) string {
	cleanPath := filepath.Clean(repoRoot)
	// Build a linkFarm that references every package in packages.${currentSystem}.
	// Cachix pins closures, so pinning this single store path protects every package.
	return fmt.Sprintf(`let
  f = builtins.getFlake (toString %q);
  system = builtins.currentSystem;
  pkgs = f.inputs.nixpkgs.legacyPackages.${system};
in
  pkgs.linkFarm %q (map (name: { inherit name; path = f.packages.${system}.${name}; }) (builtins.attrNames f.packages.${system}))`,
		cleanPath,
		pinName,
	)
}

func (opts *Options) Validate() error {
	if strings.TrimSpace(opts.Cache) == "" {
		return ErrEmptyCache
	}
	if strings.TrimSpace(opts.Pin) == "" {
		return ErrEmptyPin
	}
	if opts.KeepRevisions <= 0 {
		return ErrInvalidRevisions
	}
	if opts.RepoRoot == "" {
		opts.RepoRoot = "."
	}
	if opts.Runner == nil {
		opts.Runner = defaultRunner{}
	}
	return nil
}

func Run(ctx context.Context, opts Options) error {
	if err := opts.Validate(); err != nil {
		return err
	}

	absRoot, err := filepath.Abs(opts.RepoRoot)
	if err != nil {
		return fmt.Errorf("resolve repository root: %w", err)
	}

	expr := BuildLinkFarmExpr(absRoot, opts.Pin)

	fmt.Printf("Building linkFarm derivation for pin %q...\n", opts.Pin)
	buildOutput, err := opts.Runner.RunCombined(ctx, "nix", "build", "--impure", "--no-link", "--print-out-paths", "--expr", expr)
	if err != nil {
		return fmt.Errorf("nix build failed: %w (output: %s)", err, strings.TrimSpace(string(buildOutput)))
	}

	lines := bytes.Split(bytes.TrimSpace(buildOutput), []byte("\n"))
	if len(lines) == 0 || len(lines[len(lines)-1]) == 0 {
		return ErrEmptyStorePath
	}
	storePath := string(lines[len(lines)-1])
	fmt.Printf("Built store path: %s\n", storePath)

	if opts.DryRun {
		fmt.Printf("[dry-run] Would run: cachix push %s %s\n", opts.Cache, storePath)
		fmt.Printf("[dry-run] Would run: cachix pin %s %s %s --keep-revisions %d\n", opts.Cache, opts.Pin, storePath, opts.KeepRevisions)
		return nil
	}

	fmt.Printf("Pushing store path to Cachix cache %q...\n", opts.Cache)
	if err := opts.Runner.RunInteractive(ctx, "cachix", "push", opts.Cache, storePath); err != nil {
		return fmt.Errorf("cachix push failed: %w", err)
	}

	fmt.Printf("Pinning store path on Cachix cache %q as %q (keep-revisions: %d)...\n", opts.Cache, opts.Pin, opts.KeepRevisions)
	if err := opts.Runner.RunInteractive(ctx, "cachix", "pin", opts.Cache, opts.Pin, storePath, "--keep-revisions", strconv.Itoa(opts.KeepRevisions)); err != nil {
		return fmt.Errorf("cachix pin failed: %w", err)
	}

	fmt.Println("Successfully pushed and pinned all packages.")
	return nil
}
