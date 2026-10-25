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
		filtered := FilterDistroAdvisories(r.Vulns)
		if len(filtered) > 0 {
			affected = append(affected, PackageVulnerabilities{
				Package:         pkgs[i],
				Vulnerabilities: filtered,
			})
		}
	}

	return affected, nil
}

// distroAdvisoryPrefixes lists advisory ID prefixes from Linux distributions.
// Why not include distro trackers:
// Nix builds packages from upstream source versions without distro epoch numbers.
// Distribution trackers (Debian, Ubuntu, Red Hat, SUSE, Alpine) use package versions
// with their own epoch and revision numbers (for example, dpkg "2:1.6.2-...").
// When OSV compares upstream versions against distro feeds using dpkg/rpm rules,
// upstream packages (such as libx11 1.8.14) falsely match against old fixed vulnerabilities
// because epoch 0 is treated as older than epoch 2.
//
// Known problems / FIXME:
//  1. OSV querybatch does not support filtering by ecosystem without per-package ecosystem requests.
//     If OSV adds ecosystem filtering to batch queries, consider querying only desired ecosystems.
//  2. If ancient false positives still appear from generic CVE feeds, consider filtering by CVE year
//     (Approach B: dropping CVEs from many years before the package release).
var distroAdvisoryPrefixes = []string{
	"DEBIAN-",
	"DLA-",
	"DSA-",
	"UBUNTU-",
	"USN-",
	"ALMA-",
	"RLSA-",
	"RHSA-",
	"RHBA-",
	"RHEA-",
	"SUSE-",
	"OPENSUSE-",
	"ALPINE-",
}

// IsDistroAdvisoryID reports whether the advisory ID comes from a Linux distribution security tracker.
func IsDistroAdvisoryID(id string) bool {
	upper := strings.ToUpper(id)
	for _, prefix := range distroAdvisoryPrefixes {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}
	return false
}

// FilterDistroAdvisories returns a new slice omitting advisories from Linux distribution trackers.
func FilterDistroAdvisories(vulns []Vulnerability) []Vulnerability {
	if len(vulns) == 0 {
		return nil
	}
	filtered := make([]Vulnerability, 0, len(vulns))
	for _, v := range vulns {
		if !IsDistroAdvisoryID(v.ID) {
			filtered = append(filtered, v)
		}
	}
	return filtered
}
