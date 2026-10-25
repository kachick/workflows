package diff

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kachick/workflows/pkgs/nix-diff-closures/internal/report"
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

// ParseTargets parses comma or newline separated flake attribute paths.
func ParseTargets(input string) []string {
	var targets []string
	lines := strings.FieldsFunc(input, func(r rune) bool {
		return r == '\n' || r == ','
	})

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		trimmed = strings.TrimPrefix(trimmed, ".#")
		targets = append(targets, trimmed)
	}

	return targets
}

// Engine inspects Nix derivations and computes closure diffs.
type Engine struct {
	Exec    CommandExecutor
	RepoDir string
}

// Run computes closure differences for all targets between baseRev and current HEAD.
func (e *Engine) Run(ctx context.Context, baseRev string, rawTargets string) ([]report.TargetResult, error) {
	targets := ParseTargets(rawTargets)
	if len(targets) == 0 {
		return nil, errors.New("at least one target must be specified")
	}

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

	// Escape baseRev to avoid breaking URL query or fragment syntax if unexpected characters are passed.
	// NOTE: Avoid invoking `git rev-parse` here to keep the CLI simple and fast without extra subprocess calls.
	baseInstallable := fmt.Sprintf("git+file://%s?rev=%s#%s", e.RepoDir, url.QueryEscape(baseRev), target)
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

	diffOut = normalizeDiffOutput(diffOut)

	if strings.TrimSpace(diffOut) == "" {
		return report.TargetResult{
			Target:   target,
			BaseDrv:  baseDrv,
			HeadDrv:  headDrv,
			NoChange: true,
		}
	}

	treeOut, _ := e.Exec.Run(ctx, "nix-store", "--query", "--tree", headDrv)
	whyDependsOut := e.computeWhyDepends(ctx, headDrv, diffOut)

	return report.TargetResult{
		Target:         target,
		BaseDrv:        baseDrv,
		HeadDrv:        headDrv,
		DiffText:       diffOut,
		TreeText:       treeOut,
		WhyDependsText: whyDependsOut,
	}
}

// Why query why-depends only for updated packages found in diff-closures?
//
// Running `nix why-depends` is essentially an N+1 operation against subprocesses.
// A raw derivation closure can contain thousands of internal `.drv` nodes.
// Running why-depends for every single `.drv` node would easily time out the CI job.
//
// By filtering to packages summarized in `diff-closures`, we keep the command count
// to a small number of packages, finishing in a reasonable time.
// For any dependencies not listed, `tree.txt` (a single O(1) command) gives
// the full dependency tree.
func (e *Engine) computeWhyDepends(ctx context.Context, headDrv string, diffText string) string {
	pkgs := ExtractUpdatedPackages(diffText)
	if len(pkgs) == 0 {
		return ""
	}

	headPathsRaw, err := e.Exec.Run(ctx, "nix", "path-info", "-r", "--derivation", headDrv)
	if err != nil {
		return fmt.Sprintf("# Failed to query head derivation closure paths: %v\n", err)
	}

	pathMap := make(map[string]string)
	for _, p := range strings.Split(headPathsRaw, "\n") {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			continue
		}
		base := filepath.Base(trimmed)
		parts := strings.SplitN(base, "-", 2)
		if len(parts) == 2 {
			pathMap[parts[1]] = trimmed
		}
	}

	seenDrv := make(map[string]bool)
	var b strings.Builder

	for _, pkg := range pkgs {
		drvPath := findMatchingDrv(pathMap, pkg)
		if drvPath == "" || seenDrv[drvPath] {
			continue
		}
		seenDrv[drvPath] = true

		verLabel := strings.TrimSuffix(pkg.Version, ".drv")
		out, err := e.Exec.Run(ctx, "nix", "why-depends", "--derivation", headDrv, drvPath)
		if err != nil {
			b.WriteString(fmt.Sprintf("# why-depends for %s (%s): %v\n\n", pkg.Name, verLabel, err))
		} else {
			trimmedOut := simplifyWhyDependsOutput(out)
			if trimmedOut != "" {
				b.WriteString(fmt.Sprintf("# why-depends for %s (%s)\n%s\n\n", pkg.Name, verLabel, trimmedOut))
			}
		}
	}

	return b.String()
}

func findMatchingDrv(pathMap map[string]string, pkg Package) string {
	key := pkg.Name + "-" + pkg.Version
	if p, ok := pathMap[key]; ok {
		return p
	}
	if !strings.HasSuffix(key, ".drv") {
		if p, ok := pathMap[key+".drv"]; ok {
			return p
		}
	}

	// Loose fallback in case version parsing differed slightly from drv filename
	prefix := pkg.Name + "-"
	for nameVer, p := range pathMap {
		if strings.HasPrefix(nameVer, prefix) && strings.Contains(nameVer, pkg.Version) {
			return p
		}
	}

	return ""
}

var (
	// reRedPositiveSize matches ANSI_RED before digits, which indicates positive size changes.
	reRedPositiveSize = regexp.MustCompile(`\x1b\[31;1m([0-9])`)
	// reANSI matches ANSI escape sequences like color codes.
	reANSI = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
	// reStorePath matches /nix/store/<hash>-<name-version> and captures <name-version>.
	reStorePath = regexp.MustCompile(`/nix/store/[^/\s-]+-([^\s]+)`)
)

// normalizeDiffOutput strips ANSI escape codes and adds '+' to positive sizes.
//
// Why not pass NO_COLOR=1 to nix?
// Nix's diff-closures prints ANSI color codes to stdout directly, and ignores NO_COLOR.
//
// Why not just strip ANSI codes?
// Nix marks positive size changes with red color instead of a plus sign.
// Because GitHub markdown code blocks cannot show colors, this adds '+'
// to red positive sizes, and then strips all ANSI escape codes.
func normalizeDiffOutput(s string) string {
	s = reRedPositiveSize.ReplaceAllString(s, "+$1")
	return reANSI.ReplaceAllString(s, "")
}

// simplifyWhyDependsOutput strips ANSI codes, /nix/store/<hash>- prefixes, and .drv extensions.
//
// Why simplify store paths:
// Raw 32-character hashes make up 80% of line width and obscure dependency relationships.
// Showing package names and versions (pname-version) makes it easy to understand why
// a package is pulled in without hash noise.
func simplifyWhyDependsOutput(s string) string {
	s = reANSI.ReplaceAllString(s, "")

	s = reStorePath.ReplaceAllStringFunc(s, func(match string) string {
		sub := reStorePath.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		name := sub[1]
		return strings.TrimSuffix(name, ".drv")
	})

	var lines []string
	for _, line := range strings.Split(s, "\n") {
		lines = append(lines, strings.TrimRight(line, " \t\r"))
	}

	return strings.TrimSpace(strings.Join(lines, "\n"))
}
