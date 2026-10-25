package osv

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestQueryBatch(t *testing.T) {
	t.Run("returns empty when pkgs is empty", func(t *testing.T) {
		client := NewClient("http://dummy")
		results, err := client.QueryBatch(context.Background(), nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) != 0 {
			t.Fatalf("expected 0 results, got %d", len(results))
		}
	})

	t.Run("successfully queries OSV and filters affected packages", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Fatalf("expected POST, got %s", r.Method)
			}
			if r.Header.Get("Content-Type") != "application/json" {
				t.Fatalf("expected application/json, got %s", r.Header.Get("Content-Type"))
			}

			var req batchQueryRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("failed to decode request: %v", err)
			}

			if len(req.Queries) != 2 {
				t.Fatalf("expected 2 queries, got %d", len(req.Queries))
			}

			res := batchQueryResponse{
				Results: []queryResult{
					{
						Vulns: []Vulnerability{
							{ID: "GHSA-1234"},
							{ID: "CVE-2026-9999"},
						},
					},
					{
						Vulns: nil, // clean package
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(res)
		}))
		defer ts.Close()

		client := NewClient(ts.URL)
		pkgs := []Package{
			{Name: "vulnerable-pkg", Version: "1.0.0"},
			{Name: "clean-pkg", Version: "2.0.0"},
		}

		results, err := client.QueryBatch(context.Background(), pkgs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expected := []PackageVulnerabilities{
			{
				Package: Package{Name: "vulnerable-pkg", Version: "1.0.0"},
				Vulnerabilities: []Vulnerability{
					{ID: "GHSA-1234"},
					{ID: "CVE-2026-9999"},
				},
			},
		}

		if !reflect.DeepEqual(results, expected) {
			t.Fatalf("expected %#v, got %#v", expected, results)
		}
	})

	t.Run("retains only allowed advisories", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			res := batchQueryResponse{
				Results: []queryResult{
					{
						// Package with only distro and echo advisories should be considered clean
						Vulns: []Vulnerability{
							{ID: "DEBIAN-CVE-2016-7942"},
							{ID: "UBUNTU-CVE-2016-7942"},
							{ID: "ECHO-4c1a-f6d4-292c"},
							{ID: "ROOT-OS-DEBIAN-12-CVE-2026-88806"},
						},
					},
					{
						// Package with cross-ecosystem advisory (e.g., Rust gcc crate on GNU gcc) should be clean
						Vulns: []Vulnerability{
							{ID: "RUSTSEC-2025-0121"},
						},
					},
					{
						// Package with mixed advisories should keep allowed advisories
						Vulns: []Vulnerability{
							{ID: "DSA-1234"},
							{ID: "GHSA-5678"},
							{ID: "RHSA-2021-9999"},
							{ID: "CVE-2024-1111"},
							{ID: "OSV-2024-0001"},
							{ID: "PYSEC-2024-123"},
						},
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(res)
		}))
		defer ts.Close()

		client := NewClient(ts.URL)
		pkgs := []Package{
			{Name: "libx11", Version: "1.8.14"},
			{Name: "gcc", Version: "16.2.0"},
			{Name: "app", Version: "2.0.0"},
		}

		results, err := client.QueryBatch(context.Background(), pkgs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expected := []PackageVulnerabilities{
			{
				Package: Package{Name: "app", Version: "2.0.0"},
				Vulnerabilities: []Vulnerability{
					{ID: "GHSA-5678"},
					{ID: "CVE-2024-1111"},
					{ID: "OSV-2024-0001"},
				},
			},
		}

		if !reflect.DeepEqual(results, expected) {
			t.Fatalf("expected %#v, got %#v", expected, results)
		}
	})

	t.Run("handles HTTP error status", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "rate limited", http.StatusTooManyRequests)
		}))
		defer ts.Close()

		client := NewClient(ts.URL)
		pkgs := []Package{
			{Name: "pkg", Version: "1.0.0"},
		}

		_, err := client.QueryBatch(context.Background(), pkgs)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestIsAllowedAdvisoryID(t *testing.T) {
	tests := []struct {
		id      string
		allowed bool
	}{
		// Allowed prefixes
		{id: "GHSA-1234-5678-90ab", allowed: true},
		{id: "ghsa-1234-5678-90ab", allowed: true},
		{id: "CVE-2024-12345", allowed: true},
		{id: "cve-2024-12345", allowed: true},
		{id: "OSV-2024-999", allowed: true},
		{id: "osv-2024-999", allowed: true},

		// Linux distro feeds must be excluded
		{id: "DEBIAN-CVE-2016-7942", allowed: false},
		{id: "DLA-1234-1", allowed: false},
		{id: "DSA-4321-1", allowed: false},
		{id: "UBUNTU-CVE-2016-7942", allowed: false},
		{id: "USN-3093-1", allowed: false},
		{id: "ALMA-2023-1111", allowed: false},
		{id: "RLSA-2023-2222", allowed: false},
		{id: "RHSA-2023-3333", allowed: false},
		{id: "RHBA-2023-4444", allowed: false},
		{id: "RHEA-2023-5555", allowed: false},
		{id: "SUSE-SU-2023-6666", allowed: false},
		{id: "openSUSE-SU-2023-7777", allowed: false},
		{id: "ALPINE-CVE-2023-8888", allowed: false},
		{id: "ECHO-4c1a-f6d4-292c", allowed: false},
		{id: "ROOT-OS-DEBIAN-12-CVE-2026-88806", allowed: false},
		{id: "BELL-CVE-2026-102633", allowed: false},
		{id: "MGASA-2026-0478", allowed: false},
		{id: "OESA-2023-1001", allowed: false},

		// Cross-ecosystem package name collisions must be excluded
		{id: "RUSTSEC-2025-0121", allowed: false},
		{id: "GO-2024-1234", allowed: false},
		{id: "PYSEC-2024-567", allowed: false},
		{id: "CGA-1111-2222-3333", allowed: false},
	}

	for _, tt := range tests {
		got := IsAllowedAdvisoryID(tt.id)
		if got != tt.allowed {
			t.Errorf("IsAllowedAdvisoryID(%q) = %v; want %v", tt.id, got, tt.allowed)
		}
	}
}

func TestFilterAllowedAdvisories(t *testing.T) {
	if got := FilterAllowedAdvisories(nil); got != nil {
		t.Fatalf("expected nil for empty slice, got %#v", got)
	}

	input := []Vulnerability{
		{ID: "DEBIAN-CVE-2016-7942"},
		{ID: "ECHO-4c1a-f6d4-292c"},
		{ID: "RUSTSEC-2025-0121"},
		{ID: "GHSA-1234"},
		{ID: "UBUNTU-CVE-2016-7942"},
		{ID: "CVE-2024-9999"},
		{ID: "OSV-2024-0001"},
		{ID: "USN-3093-1"},
	}

	got := FilterAllowedAdvisories(input)
	expected := []Vulnerability{
		{ID: "GHSA-1234"},
		{ID: "CVE-2024-9999"},
		{ID: "OSV-2024-0001"},
	}

	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("expected %#v, got %#v", expected, got)
	}
}
