package gh

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// ErrPermissionDenied is returned when GitHub returns 403 Forbidden.
var ErrPermissionDenied = errors.New("github api permission denied")

// Client interacts with GitHub REST and GraphQL APIs without third-party dependencies.
type Client struct {
	Token      string
	APIBaseURL string
	GraphQLURL string
	HTTPClient *http.Client
}

// NewClient creates a new GitHub API client.
func NewClient(token string) *Client {
	apiBase := os.Getenv("GITHUB_API_URL")
	if apiBase == "" {
		apiBase = "https://api.github.com"
	}
	graphqlURL := os.Getenv("GITHUB_GRAPHQL_URL")
	if graphqlURL == "" {
		graphqlURL = "https://api.github.com/graphql"
	}

	return &Client{
		Token:      token,
		APIBaseURL: strings.TrimRight(apiBase, "/"),
		GraphQLURL: graphqlURL,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

type commentItem struct {
	NodeID string `json:"node_id"`
	Body   string `json:"body"`
	User   struct {
		Login string `json:"login"`
	} `json:"user"`
}

// FindOutdatedComments finds comments made by github-actions[bot] that contain marker.
func (c *Client) FindOutdatedComments(ctx context.Context, owner, repo string, pr int, marker string) ([]string, error) {
	var nodeIDs []string
	page := 1

	for {
		url := fmt.Sprintf("%s/repos/%s/%s/issues/%d/comments?per_page=100&page=%d", c.APIBaseURL, owner, repo, pr, page)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		c.setHeaders(req)

		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode == http.StatusForbidden {
			resp.Body.Close()
			return nil, ErrPermissionDenied
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("github api error (%d): %s", resp.StatusCode, string(body))
		}

		var comments []commentItem
		err = json.NewDecoder(resp.Body).Decode(&comments)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}

		if len(comments) == 0 {
			break
		}

		for _, item := range comments {
			if item.User.Login == "github-actions[bot]" && strings.Contains(item.Body, marker) {
				nodeIDs = append(nodeIDs, item.NodeID)
			}
		}

		if len(comments) < 100 {
			break
		}

		page++
	}

	return nodeIDs, nil
}

// MinimizeComment marks a comment as OUTDATED via GraphQL.
func (c *Client) MinimizeComment(ctx context.Context, nodeID string) error {
	query := `mutation($subjectId: ID!) {
		minimizeComment(input: { subjectId: $subjectId, classifier: OUTDATED }) {
			minimizedComment { isMinimized }
		}
	}`

	payload := map[string]interface{}{
		"query": query,
		"variables": map[string]interface{}{
			"subjectId": nodeID,
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.GraphQLURL, bytes.NewReader(data))
	if err != nil {
		return err
	}
	c.setHeaders(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		return ErrPermissionDenied
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("graphql error (%d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// PostComment posts a new comment on the pull request.
func (c *Client) PostComment(ctx context.Context, owner, repo string, pr int, body string) error {
	url := fmt.Sprintf("%s/repos/%s/%s/issues/%d/comments", c.APIBaseURL, owner, repo, pr)
	payload := map[string]string{
		"body": body,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	c.setHeaders(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		return ErrPermissionDenied
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to post comment (%d): %s", resp.StatusCode, string(resBody))
	}

	return nil
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.Token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.Token))
	}
}
