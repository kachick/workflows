package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/kachick/workflows/internal/gh"
)

func parseRepoEnv() (string, string) {
	repoFull := os.Getenv("GITHUB_REPOSITORY")
	parts := strings.SplitN(repoFull, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return os.Getenv("GITHUB_REPOSITORY_OWNER"), ""
}

func main() {
	defaultOwner, defaultRepo := parseRepoEnv()
	defaultPRStr := os.Getenv("PR_NUMBER")
	defaultPR := 0
	if defaultPRStr != "" {
		if n, err := strconv.Atoi(defaultPRStr); err == nil {
			defaultPR = n
		}
	}

	owner := flag.String("owner", defaultOwner, "GitHub repository owner")
	repo := flag.String("repo", defaultRepo, "GitHub repository name")
	pr := flag.Int("pr", defaultPR, "Target pull request number")
	marker := flag.String("marker", "", "Substring pattern to match in outdated bot comments")
	token := flag.String("token", os.Getenv("GITHUB_TOKEN"), "GitHub access token")
	flag.Parse()

	if *marker == "" {
		fmt.Fprintln(os.Stderr, "Error: --marker is required")
		os.Exit(1)
	}
	if *owner == "" || *repo == "" {
		fmt.Fprintln(os.Stderr, "Error: --owner and --repo are required (or set GITHUB_REPOSITORY)")
		os.Exit(1)
	}
	if *pr <= 0 {
		fmt.Fprintln(os.Stderr, "Error: valid --pr number is required (or set PR_NUMBER)")
		os.Exit(1)
	}
	if *token == "" {
		fmt.Fprintln(os.Stderr, "Error: GitHub token is required (set GITHUB_TOKEN or pass --token)")
		os.Exit(1)
	}

	ctx := context.Background()
	client := gh.NewClient(*token)

	count, err := client.MinimizeOutdatedComments(ctx, *owner, *repo, *pr, *marker)
	if err != nil {
		if errors.Is(err, gh.ErrPermissionDenied) {
			fmt.Fprintln(os.Stderr, "::warning::GitHub token lacks permission to list/minimize comments. Continuing without comment minimize.")
			return
		}
		fmt.Fprintf(os.Stderr, "Error: failed to minimize comments: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stdout, "Minimized %d comment(s) matching %q\n", count, *marker)
}
