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

	t.Run("filters out distro-specific advisories", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			res := batchQueryResponse{
				Results: []queryResult{
					{
						// Package with only distro advisories should be considered clean
						Vulns: []Vulnerability{
							{ID: "DEBIAN-CVE-2016-7942"},
							{ID: "UBUNTU-CVE-2016-7942"},
							{ID: "USN-3093-1"},
						},
					},
					{
						// Package with mixed advisories should keep non-distro advisories
						Vulns: []Vulnerability{
							{ID: "DSA-1234"},
							{ID: "GHSA-5678"},
							{ID: "RHSA-2021-9999"},
							{ID: "CVE-2024-1111"},
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

func TestIsDistroAdvisoryID(t *testing.T) {
	tests := []struct {
		id       string
		isDistro bool
	}{
		{id: "DEBIAN-CVE-2016-7942", isDistro: true},
		{id: "debian-cve-2016-7942", isDistro: true},
		{id: "DLA-1234-1", isDistro: true},
		{id: "DSA-4321-1", isDistro: true},
		{id: "UBUNTU-CVE-2016-7942", isDistro: true},
		{id: "USN-3093-1", isDistro: true},
		{id: "ALMA-2023-1111", isDistro: true},
		{id: "RLSA-2023-2222", isDistro: true},
		{id: "RHSA-2023-3333", isDistro: true},
		{id: "RHBA-2023-4444", isDistro: true},
		{id: "RHEA-2023-5555", isDistro: true},
		{id: "SUSE-SU-2023-6666", isDistro: true},
		{id: "openSUSE-SU-2023-7777", isDistro: true},
		{id: "ALPINE-CVE-2023-8888", isDistro: true},
		// Non-distro advisories should be kept
		{id: "GHSA-1234-5678-90ab", isDistro: false},
		{id: "CVE-2024-12345", isDistro: false},
		{id: "RUSTSEC-2024-0001", isDistro: false},
		{id: "GO-2024-1234", isDistro: false},
		{id: "PYSEC-2024-567", isDistro: false},
		{id: "CGA-1111-2222-3333", isDistro: false},
		{id: "OSV-2024-999", isDistro: false},
	}

	for _, tt := range tests {
		got := IsDistroAdvisoryID(tt.id)
		if got != tt.isDistro {
			t.Errorf("IsDistroAdvisoryID(%q) = %v; want %v", tt.id, got, tt.isDistro)
		}
	}
}

func TestFilterDistroAdvisories(t *testing.T) {
	if got := FilterDistroAdvisories(nil); got != nil {
		t.Fatalf("expected nil for empty slice, got %#v", got)
	}

	input := []Vulnerability{
		{ID: "DEBIAN-CVE-2016-7942"},
		{ID: "GHSA-1234"},
		{ID: "UBUNTU-CVE-2016-7942"},
		{ID: "CVE-2024-9999"},
		{ID: "USN-3093-1"},
	}

	got := FilterDistroAdvisories(input)
	expected := []Vulnerability{
		{ID: "GHSA-1234"},
		{ID: "CVE-2024-9999"},
	}

	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("expected %#v, got %#v", expected, got)
	}
}
