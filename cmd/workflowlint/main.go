package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/kachick/workflows/internal/workflowlint"
)

func main() {
	repoRoot := flag.String("repo-root", ".", "Path to the repository root directory")
	flag.Parse()

	missing, err := workflowlint.Check(*repoRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "workflowlint: %v\n", err)
		os.Exit(2)
	}

	if len(missing) > 0 {
		fmt.Fprintln(os.Stderr, "workflowlint: missing reusable workflows in documentation:")
		for _, name := range missing {
			fmt.Fprintf(os.Stderr, "  - %s (.github/workflows/reusable-%s.yml)\n", name, name)
		}
		os.Exit(1)
	}

	if err := workflowlint.CheckNixInstallerSteps(*repoRoot); err != nil {
		fmt.Fprintf(os.Stderr, "workflowlint: %v\n", err)
		os.Exit(1)
	}

	if err := workflowlint.CheckReusableJobGuards(*repoRoot); err != nil {
		fmt.Fprintf(os.Stderr, "workflowlint: %v\n", err)
		os.Exit(1)
	}
}
