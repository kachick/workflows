package gh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFindOutdatedComments(t *testing.T) {
	t.Run("paginates and filters bot comments by marker", func(t *testing.T) {
		var pages []string
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/repos/owner/repo/issues/42/comments" {
				http.NotFound(w, r)
				return
			}
			page := r.URL.Query().Get("page")
			pages = append(pages, page)
			w.Header().Set("Content-Type", "application/json")

			switch page {
			case "1":
				comments := make([]commentItem, 100)
				for i := range comments {
					comments[i] = commentItem{
						NodeID: fmt.Sprintf("other_%d", i),
						Body:   "unrelated",
					}
					comments[i].User.Login = "kachick"
				}
				comments[0].NodeID = "node_1"
				comments[0].Body = "Hello <!-- nix-diff-closures-report -->"
				comments[0].User.Login = "github-actions[bot]"
				_ = json.NewEncoder(w).Encode(comments)
			case "2":
				comments := []commentItem{
					{
						NodeID: "node_2",
						Body:   "Second page <!-- nix-diff-closures-report -->",
						User: struct {
							Login string `json:"login"`
						}{Login: "github-actions[bot]"},
					},
				}
				_ = json.NewEncoder(w).Encode(comments)
			default:
				_ = json.NewEncoder(w).Encode([]commentItem{})
			}
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

		if len(pages) != 2 || pages[0] != "1" || pages[1] != "2" {
			t.Fatalf("expected pages [1 2], got %v", pages)
		}
		if len(nodeIDs) != 2 || nodeIDs[0] != "node_1" || nodeIDs[1] != "node_2" {
			t.Fatalf("unexpected nodeIDs: %v", nodeIDs)
		}
	})

	t.Run("returns ErrPermissionDenied on 403", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}))
		defer ts.Close()

		client := &Client{Token: "dummy", APIBaseURL: ts.URL, HTTPClient: ts.Client()}
		_, err := client.FindOutdatedComments(context.Background(), "owner", "repo", 42, "marker")
		if !errors.Is(err, ErrPermissionDenied) {
			t.Fatalf("expected ErrPermissionDenied, got %v", err)
		}
	})
}

func TestMinimizeComment(t *testing.T) {
	t.Run("succeeds when isMinimized is true", func(t *testing.T) {
		var gotContentType string
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotContentType = r.Header.Get("Content-Type")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"minimizeComment":{"minimizedComment":{"isMinimized":true}}}}`))
		}))
		defer ts.Close()

		client := &Client{Token: "dummy", GraphQLURL: ts.URL, HTTPClient: ts.Client()}
		if err := client.MinimizeComment(context.Background(), "node_1"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if gotContentType != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", gotContentType)
		}
	})

	t.Run("returns error on HTTP 200 with GraphQL errors", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"errors":[{"type":"NOT_FOUND","message":"Could not resolve to a node"}]}`))
		}))
		defer ts.Close()

		client := &Client{Token: "dummy", GraphQLURL: ts.URL, HTTPClient: ts.Client()}
		err := client.MinimizeComment(context.Background(), "bad_node")
		if err == nil {
			t.Fatal("expected error for GraphQL errors payload, got nil")
		}
		if errors.Is(err, ErrPermissionDenied) {
			t.Fatalf("did not expect ErrPermissionDenied, got %v", err)
		}
	})

	t.Run("returns ErrPermissionDenied on HTTP 200 with GraphQL FORBIDDEN", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"errors":[{"type":"FORBIDDEN","message":"Resource not accessible by integration"}]}`))
		}))
		defer ts.Close()

		client := &Client{Token: "dummy", GraphQLURL: ts.URL, HTTPClient: ts.Client()}
		err := client.MinimizeComment(context.Background(), "node_1")
		if !errors.Is(err, ErrPermissionDenied) {
			t.Fatalf("expected ErrPermissionDenied, got %v", err)
		}
	})

	t.Run("returns error when isMinimized is false", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"minimizeComment":{"minimizedComment":{"isMinimized":false}}}}`))
		}))
		defer ts.Close()

		client := &Client{Token: "dummy", GraphQLURL: ts.URL, HTTPClient: ts.Client()}
		if err := client.MinimizeComment(context.Background(), "node_1"); err == nil {
			t.Fatal("expected error when isMinimized is false, got nil")
		}
	})
}

func TestMinimizeOutdatedComments(t *testing.T) {
	minimized := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/owner/repo/issues/42/comments":
			comments := []commentItem{
				{
					NodeID: "node_1",
					Body:   "marker here",
					User: struct {
						Login string `json:"login"`
					}{Login: "github-actions[bot]"},
				},
			}
			_ = json.NewEncoder(w).Encode(comments)
		case "/graphql":
			minimized++
			_, _ = w.Write([]byte(`{"data":{"minimizeComment":{"minimizedComment":{"isMinimized":true}}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	client := &Client{
		Token:      "dummy",
		APIBaseURL: ts.URL,
		GraphQLURL: ts.URL + "/graphql",
		HTTPClient: ts.Client(),
	}

	count, err := client.MinimizeOutdatedComments(context.Background(), "owner", "repo", 42, "marker")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 1 || minimized != 1 {
		t.Fatalf("expected 1 minimized comment, got count=%d calls=%d", count, minimized)
	}
}

func TestPostComment(t *testing.T) {
	t.Run("posts comment with JSON content type", func(t *testing.T) {
		posted := false
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && r.URL.Path == "/repos/owner/repo/issues/42/comments" {
				if r.Header.Get("Content-Type") != "application/json" {
					w.WriteHeader(http.StatusUnsupportedMediaType)
					return
				}
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
	})

	t.Run("returns ErrPermissionDenied on 403", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}))
		defer ts.Close()

		client := &Client{Token: "dummy", APIBaseURL: ts.URL, HTTPClient: ts.Client()}
		err := client.PostComment(context.Background(), "owner", "repo", 42, "test")
		if !errors.Is(err, ErrPermissionDenied) {
			t.Fatalf("expected ErrPermissionDenied, got %v", err)
		}
	})
}
