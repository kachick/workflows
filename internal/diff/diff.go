package diff

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/kachick/nix-diff-closures-action/internal/report"
)

// CommandExecutor abstracts command execution for testability.
type CommandExecutor interface {
	Run(ctx context.Context, name string, args ...string) (string, error)
}

// DefaultCommandExecutor executes commands via os/exec.
type DefaultCommandExecutor struct {
	Dir string
}

func (e *DefaultCommandExecutor) Run(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if e.Dir != "" {
		cmd.Dir = e.Dir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// ParseTargets parses comma or newline separated target strings and replaces system placeholder.
func ParseTargets(input string, system string) []string {
	var targets []string
	lines := strings.FieldsFunc(input, func(r rune) bool {
		return r == '\n' || r == ','
	})

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// Strip leading '.#' if user specified it
		trimmed = strings.TrimPrefix(trimmed, ".#")
		// Replace ${SYSTEM} placeholder
		resolved := strings.ReplaceAll(trimmed, "${SYSTEM}", system)
		targets = append(targets, resolved)
	}

	if len(targets) == 0 {
		targets = append(targets, fmt.Sprintf("devShells.%s.default", system))
	}

	return targets
}

// Engine inspects Nix derivations and computes closure diffs.
type Engine struct {
	Exec    CommandExecutor
	RepoDir string
}

// CurrentSystem queries Nix for builtins.currentSystem.
func (e *Engine) CurrentSystem(ctx context.Context) (string, error) {
	out, err := e.Exec.Run(ctx, "nix", "eval", "--raw", "--impure", "--expr", "builtins.currentSystem")
	if err != nil {
		return "", fmt.Errorf("failed to query nix system: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// Run computes closure differences for all targets between baseRev and current HEAD.
func (e *Engine) Run(ctx context.Context, baseRev string, rawTargets string) ([]report.TargetResult, error) {
	system, err := e.CurrentSystem(ctx)
	if err != nil {
		return nil, err
	}

	targets := ParseTargets(rawTargets, system)
	var results []report.TargetResult

	for _, target := range targets {
		res := e.diffTarget(ctx, baseRev, target)
		results = append(results, res)
	}

	return results, nil
}

func (e *Engine) diffTarget(ctx context.Context, baseRev string, target string) report.TargetResult {
	prInstallable := fmt.Sprintf(".#%s", target)
	headDrv, err := e.Exec.Run(ctx, "nix", "path-info", "--derivation", prInstallable)
	if err != nil {
		return report.TargetResult{
			Target: target,
			Error:  fmt.Sprintf("failed to evaluate PR derivation (%s): %v", prInstallable, err),
		}
	}

	baseInstallable := fmt.Sprintf("git+file://%s?rev=%s#%s", e.RepoDir, baseRev, target)
	baseDrv, err := e.Exec.Run(ctx, "nix", "path-info", "--derivation", baseInstallable)
	if err != nil {
		// If base evaluation fails, it might be a newly added target or removed attribute in base
		return report.TargetResult{
			Target:   target,
			HeadDrv:  headDrv,
			DiffText: fmt.Sprintf("(Target not found in base revision %s)\n", baseRev),
		}
	}

	if headDrv == baseDrv {
		return report.TargetResult{
			Target:   target,
			BaseDrv:  baseDrv,
			HeadDrv:  headDrv,
			NoChange: true,
		}
	}

	diffOut, err := e.Exec.Run(ctx, "nix", "store", "diff-closures", baseDrv, headDrv)
	if err != nil {
		return report.TargetResult{
			Target:  target,
			BaseDrv: baseDrv,
			HeadDrv: headDrv,
			Error:   fmt.Sprintf("nix store diff-closures failed: %v", err),
		}
	}

	if strings.TrimSpace(diffOut) == "" {
		return report.TargetResult{
			Target:   target,
			BaseDrv:  baseDrv,
			HeadDrv:  headDrv,
			NoChange: true,
		}
	}

	return report.TargetResult{
		Target:   target,
		BaseDrv:  baseDrv,
		HeadDrv:  headDrv,
		DiffText: diffOut,
	}
}
