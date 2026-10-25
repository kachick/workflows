package osv

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultEndpoint is the public OSV batch query endpoint.
const DefaultEndpoint = "https://api.osv.dev/v1/querybatch"

// Package represents an updated or newly added package to query in OSV.
type Package struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Vulnerability represents an advisory ID returned by OSV.
type Vulnerability struct {
	ID       string `json:"id"`
	Modified string `json:"modified,omitempty"`
}

// PackageVulnerabilities represents an affected package and its detected vulnerabilities.
type PackageVulnerabilities struct {
	Package         Package
	Vulnerabilities []Vulnerability
}

type batchQueryRequest struct {
	Queries []queryItem `json:"queries"`
}

type queryItem struct {
	Package packageItem `json:"package"`
	Version string      `json:"version"`
}

type packageItem struct {
	Name string `json:"name"`
}

type batchQueryResponse struct {
	Results []queryResult `json:"results"`
}

type queryResult struct {
	Vulns []Vulnerability `json:"vulns"`
}

// Client queries the OSV API.
type Client struct {
	Endpoint   string
	HTTPClient *http.Client
}

// NewClient returns a new OSV API client.
func NewClient(endpoint string) *Client {
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	return &Client{
		Endpoint: endpoint,
		HTTPClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// QueryBatch queries OSV for vulnerabilities affecting the specified packages in a single batch request.
func (c *Client) QueryBatch(ctx context.Context, pkgs []Package) ([]PackageVulnerabilities, error) {
	if len(pkgs) == 0 {
		return nil, nil
	}

	reqBody := batchQueryRequest{
		Queries: make([]queryItem, 0, len(pkgs)),
	}
	for _, p := range pkgs {
		reqBody.Queries = append(reqBody.Queries, queryItem{
			Package: packageItem{Name: p.Name},
			Version: p.Version,
		})
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to encode OSV request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create OSV request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("OSV request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("OSV API returned status %d: %s", resp.StatusCode, string(body))
	}

	var res batchQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("failed to decode OSV response: %w", err)
	}

	if len(res.Results) != len(pkgs) {
		return nil, fmt.Errorf("OSV response count mismatch: expected %d, got %d", len(pkgs), len(res.Results))
	}

	var affected []PackageVulnerabilities
	for i, r := range res.Results {
		filtered := FilterAllowedAdvisories(r.Vulns)
		if len(filtered) > 0 {
			affected = append(affected, PackageVulnerabilities{
				Package:         pkgs[i],
				Vulnerabilities: filtered,
			})
		}
	}

	return affected, nil
}

// allowedAdvisoryPrefixes defines advisory ID prefixes that are trusted for Nix derivations.
//
// Why opt-in instead of opt-out:
// At first glance, opt-in looks risky because it drops unfamiliar advisory feeds.
// However, an opt-out blacklist is impossible to maintain and fails in practice for Nix:
//  1. Linux distro feeds (Debian, Ubuntu, Red Hat, Echo, Mageia, openEuler, etc.)
//     use package versions with distro-specific epochs and revisions (for example, dpkg "2:1.6.2-...").
//     Nix evaluates upstream source tarballs (epoch 0).
//     OSV evaluates distro feeds using dpkg/rpm rules, so modern upstream packages (like libx11 1.8.14)
//     falsely match decade-old fixed vulnerabilities (epoch 0 < epoch 2).
//     OSV continually adds new distro feeds (ECHO, BELL, ROOT-OS, CLSA, ALSA), making blacklists futile.
//  2. OSV batch queries search by package name alone without an ecosystem.
//     Nix packages frequently collide with unrelated packages in other ecosystems.
//     For example, GNU gcc matched RUSTSEC-2025-0121 (an obsolete crates.io crate named "gcc"),
//     and pcre2 matched PYSEC (a Python package named "pcre2").
//
// Until OSV supports a first-class Nix/NixOS ecosystem or ecosystem-constrained batch queries,
// we strictly allow only generic and ecosystem-audited feeds (GHSA, CVE, OSV).
// Real-world package vulnerabilities (such as Electron and pnpm CVEs/advisories) arrive via GHSA and CVE.
var allowedAdvisoryPrefixes = []string{
	"GHSA-",
	"CVE-",
	"OSV-",
}

// IsAllowedAdvisoryID reports whether the advisory ID comes from a trusted general advisory database.
func IsAllowedAdvisoryID(id string) bool {
	upper := strings.ToUpper(id)
	for _, prefix := range allowedAdvisoryPrefixes {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}
	return false
}

// FilterAllowedAdvisories returns a new slice retaining only advisories with allowed prefixes.
func FilterAllowedAdvisories(vulns []Vulnerability) []Vulnerability {
	if len(vulns) == 0 {
		return nil
	}
	filtered := make([]Vulnerability, 0, len(vulns))
	for _, v := range vulns {
		if IsAllowedAdvisoryID(v.ID) {
			filtered = append(filtered, v)
		}
	}
	return filtered
}
