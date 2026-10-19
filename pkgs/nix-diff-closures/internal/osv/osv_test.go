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
