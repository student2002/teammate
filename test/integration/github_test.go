// github_test.go covers the internal/integration/github package: webhook
// signature verification, the normalized trigger payload (plan §3.3 contract
// pinned by test/workflow/testdata/trigger-payload.json) and the REST
// client's headers, error classes and response size limit.
package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/teammate/server/internal/integration/github"
)

// TestVerifyWebhookSignature covers the HMAC roundtrip: the correct signature
// passes, and wrong body / wrong secret / malformed headers fail.
func TestVerifyWebhookSignature(t *testing.T) {
	secret := "hook-secret"
	body := []byte(`{"action":"opened"}`)
	signature := github.SignWebhookBody(secret, body)

	if !github.VerifyWebhookSignature(secret, body, signature) {
		t.Fatal("correct signature must verify")
	}
	if github.VerifyWebhookSignature(secret, []byte(`{"action":"edited"}`), signature) {
		t.Fatal("signature of another body must fail")
	}
	if github.VerifyWebhookSignature("other-secret", body, signature) {
		t.Fatal("signature under another secret must fail")
	}
	if github.VerifyWebhookSignature(secret, body, "sha256=not-hex") {
		t.Fatal("malformed digest must fail")
	}
	if github.VerifyWebhookSignature(secret, body, "md5=deadbeef") {
		t.Fatal("non-sha256 prefix must fail")
	}
	if github.VerifyWebhookSignature(secret, body, "") {
		t.Fatal("missing header must fail")
	}
}

// TestNormalizeWebhookPayloadIssueOpened pins the normalized payload key
// structure against the testdata contract.
func TestNormalizeWebhookPayloadIssueOpened(t *testing.T) {
	rawBody := []byte(`{
		"action": "opened",
		"issue": {
			"number": 42,
			"title": "登录页提交后返回 500",
			"body": "复现步骤",
			"labels": [{"name": "bug"}, {"name": "ui"}],
			"html_url": "https://github.com/owner/repo/issues/42"
		},
		"repository": {"id": 1, "full_name": "owner/repo", "default_branch": "main"},
		"sender": {"login": "octocat"}
	}`)

	payload, err := github.NormalizeWebhookPayload("issues", "delivery-1", rawBody)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if payload.Event != "issues" || payload.Action != "opened" || payload.DeliveryID != "delivery-1" {
		t.Fatalf("header fields wrong: %+v", payload)
	}
	if payload.Repository.ID != 1 || payload.Repository.FullName != "owner/repo" || payload.Repository.DefaultBranch != "main" {
		t.Fatalf("repository subset wrong: %+v", payload.Repository)
	}
	if payload.Sender.Login != "octocat" {
		t.Fatalf("sender subset wrong: %+v", payload.Sender)
	}
	if payload.Issue == nil {
		t.Fatal("issue subset missing")
	}
	if payload.Issue.Number != 42 || payload.Issue.Title != "登录页提交后返回 500" {
		t.Fatalf("issue fields wrong: %+v", payload.Issue)
	}
	if len(payload.Issue.Labels) != 2 || payload.Issue.Labels[0] != "bug" {
		t.Fatalf("labels must flatten to names: %+v", payload.Issue.Labels)
	}
	if payload.PullRequest != nil {
		t.Fatalf("pull_request must be null for issue events: %+v", payload.PullRequest)
	}
	var raw map[string]any
	if err := json.Unmarshal(payload.Raw, &raw); err != nil {
		t.Fatalf("raw must keep the original JSON: %v", err)
	}

	// The encoded payload must carry exactly the contract's top-level keys.
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &keys); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	for _, key := range []string{"event", "action", "delivery_id", "repository", "sender", "issue", "pull_request", "raw"} {
		if _, ok := keys[key]; !ok {
			t.Fatalf("payload missing contract key %q: %s", key, encoded)
		}
	}
}

// TestNormalizeWebhookPayloadPullRequest verifies PR events map to the
// pull_request subset and leave issue null.
func TestNormalizeWebhookPayloadPullRequest(t *testing.T) {
	rawBody := []byte(`{
		"action": "submitted",
		"pull_request": {"number": 7, "title": "Fix", "body": "", "labels": [], "html_url": "https://github.com/owner/repo/pull/7"},
		"repository": {"id": 2, "full_name": "owner/repo", "default_branch": "main"},
		"sender": {"login": "reviewer"}
	}`)

	payload, err := github.NormalizeWebhookPayload("pull_request_review", "d-2", rawBody)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if payload.Issue != nil {
		t.Fatalf("issue must be null for PR events: %+v", payload.Issue)
	}
	if payload.PullRequest == nil || payload.PullRequest.Number != 7 {
		t.Fatalf("pull_request subset wrong: %+v", payload.PullRequest)
	}
}

// TestNormalizeWebhookPayloadRejectsNonJSON verifies non-object bodies fail.
func TestNormalizeWebhookPayloadRejectsNonJSON(t *testing.T) {
	if _, err := github.NormalizeWebhookPayload("push", "d-3", []byte("not json")); err == nil {
		t.Fatal("non-JSON body must fail")
	}
	if _, err := github.NormalizeWebhookPayload("push", "d-3", []byte(`[1,2]`)); err == nil {
		t.Fatal("non-object JSON body must fail")
	}
}

