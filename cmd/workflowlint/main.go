package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/kachick/workflows/cmd/workflowlint/internal/workflowlint"
)

func main() {
	repoRoot := flag.String("repo-root", ".", "Path to the repository root directory")
	flag.Parse()

	failed := false

	missing, stale, err := workflowlint.Check(*repoRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "workflowlint: %v\n", err)
		os.Exit(2)
	}

	if len(missing) > 0 {
		fmt.Fprintln(os.Stderr, "workflowlint: missing reusable workflows in documentation:")
		for _, name := range missing {
			fmt.Fprintf(os.Stderr, "  - %s (.github/workflows/reusable-%s.yml)\n", name, name)
		}
		failed = true
	}

	if len(stale) > 0 {
		fmt.Fprintln(os.Stderr, "workflowlint: stale reusable workflows in documentation (no matching workflow file):")
		for _, name := range stale {
			fmt.Fprintf(os.Stderr, "  - %s\n", name)
		}
		failed = true
	}

	if err := workflowlint.CheckNixInstallerSteps(*repoRoot); err != nil {
		fmt.Fprintf(os.Stderr, "workflowlint: %v\n", err)
		failed = true
	}

	if err := workflowlint.CheckReusableJobGuards(*repoRoot); err != nil {
		fmt.Fprintf(os.Stderr, "workflowlint: %v\n", err)
		failed = true
	}

	if failed {
		os.Exit(1)
	}
}
