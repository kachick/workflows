package osv

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
		if len(r.Vulns) > 0 {
			affected = append(affected, PackageVulnerabilities{
				Package:         pkgs[i],
				Vulnerabilities: r.Vulns,
			})
		}
	}

	return affected, nil
}
