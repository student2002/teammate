// webhook.go 提供 GitHub Webhook 事件处理端点（HMAC 签名校验）。
package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/service"
)

type WebhookHandler struct {
	Svc *service.Service
}

type githubWebhookPayload struct {
	Action     string `json:"action"`
	Repository struct {
		Name  string `json:"name"`
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
	Issue struct {
		Number  int    `json:"number"`
		Title   string `json:"title"`
		Body    string `json:"body"`
		HTMLURL string `json:"html_url"`
		User    struct {
			Login string `json:"login"`
		} `json:"user"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
	} `json:"issue"`
}

func NewWebhookHandler(svc *service.Service) *WebhookHandler {
	return &WebhookHandler{Svc: svc}
}

func (h *WebhookHandler) GitHub(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		response.BadRequest(w, "read request body failed")
		return
	}

	var payload githubWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		response.BadRequest(w, "invalid github webhook payload")
		return
	}
	if payload.Repository.Owner.Login == "" || payload.Repository.Name == "" {
		response.BadRequest(w, "repository owner and name are required")
		return
	}

	triggerSvc := service.NewWorkflowTriggerService(h.Svc)
	secrets, err := triggerSvc.GitHubSecretsForRepo(r.Context(), payload.Repository.Owner.Login, payload.Repository.Name)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}
	if !validGitHubSignature(r.Header.Get("X-Hub-Signature-256"), body, secrets) {
		response.Unauthorized(w, "invalid github signature")
		return
	}

	if r.Header.Get("X-GitHub-Event") != "issues" || payload.Action != "opened" {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	labels := make([]string, 0, len(payload.Issue.Labels))
	for _, label := range payload.Issue.Labels {
		if label.Name != "" {
			labels = append(labels, label.Name)
		}
	}

	_, err = triggerSvc.HandleGitHubIssue(r.Context(), service.GitHubIssueEvent{
		Owner:      payload.Repository.Owner.Login,
		Repo:       payload.Repository.Name,
		Number:     payload.Issue.Number,
		Title:      payload.Issue.Title,
		Body:       payload.Issue.Body,
		URL:        payload.Issue.HTMLURL,
		Author:     payload.Issue.User.Login,
		Labels:     labels,
		Action:     payload.Action,
		RawPayload: body,
	})
	if err != nil {
		response.InternalServerError(w, fmt.Errorf("handle github issue webhook: %w", err))
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

func validGitHubSignature(header string, body []byte, secrets []string) bool {
	if header == "" || len(secrets) == 0 {
		return false
	}
	signatureHex, ok := strings.CutPrefix(header, "sha256=")
	if !ok {
		return false
	}
	signature, err := hex.DecodeString(signatureHex)
	if err != nil {
		return false
	}
	for _, secret := range secrets {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		if hmac.Equal(signature, mac.Sum(nil)) {
			return true
		}
	}
	return false
}
