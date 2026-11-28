package main

import (
	"encoding/json/v2"
	"fmt"
	"net/url"
	"slices"
	"strings"
)

type flakeLock struct {
	Nodes map[string]flakeLockNode `json:"nodes"`
}

type flakeLockNode struct {
	Locked *flakeLockRef `json:"locked"`
}

type flakeLockRef struct {
	Type  string `json:"type"`
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
	Host  string `json:"host"`
	Rev   string `json:"rev"`
	URL   string `json:"url"`
}

type flakeInputUpdate struct {
	Node   string
	Owner  string
	Repo   string
	OldRev string
	NewRev string
}

func (u flakeInputUpdate) CompareURL() string {
	return fmt.Sprintf("https://github.com/%s/%s/compare/%s...%s", u.Owner, u.Repo, u.OldRev, u.NewRev)
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func isValidCommitSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	for i := range len(s) {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func isValidGitHubSegment(s string) bool {
	if s == "" || s == "." || s == ".." {
		return false
	}
	for i := range len(s) {
		c := s[i]
		isAlphaNum := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
		if !isAlphaNum && c != '-' && c != '_' && c != '.' {
			return false
		}
	}
	return true
}

func isNixpkgsReleaseTarball(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if u.Scheme != "https" || !strings.EqualFold(u.Host, "releases.nixos.org") {
		return false
	}
	return strings.HasPrefix(u.Path, "/nixpkgs/") || strings.HasPrefix(u.Path, "/nixos/")
}

// resolveGitHubRef maps a locked flake input to a github.com repository and commit SHA.
//
// Why not support non-GitHub forges or arbitrary tarballs:
// Forge compare URL formats differ across providers, and arbitrary tarball URLs cannot be mapped
// reliably to a GitHub repository. Only github.com inputs and official NixOS/nixpkgs channel tarballs
// (resolved to releases.nixos.org with a commit rev) are included; everything else is omitted.
func resolveGitHubRef(ref *flakeLockRef) (owner, repo, rev string, ok bool) {
	if ref == nil || !isValidCommitSHA(ref.Rev) {
		return "", "", "", false
	}

	switch ref.Type {
	case "github":
		if ref.Host != "" && !strings.EqualFold(ref.Host, "github.com") {
			return "", "", "", false
		}
		if !isValidGitHubSegment(ref.Owner) || !isValidGitHubSegment(ref.Repo) {
			return "", "", "", false
		}
		return ref.Owner, ref.Repo, ref.Rev, true

	case "git":
		u, err := url.Parse(ref.URL)
		if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Host, "github.com") || u.RawQuery != "" || u.Fragment != "" {
			return "", "", "", false
		}
		trimmed := strings.Trim(u.Path, "/")
		parts := strings.Split(trimmed, "/")
		if len(parts) != 2 {
			return "", "", "", false
		}
		repoName := strings.TrimSuffix(parts[1], ".git")
		if !isValidGitHubSegment(parts[0]) || !isValidGitHubSegment(repoName) {
			return "", "", "", false
		}
		return parts[0], repoName, ref.Rev, true

	case "tarball":
		if !isNixpkgsReleaseTarball(ref.URL) {
			return "", "", "", false
		}
		return "NixOS", "nixpkgs", ref.Rev, true

	default:
		return "", "", "", false
	}
}

func diffFlakeLocks(oldData, newData []byte) ([]flakeInputUpdate, error) {
	var oldLock, newLock flakeLock
	if err := json.Unmarshal(oldData, &oldLock); err != nil {
		return nil, fmt.Errorf("parse old flake.lock: %w", err)
	}
	if err := json.Unmarshal(newData, &newLock); err != nil {
		return nil, fmt.Errorf("parse new flake.lock: %w", err)
	}

	var updates []flakeInputUpdate
	for name, newNode := range newLock.Nodes {
		oldNode, exists := oldLock.Nodes[name]
		if !exists {
			continue
		}
		oldOwner, oldRepo, oldRev, oldOK := resolveGitHubRef(oldNode.Locked)
		newOwner, newRepo, newRev, newOK := resolveGitHubRef(newNode.Locked)
		if !oldOK || !newOK {
			continue
		}
		if !strings.EqualFold(oldOwner, newOwner) || !strings.EqualFold(oldRepo, newRepo) {
			continue
		}
		if oldRev == newRev {
			continue
		}
		updates = append(updates, flakeInputUpdate{
			Node:   name,
			Owner:  newOwner,
			Repo:   newRepo,
			OldRev: oldRev,
			NewRev: newRev,
		})
	}

	slices.SortFunc(updates, func(a, b flakeInputUpdate) int {
		return strings.Compare(a.Node, b.Node)
	})

	return updates, nil
}

func inlineCode(s string) string {
	ticks := "`"
	for strings.Contains(s, ticks) {
		ticks += "`"
	}
	if strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") {
		return ticks + " " + s + " " + ticks
	}
	return ticks + s + ticks
}

func formatFlakeLockUpdates(updates []flakeInputUpdate) string {
	if len(updates) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("### Updated `flake.lock` inputs\n\n")
	for _, u := range updates {
		fmt.Fprintf(&sb, "- %s (%s): [%s](%s)\n",
			inlineCode(u.Node),
			inlineCode(u.Owner+"/"+u.Repo),
			inlineCode(shortSHA(u.OldRev)+"..."+shortSHA(u.NewRev)),
			u.CompareURL(),
		)
	}
	return strings.TrimRight(sb.String(), "\n")
}
