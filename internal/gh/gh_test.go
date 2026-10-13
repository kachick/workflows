package gh

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFindOutdatedComments(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/owner/repo/issues/42/comments" {
			if r.URL.Query().Get("page") != "1" {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode([]commentItem{})
				return
			}
			comments := []commentItem{
				{
					NodeID: "node_1",
					Body:   "Hello <!-- nix-diff-closures-report -->",
					User: struct {
						Login string `json:"login"`
					}{Login: "github-actions[bot]"},
				},
				{
					NodeID: "node_2",
					Body:   "Other comment",
					User: struct {
						Login string `json:"login"`
					}{Login: "kachick"},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(comments)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	client := &Client{
		Token:      "dummy",
		APIBaseURL: ts.URL,
		HTTPClient: ts.Client(),
	}

	nodeIDs, err := client.FindOutdatedComments(context.Background(), "owner", "repo", 42, "<!-- nix-diff-closures-report -->")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(nodeIDs) != 1 || nodeIDs[0] != "node_1" {
		t.Fatalf("unexpected nodeIDs: %v", nodeIDs)
	}
}

func TestPostComment(t *testing.T) {
	posted := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/repos/owner/repo/issues/42/comments" {
			var payload map[string]string
			_ = json.NewDecoder(r.Body).Decode(&payload)
			if payload["body"] == "test comment" {
				posted = true
				w.WriteHeader(http.StatusCreated)
				return
			}
		}
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer ts.Close()

	client := &Client{
		Token:      "dummy",
		APIBaseURL: ts.URL,
		HTTPClient: ts.Client(),
	}

	err := client.PostComment(context.Background(), "owner", "repo", 42, "test comment")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !posted {
		t.Fatal("expected comment to be posted")
	}
}
