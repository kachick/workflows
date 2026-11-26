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
	ErrInvalidStorePath = errors.New("nix build produced an invalid store path")
)

type Runner interface {
	RunCapture(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error)
	RunInteractive(ctx context.Context, name string, args ...string) error
}

type defaultRunner struct{}

func (defaultRunner) RunCapture(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
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

// quoteNixString encodes s as a double-quoted Nix string literal.
// Go's %q cannot be used because it leaves "${" unescaped (Nix interpolation)
// and emits escape forms like \xNN that Nix rejects.
func quoteNixString(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '$':
			if i+1 < len(s) && s[i+1] == '{' {
				b.WriteString(`\${`)
				i++
			} else {
				b.WriteByte('$')
			}
		default:
			b.WriteByte(s[i])
		}
	}
	b.WriteByte('"')
	return b.String()
}

// BuildLinkFarmExpr returns a Nix expression that collects all exposed packages
// in the flake and combines them into a single linkFarm derivation.
func BuildLinkFarmExpr(repoRoot string, pinName string) string {
	cleanPath := filepath.Clean(repoRoot)
	// Build a linkFarm that references every package in packages.${currentSystem}.
	// Cachix pins closures, so pinning this single store path protects every package.
	return fmt.Sprintf(`let
  f = builtins.getFlake (toString %s);
  system = builtins.currentSystem;
  pkgs = f.inputs.nixpkgs.legacyPackages.${system};
in
  pkgs.linkFarm %s (map (name: { inherit name; path = f.packages.${system}.${name}; }) (builtins.attrNames f.packages.${system}))`,
		quoteNixString(cleanPath),
		quoteNixString(pinName),
	)
}

func (opts *Options) Validate() error {
	opts.Cache = strings.TrimSpace(opts.Cache)
	if opts.Cache == "" {
		return ErrEmptyCache
	}
	opts.Pin = strings.TrimSpace(opts.Pin)
	if opts.Pin == "" {
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
	stdout, stderr, err := opts.Runner.RunCapture(ctx, "nix", "build", "--impure", "--no-link", "--print-out-paths", "--expr", expr)
	if err != nil {
		errOut := strings.TrimSpace(string(stderr))
		if errOut == "" {
			errOut = strings.TrimSpace(string(stdout))
		}
		return fmt.Errorf("nix build failed: %w (output: %s)", err, errOut)
	}

	lines := bytes.Split(bytes.TrimSpace(stdout), []byte("\n"))
	if len(lines) == 0 || len(lines[len(lines)-1]) == 0 {
		return ErrEmptyStorePath
	}
	storePath := strings.TrimSpace(string(lines[len(lines)-1]))
	if !strings.HasPrefix(storePath, "/nix/store/") {
		return fmt.Errorf("%w: %q", ErrInvalidStorePath, storePath)
	}
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
