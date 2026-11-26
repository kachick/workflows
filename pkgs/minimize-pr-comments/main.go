package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/kachick/workflows/internal/gh"
	"github.com/kachick/workflows/internal/workflowmeta"
)

func parseRepoEnv() (string, string) {
	repoFull := os.Getenv("GITHUB_REPOSITORY")
	owner, repo, ok := strings.Cut(repoFull, "/")
	if ok && owner != "" && repo != "" {
		return owner, repo
	}
	return os.Getenv("GITHUB_REPOSITORY_OWNER"), ""
}

func defaultTokenEnv() string {
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return token
	}
	return os.Getenv("GH_TOKEN")
}

func run(args []string, stdout, stderr io.Writer) int {
	if err := workflowmeta.VerifyEnv(); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	defaultOwner, defaultRepo := parseRepoEnv()
	defaultPRStr := os.Getenv("PR_NUMBER")
	defaultPR := 0
	if defaultPRStr != "" {
		n, err := strconv.Atoi(defaultPRStr)
		if err != nil {
			fmt.Fprintf(stderr, "Error: invalid PR_NUMBER %q: %v\n", defaultPRStr, err)
			return 1
		}
		defaultPR = n
	}

	fs := flag.NewFlagSet("minimize-pr-comments", flag.ContinueOnError)
	fs.SetOutput(stderr)
	owner := fs.String("owner", defaultOwner, "GitHub repository owner")
	repo := fs.String("repo", defaultRepo, "GitHub repository name")
	pr := fs.Int("pr", defaultPR, "Target pull request number")
	marker := fs.String("marker", "", "Substring pattern to match in outdated bot comments")
	token := fs.String("token", defaultTokenEnv(), "GitHub access token")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if *marker == "" {
		fmt.Fprintln(stderr, "Error: --marker is required")
		return 1
	}
	if *owner == "" || *repo == "" {
		fmt.Fprintln(stderr, "Error: --owner and --repo are required (or set GITHUB_REPOSITORY)")
		return 1
	}
	if *pr <= 0 {
		fmt.Fprintln(stderr, "Error: valid --pr number is required (or set PR_NUMBER)")
		return 1
	}
	if *token == "" {
		fmt.Fprintln(stderr, "Error: GitHub token is required (set GITHUB_TOKEN/GH_TOKEN or pass --token)")
		return 1
	}

	ctx := context.Background()
	client := gh.NewClient(*token)

	count, err := client.MinimizeOutdatedComments(ctx, *owner, *repo, *pr, *marker)
	if err != nil {
		if errors.Is(err, gh.ErrPermissionDenied) {
			fmt.Fprintln(stderr, "::warning::GitHub token lacks permission to list/minimize comments. Continuing without comment minimize.")
			return 0
		}
		fmt.Fprintf(stderr, "Error: failed to minimize comments: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "Minimized %d comment(s) matching %q\n", count, *marker)
	return 0
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
