// payload.go normalizes a GitHub webhook delivery into the stable trigger
// payload subset of plan §3.3. The key structure is pinned by
// test/workflowv2/testdata/trigger-payload.json: event/action/delivery_id/
// repository{id,full_name,default_branch}/sender{login}/issue{...}/
// pull_request{...}/raw. raw keeps the original JSON but is never auto-injected
// into agent prompts.
package github

import (
	"encoding/json"
	"fmt"
)

// RepositoryInfo is the normalized repository subset.
type RepositoryInfo struct {
	ID            int64  `json:"id"`
	FullName      string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
}

// SenderInfo is the normalized sender subset.
type SenderInfo struct {
	Login string `json:"login"`
}

// IssueInfo is the normalized issue / pull request subset.
type IssueInfo struct {
	Number int64    `json:"number"`
	Title  string   `json:"title"`
	Body   string   `json:"body"`
	Labels []string `json:"labels"`
	URL    string   `json:"url"`
}

// NormalizedPayload is the trigger payload stored on workflow_events and on
// the run's trigger node output.
type NormalizedPayload struct {
	Event       string          `json:"event"`
	Action      string          `json:"action"`
	DeliveryID  string          `json:"delivery_id"`
	Repository  RepositoryInfo  `json:"repository"`
	Sender      SenderInfo      `json:"sender"`
	Issue       *IssueInfo      `json:"issue"`
	PullRequest *IssueInfo      `json:"pull_request"`
	Raw         json.RawMessage `json:"raw"`
}

// normalizeIssue extracts the stable subset from a GitHub issue or pull
// request object; nil input yields nil.
func normalizeIssue(raw any) *IssueInfo {
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	info := &IssueInfo{Labels: []string{}}
	info.Number = toInt64(obj["number"])
	info.Title = toString(obj["title"])
	info.Body = toString(obj["body"])
	info.URL = toString(obj["html_url"])
	if labels, ok := obj["labels"].([]any); ok {
		for _, label := range labels {
			switch typed := label.(type) {
			case map[string]any:
				info.Labels = append(info.Labels, toString(typed["name"]))
			case string:
				info.Labels = append(info.Labels, typed)
			}
		}
	}
	return info
}

// toInt64 converts a decoded JSON number to int64.
func toInt64(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case json.Number:
		n, _ := typed.Int64()
		return n
	default:
		return 0
	}
}

// toString converts a decoded JSON value to its string form.
func toString(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

// NormalizeWebhookPayload builds the normalized trigger payload from the
// webhook headers and raw body. The raw body must be a JSON object.
func NormalizeWebhookPayload(event, deliveryID string, rawBody []byte) (NormalizedPayload, error) {
	var raw map[string]any
	if err := json.Unmarshal(rawBody, &raw); err != nil || raw == nil {
		return NormalizedPayload{}, fmt.Errorf("webhook body is not a JSON object")
	}

	out := NormalizedPayload{
		Event:      event,
		Action:     toString(raw["action"]),
		DeliveryID: deliveryID,
		Raw:        json.RawMessage(rawBody),
	}
	if repo, ok := raw["repository"].(map[string]any); ok {
		out.Repository = RepositoryInfo{
			ID:            toInt64(repo["id"]),
			FullName:      toString(repo["full_name"]),
			DefaultBranch: toString(repo["default_branch"]),
		}
	}
	if sender, ok := raw["sender"].(map[string]any); ok {
		out.Sender = SenderInfo{Login: toString(sender["login"])}
	}
	// Events that carry an issue (issues, issue_comment) map the object to the
	// issue subset; pull_request events map it to the pull_request subset.
	if event == "pull_request" || event == "pull_request_review" {
		out.PullRequest = normalizeIssue(raw["pull_request"])
	} else if _, hasIssue := raw["issue"]; hasIssue {
		out.Issue = normalizeIssue(raw["issue"])
	}
	return out, nil
}
