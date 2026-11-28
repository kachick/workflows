package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kachick/workflows/internal/gh"
	"github.com/kachick/workflows/internal/leakcheck"
	"github.com/kachick/workflows/internal/workflowmeta"
)

const maxErrorBodyBytes = 4096

type packageVersion struct {
	ID        int64     `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	Metadata  struct {
		Container struct {
			Tags []string `json:"tags"`
		} `json:"container"`
	} `json:"metadata"`
}

type ghcrClient struct {
	Token      string
	APIBaseURL string
	HTTPClient *http.Client
}

func newGHCRClient(token string) *ghcrClient {
	apiBase := os.Getenv("GITHUB_API_URL")
	if apiBase == "" {
		apiBase = "https://api.github.com"
	}
	return &ghcrClient{
		Token:      token,
		APIBaseURL: strings.TrimRight(apiBase, "/"),
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *ghcrClient) setHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
}

func (c *ghcrClient) ListVersions(ctx context.Context, owner, pkgName string) ([]packageVersion, error) {
	var all []packageVersion
	for page := 1; ; page++ {
		endpoint := fmt.Sprintf("%s/users/%s/packages/container/%s/versions?per_page=100&page=%d",
			c.APIBaseURL, url.PathEscape(owner), url.PathEscape(pkgName), page)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		c.setHeaders(req)

		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
			resp.Body.Close()
			return nil, fmt.Errorf("list package versions failed (%d): %s", resp.StatusCode, string(body))
		}

		var batch []packageVersion
		err = json.UnmarshalRead(resp.Body, &batch)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("decode package versions: %w", err)
		}
		all = append(all, batch...)
		if len(batch) < 100 {
			break
		}
	}
	return all, nil
}

func (c *ghcrClient) DeleteVersion(ctx context.Context, owner, pkgName string, versionID int64) error {
	endpoint := fmt.Sprintf("%s/users/%s/packages/container/%s/versions/%d",
		c.APIBaseURL, url.PathEscape(owner), url.PathEscape(pkgName), versionID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	c.setHeaders(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		return fmt.Errorf("delete package version %d failed (%d): %s", versionID, resp.StatusCode, string(body))
	}
	return nil
}

func validateOwner(owner string) error {
	if owner == "" || strings.ContainsAny(owner, "/\\ \t\r\n") || strings.Contains(owner, "..") {
		return fmt.Errorf("invalid owner %q", owner)
	}
	return nil
}

func validatePackageName(pkg string) error {
	if pkg == "" || strings.ContainsAny(pkg, "\\ \t\r\n") || strings.Contains(pkg, "..") {
		return fmt.Errorf("invalid package name %q", pkg)
	}
	return nil
}

// selectPruneTargets returns version IDs to delete, keeping the newest keepLatest items
// and capping deletions to maxDelete per run.
func selectPruneTargets(versions []packageVersion, keepLatest int, maxDelete int) []int64 {
	if len(versions) <= keepLatest || keepLatest <= 0 || maxDelete <= 0 {
		return nil
	}
	sorted := make([]packageVersion, len(versions))
	copy(sorted, versions)
	sort.Slice(sorted, func(i, j int) bool {
		if !sorted[i].CreatedAt.Equal(sorted[j].CreatedAt) {
			return sorted[i].CreatedAt.Before(sorted[j].CreatedAt)
		}
		return sorted[i].ID < sorted[j].ID
	})

	deleteCount := min(len(sorted)-keepLatest, maxDelete)
	ids := make([]int64, 0, deleteCount)
	for i := range deleteCount {
		ids = append(ids, sorted[i].ID)
	}
	return ids
}

// selectStagingTargets returns deduplicated version IDs whose tags include "pr-<prNumber>-*".
func selectStagingTargets(versions []packageVersion, prNumber int) []int64 {
	if prNumber <= 0 {
		return nil
	}
	prefix := fmt.Sprintf("pr-%d-", prNumber)
	seen := make(map[int64]bool)
	var ids []int64
	for _, v := range versions {
		if seen[v.ID] {
			continue
		}
		for _, tag := range v.Metadata.Container.Tags {
			if strings.HasPrefix(tag, prefix) {
				seen[v.ID] = true
				ids = append(ids, v.ID)
				break
			}
		}
	}
	return ids
}

func formatStagingComment(deletedIDs []int64, meta workflowmeta.Metadata) string {
	idStrs := make([]string, len(deletedIDs))
	for i, id := range deletedIDs {
		idStrs[i] = strconv.FormatInt(id, 10)
	}
	return fmt.Sprintf("🤖 removed 🗑️ staging ⬢ from ghcr.io\n\n```plaintext\n%s\n```\n\n%s",
		strings.Join(idStrs, ","), meta.ProvenanceText("Cleaned up"))
}

type config struct {
	mode        string
	owner       string
	packageName string
	keepLatest  int
	maxDelete   int
	prNumber    int
	repo        string
	postComment bool
	token       string
}

func defaultTokenEnv() string {
	if t := os.Getenv("GITHUB_TOKEN"); t != "" {
		return t
	}
	return os.Getenv("GH_TOKEN")
}

func defaultOwnerEnv() string {
	if o := os.Getenv("REPOSITORY_OWNER"); o != "" {
		return o
	}
	return os.Getenv("GITHUB_REPOSITORY_OWNER")
}

func parseConfig(args []string) (config, error) {
	var cfg config
	fs := flag.NewFlagSet("cleanup-ghcr", flag.ContinueOnError)
	fs.StringVar(&cfg.mode, "mode", "", "Cleanup mode: prune or staging")
	fs.StringVar(&cfg.owner, "owner", "", "GitHub user/owner of the GHCR package")
	fs.StringVar(&cfg.packageName, "package", "", "GHCR container package name")
	fs.IntVar(&cfg.keepLatest, "keep-latest", 50, "Number of newest versions to keep (prune mode)")
	fs.IntVar(&cfg.maxDelete, "max-delete", 100, "Maximum versions to delete per run (prune mode)")
	fs.IntVar(&cfg.prNumber, "pr", 0, "Pull request number (staging mode)")
	fs.StringVar(&cfg.repo, "repo", "", "Repository in owner/repo format for PR comment (staging mode)")
	fs.BoolVar(&cfg.postComment, "post-comment", true, "Post PR comment after deleting staging versions")
	fs.StringVar(&cfg.token, "token", "", "GitHub access token")

	if err := fs.Parse(args); err != nil {
		return cfg, err
	}

	explicit := make(map[string]bool)
	fs.Visit(func(f *flag.Flag) {
		explicit[f.Name] = true
	})

	if !explicit["owner"] {
		cfg.owner = defaultOwnerEnv()
	}
	if !explicit["package"] {
		cfg.packageName = os.Getenv("PACKAGE_NAME")
	}
	if !explicit["keep-latest"] {
		if env := strings.TrimSpace(os.Getenv("KEEP_LATEST_ITEMS")); env != "" {
			n, err := strconv.Atoi(env)
			if err != nil {
				return cfg, fmt.Errorf("invalid KEEP_LATEST_ITEMS %q: %w", env, err)
			}
			cfg.keepLatest = n
		}
	}
	if !explicit["pr"] {
		if env := strings.TrimSpace(os.Getenv("PR_NUMBER")); env != "" {
			n, err := strconv.Atoi(env)
			if err != nil {
				return cfg, fmt.Errorf("invalid PR_NUMBER %q: %w", env, err)
			}
			cfg.prNumber = n
		}
	}
	if !explicit["repo"] {
		cfg.repo = os.Getenv("GITHUB_REPOSITORY")
	}
	if !explicit["token"] {
		cfg.token = defaultTokenEnv()
	}

	cfg.owner = strings.TrimSpace(cfg.owner)
	cfg.packageName = strings.TrimSpace(cfg.packageName)

	if cfg.mode != "prune" && cfg.mode != "staging" {
		return cfg, fmt.Errorf("invalid --mode %q (must be prune or staging)", cfg.mode)
	}
	if err := validateOwner(cfg.owner); err != nil {
		return cfg, err
	}
	if err := validatePackageName(cfg.packageName); err != nil {
		return cfg, err
	}
	if cfg.token == "" {
		return cfg, errors.New("GitHub token is required (set GITHUB_TOKEN/GH_TOKEN or pass --token)")
	}
	if cfg.mode == "prune" {
		if cfg.keepLatest < 1 {
			return cfg, fmt.Errorf("keep-latest must be >= 1, got %d", cfg.keepLatest)
		}
		if cfg.maxDelete < 1 {
			return cfg, fmt.Errorf("max-delete must be >= 1, got %d", cfg.maxDelete)
		}
	}
	if cfg.mode == "staging" {
		if cfg.prNumber < 1 {
			return cfg, fmt.Errorf("pr must be a positive integer, got %d", cfg.prNumber)
		}
		if cfg.postComment {
			ownerPart, repoPart, ok := strings.Cut(cfg.repo, "/")
			if !ok || ownerPart == "" || repoPart == "" || strings.Contains(repoPart, "/") {
				return cfg, fmt.Errorf("invalid repository %q (expected owner/repo)", cfg.repo)
			}
		}
	}

	return cfg, nil
}

func execute(ctx context.Context, cfg config, pkgClient *ghcrClient, commentClient *gh.Client, checker leakcheck.Checker, meta workflowmeta.Metadata, stdout io.Writer) error {
	versions, err := pkgClient.ListVersions(ctx, cfg.owner, cfg.packageName)
	if err != nil {
		return err
	}

	var targetIDs []int64
	switch cfg.mode {
	case "prune":
		targetIDs = selectPruneTargets(versions, cfg.keepLatest, cfg.maxDelete)
	case "staging":
		targetIDs = selectStagingTargets(versions, cfg.prNumber)
	}

	if len(targetIDs) == 0 {
		fmt.Fprintln(stdout, "No matching GHCR package versions to delete.")
		return nil
	}

	for _, id := range targetIDs {
		if err := pkgClient.DeleteVersion(ctx, cfg.owner, cfg.packageName, id); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Deleted package version %d\n", id)
	}

	if cfg.mode == "staging" && cfg.postComment {
		body := formatStagingComment(targetIDs, meta)
		if err := checker.Check(ctx, body); err != nil {
			return fmt.Errorf("pre-flight leak check failed for PR comment: %w", err)
		}
		ownerPart, repoPart, _ := strings.Cut(cfg.repo, "/")
		if err := commentClient.PostComment(ctx, ownerPart, repoPart, cfg.prNumber, body); err != nil {
			return fmt.Errorf("failed to post PR comment: %w", err)
		}
	}

	return nil
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if err := workflowmeta.VerifyEnv(); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	cfg, err := parseConfig(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	pkgClient := newGHCRClient(cfg.token)
	commentClient := gh.NewClient(cfg.token)
	checker := &leakcheck.BetterleaksChecker{}
	if err := execute(ctx, cfg, pkgClient, commentClient, checker, workflowmeta.Get(), stdout); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	return 0
}

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}
