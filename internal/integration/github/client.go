// Package github provides the minimal GitHub REST API client used by workflow
// GitHub actions and integration tests (plan §10.3). It uses net/http only —
// no GitHub SDK. The client never logs and never embeds the PAT in errors;
// error messages carry the HTTP status and a truncated response body only.
package github

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

// DefaultAPIURL is the public GitHub REST API root.
const DefaultAPIURL = "https://api.github.com"

// APIVersion pins the GitHub API version header (plan §10.3).
const APIVersion = "2022-11-28"

// maxResponseBytes bounds every response body read (plan §10.3: 1 MiB).
const maxResponseBytes = 1 << 20

// requestTimeout bounds one API call.
const requestTimeout = 30 * time.Second

// Client is a minimal GitHub REST API client bound to one PAT.
type Client struct {
	baseURL string
	pat     string
	http    *http.Client
}

// NewClient returns a client for baseURL (empty selects DefaultAPIURL) that
// authenticates with the given PAT.
func NewClient(baseURL, pat string) *Client {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultAPIURL
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		pat:     pat,
		http:    &http.Client{Timeout: requestTimeout},
	}
}

// APIError is a non-2xx GitHub API response. Retryable marks 403/429 (rate
// limit / secondary limit) as retryable per plan §10.3.
type APIError struct {
	StatusCode int
	Body       string
	Retryable  bool
}

func (e *APIError) Error() string {
	kind := "github api error"
	if e.Retryable {
		kind = "github api error (retryable)"
	}
	return fmt.Sprintf("%s: status %d: %s", kind, e.StatusCode, e.Body)
}

// Repository is the repository subset returned by GetRepository.
type Repository struct {
	ID            int64  `json:"id"`
	FullName      string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
	Private       bool   `json:"private"`
}

// IssueComment is the response subset of a created issue comment.
type IssueComment struct {
	HTMLURL string `json:"html_url"`
}

// PullRequest is the response subset of a created pull request.
type PullRequest struct {
	Number  int64  `json:"number"`
	HTMLURL string `json:"html_url"`
}

// CreatePullRequestParams is the body of the create-pull-request call.
type CreatePullRequestParams struct {
	Title string `json:"title"`
	Head  string `json:"head"`
	Base  string `json:"base"`
	Body  string `json:"body"`
}

// GetRepository loads the repository metadata; used by the connection test.
func (c *Client) GetRepository(ctx context.Context, owner, repo string) (Repository, error) {
	var out Repository
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/%s", owner, repo), nil, &out); err != nil {
		return Repository{}, err
	}
	return out, nil
}

// CreateIssueComment posts a comment body on one issue and returns its URL.
func (c *Client) CreateIssueComment(ctx context.Context, owner, repo string, number int64, body string) (IssueComment, error) {
	var out IssueComment
	payload := map[string]string{"body": body}
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/%s/issues/%d/comments", owner, repo, number), payload, &out); err != nil {
		return IssueComment{}, err
	}
	return out, nil
}

// AddLabels attaches labels to one issue (issues and PRs share the endpoint).
func (c *Client) AddLabels(ctx context.Context, owner, repo string, number int64, labels []string) error {
	payload := map[string][]string{"labels": labels}
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/%s/issues/%d/labels", owner, repo, number), payload, nil)
}

// RemoveLabels detaches each label from one issue; GitHub deletes one label
// per request, so multiple labels issue one request each.
func (c *Client) RemoveLabels(ctx context.Context, owner, repo string, number int64, labels []string) error {
	for _, label := range labels {
		path := fmt.Sprintf("/repos/%s/%s/issues/%d/labels/%s", owner, repo, number, label)
		if err := c.do(ctx, http.MethodDelete, path, nil, nil); err != nil {
			return err
		}
	}
	return nil
}

// AddAssignees assigns users to one issue.
func (c *Client) AddAssignees(ctx context.Context, owner, repo string, number int64, assignees []string) error {
	payload := map[string][]string{"assignees": assignees}
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/%s/issues/%d/assignees", owner, repo, number), payload, nil)
}

// RemoveAssignees unassigns users from one issue.
func (c *Client) RemoveAssignees(ctx context.Context, owner, repo string, number int64, assignees []string) error {
	payload := map[string][]string{"assignees": assignees}
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("/repos/%s/%s/issues/%d/assignees", owner, repo, number), payload, nil)
}

// CloseIssue closes one issue.
func (c *Client) CloseIssue(ctx context.Context, owner, repo string, number int64) error {
	return c.patchIssueState(ctx, owner, repo, number, "closed")
}

// ReopenIssue reopens one issue.
func (c *Client) ReopenIssue(ctx context.Context, owner, repo string, number int64) error {
	return c.patchIssueState(ctx, owner, repo, number, "open")
}

func (c *Client) patchIssueState(ctx context.Context, owner, repo string, number int64, state string) error {
	payload := map[string]string{"state": state}
	return c.do(ctx, http.MethodPatch, fmt.Sprintf("/repos/%s/%s/issues/%d", owner, repo, number), payload, nil)
}

// CreatePullRequest opens one pull request and returns its number and URL.
func (c *Client) CreatePullRequest(ctx context.Context, owner, repo string, params CreatePullRequestParams) (PullRequest, error) {
	var out PullRequest
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/%s/pulls", owner, repo), params, &out); err != nil {
		return PullRequest{}, err
	}
	return out, nil
}

// RequestReviewers requests reviewers on one pull request.
func (c *Client) RequestReviewers(ctx context.Context, owner, repo string, number int64, reviewers []string) error {
	payload := map[string][]string{"reviewers": reviewers}
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/%s/pulls/%d/requested_reviewers", owner, repo, number), payload, nil)
}

// do performs one API call: it sends the JSON body when present, enforces the
// response size limit and decodes a 2xx response into out (when out is not
// nil). Non-2xx responses become APIError; 403/429 are marked retryable.
func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request body: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.pat)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", APIVersion)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("call github api %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read github api response: %w", err)
	}
	if len(respBody) > maxResponseBytes {
		return fmt.Errorf("github api response exceeds %d bytes", maxResponseBytes)
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out == nil || len(respBody) == 0 {
			return nil
		}
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode github api response: %w", err)
		}
		return nil
	}

	retryable := resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests
	return &APIError{
		StatusCode: resp.StatusCode,
		Body:       truncateForError(string(respBody)),
		Retryable:  retryable,
	}
}

// truncateForError bounds the body excerpt carried in error messages.
func truncateForError(body string) string {
	const max = 512
	if len(body) > max {
		return body[:max]
	}
	return body
}