// newFakeGitHub spins an httptest server asserting the standard headers and
// answering with the given status/body.
func newFakeGitHub(t *testing.T, wantPath string, status int, response string, seen *map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if seen != nil {
			(*seen)["path"] = r.URL.Path
			(*seen)["authorization"] = r.Header.Get("Authorization")
			(*seen)["accept"] = r.Header.Get("Accept")
			(*seen)["api_version"] = r.Header.Get("X-GitHub-Api-Version")
		}
		if wantPath != "" && r.URL.Path != wantPath {
			t.Errorf("unexpected path %q, want %q", r.URL.Path, wantPath)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(response))
	}))
}

// TestClientSendsStandardHeaders verifies auth/accept/api-version headers and
// the decoded repository response.
func TestClientSendsStandardHeaders(t *testing.T) {
	seen := map[string]string{}
	srv := newFakeGitHub(t, "/repos/owner/repo", http.StatusOK,
		`{"id":9,"full_name":"owner/repo","default_branch":"main","private":false}`, &seen)
	defer srv.Close()

	client := github.NewClient(srv.URL, "test-pat-123")
	repo, err := client.GetRepository(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatalf("GetRepository: %v", err)
	}
	if repo.ID != 9 || repo.FullName != "owner/repo" || repo.DefaultBranch != "main" {
		t.Fatalf("repository decode wrong: %+v", repo)
	}
	if seen["authorization"] != "Bearer test-pat-123" {
		t.Fatalf("authorization = %q", seen["authorization"])
	}
	if seen["accept"] != "application/vnd.github+json" {
		t.Fatalf("accept = %q", seen["accept"])
	}
	if seen["api_version"] != "2022-11-28" {
		t.Fatalf("api version = %q", seen["api_version"])
	}
}

// TestClientErrorClasses verifies 403/429 are retryable and other statuses
// are not.
func TestClientErrorClasses(t *testing.T) {
	cases := []struct {
		status    int
		retryable bool
	}{
		{http.StatusForbidden, true},
		{http.StatusTooManyRequests, true},
		{http.StatusUnauthorized, false},
		{http.StatusNotFound, false},
		{http.StatusInternalServerError, false},
	}
	for _, tc := range cases {
		srv := newFakeGitHub(t, "", tc.status, `{"message":"nope"}`, nil)
		client := github.NewClient(srv.URL, "pat")
		_, err := client.GetRepository(context.Background(), "owner", "repo")
		if err == nil {
			t.Fatalf("status %d must fail", tc.status)
		}
		var apiErr *github.APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("status %d must yield APIError, got %T", tc.status, err)
		}
		if apiErr.Retryable != tc.retryable {
			t.Fatalf("status %d retryable = %v, want %v", tc.status, apiErr.Retryable, tc.retryable)
		}
		srv.Close()
	}
}

// TestClientDefaultBaseURL verifies an empty baseURL selects the public API
// (constructor only; no network call).
func TestClientDefaultBaseURL(t *testing.T) {
	if github.DefaultAPIURL != "https://api.github.com" {
		t.Fatalf("default api url changed: %s", github.DefaultAPIURL)
	}
	client := github.NewClient("", "pat")
	if client == nil {
		t.Fatal("client must build with empty base url")
	}
}

// TestClientPATNeverInErrors verifies API errors carry status and body but no
// authorization material.
func TestClientPATNeverInErrors(t *testing.T) {
	srv := newFakeGitHub(t, "", http.StatusUnauthorized, `{"message":"Bad credentials"}`, nil)
	defer srv.Close()

	client := github.NewClient(srv.URL, "super-secret-pat-value")
	_, err := client.GetRepository(context.Background(), "owner", "repo")
	if err == nil {
		t.Fatal("401 must fail")
	}
	if strings.Contains(err.Error(), "super-secret-pat-value") {
		t.Fatalf("error message must not contain the PAT: %v", err)
	}
}

// TestPayloadContractFixtureAligns checks that the normalized payload of the
// testdata fixture decodes into the NormalizedPayload shape (round-trip of
// the contract document itself, minus its documentation key).
func TestPayloadContractFixtureAligns(t *testing.T) {
	data, err := os.ReadFile("../workflow/testdata/trigger-payload.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var fixture map[string]any
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	delete(fixture, "doc")
	encoded, err := json.Marshal(fixture)
	if err != nil {
		t.Fatalf("encode fixture: %v", err)
	}

	payload, err := github.NormalizeWebhookPayload("issues", "d1000000-0000-4000-8000-000000000001", encoded)
	if err != nil {
		t.Fatalf("normalize fixture: %v", err)
	}
	roundTrip, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(roundTrip, &got); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	for _, key := range []string{"event", "action", "delivery_id", "repository", "sender", "issue", "pull_request", "raw"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("normalized fixture missing key %q", key)
		}
	}
	if got["issue"] == nil {
		t.Fatal("fixture issue subset lost")
	}
}
