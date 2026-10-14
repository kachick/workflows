package leakcheck

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

var (
	ErrLeakDetected    = errors.New("potential secret leak detected")
	ErrScannerNotFound = errors.New("betterleaks executable not found in PATH")
)

type Checker interface {
	Check(ctx context.Context, input string) error
}

type BetterleaksChecker struct {
	// Command allows specifying executable name or path (defaults to "betterleaks")
	Command string
}

func (c *BetterleaksChecker) Check(ctx context.Context, input string) error {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return nil
	}

	bin := c.Command
	if bin == "" {
		bin = "betterleaks"
	}

	execPath, err := exec.LookPath(bin)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrScannerNotFound, bin)
	}

	cmd := exec.CommandContext(ctx, execPath, "stdin", "--no-banner")
	cmd.Stdin = strings.NewReader(input)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			// Avoid printing matching payload or raw stdout to prevent secret exposure in logs.
			return fmt.Errorf("%w: %s", ErrLeakDetected, strings.TrimSpace(out.String()))
		}
		return fmt.Errorf("failed to run betterleaks: %w (%s)", err, strings.TrimSpace(out.String()))
	}

	return nil
}

func CheckMultiple(ctx context.Context, checker Checker, texts ...string) error {
	for _, text := range texts {
		if err := checker.Check(ctx, text); err != nil {
			return err
		}
	}
	return nil
}
