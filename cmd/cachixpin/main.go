package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/kachick/workflows/cmd/cachixpin/internal/cachixpin"
)

func main() {
	cache := flag.String("cache", "kachick", "Target Cachix cache name")
	pin := flag.String("pin", "workflows", "Cachix pin name")
	keepRevisions := flag.Int("keep-revisions", 10, "Number of revisions to keep")
	repoRoot := flag.String("repo-root", ".", "Path to the repository root directory")
	dryRun := flag.Bool("dry-run", false, "Print commands without pushing or pinning")
	flag.Parse()

	opts := cachixpin.Options{
		Cache:         *cache,
		Pin:           *pin,
		KeepRevisions: *keepRevisions,
		RepoRoot:      *repoRoot,
		DryRun:        *dryRun,
	}

	if err := cachixpin.Run(context.Background(), opts); err != nil {
		fmt.Fprintf(os.Stderr, "cachixpin: %v\n", err)
		os.Exit(1)
	}
}
