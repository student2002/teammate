// api_client.go 封装 HTTP API 客户端，供 CLI 子命令调用后端接口。
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type apiClient struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

type Credentials struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	Email     string    `json:"email,omitempty"`
}

func newAPIClient() *apiClient {
	client := newAPIClientNoAuth()
	client.token = currentToken()
	return client
}

func newAPIClientNoAuth() *apiClient {
	return &apiClient{
		baseURL:    cliBaseURL(),
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}
}

func (c *apiClient) Get(path string, out interface{}) error {
	return c.do(http.MethodGet, path, nil, out)
}

func (c *apiClient) Post(path string, body interface{}, out interface{}) error {
	return c.do(http.MethodPost, path, body, out)
}

func (c *apiClient) Put(path string, body interface{}, out interface{}) error {
	return c.do(http.MethodPut, path, body, out)
}

func (c *apiClient) Patch(path string, body interface{}, out interface{}) error {
	return c.do(http.MethodPatch, path, body, out)
}

func (c *apiClient) Delete(path string) error {
	return c.do(http.MethodDelete, path, nil, nil)
}

func (c *apiClient) do(method, path string, body interface{}, out interface{}) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, joinURL(c.baseURL, path), reader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s: status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func cliBaseURL() string {
	if v := strings.TrimSpace(os.Getenv("TEAMMATE_SERVER_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	if v := strings.TrimSpace(os.Getenv("TEAMS_SERVER_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "http://localhost:8080"
}

func joinURL(baseURL, path string) string {
	if u, err := url.Parse(path); err == nil && u.IsAbs() {
		return path
	}
	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(path, "/")
}

func currentToken() string {
	if token, _ := rootCmd.Flags().GetString("token"); strings.TrimSpace(token) != "" {
		return strings.TrimSpace(token)
	}
	if token := strings.TrimSpace(os.Getenv("TEAMMATE_TOKEN")); token != "" {
		return token
	}
	creds, err := loadCredentials()
	if err != nil {
		return ""
	}
	return creds.Token
}

func requireAuth() {
	if currentToken() != "" {
		return
	}
	exitError(ExitAuth, "not authenticated; run 'teammate auth login' or pass --token")
}

func credentialsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".teammate", "credentials.json")
	}
	return filepath.Join(home, ".teammate", "credentials.json")
}

func saveCredentials(creds *Credentials) error {
	path := credentialsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create credentials dir: %w", err)
	}
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}
	return os.WriteFile(path, data, 0600)
}

func loadCredentials() (*Credentials, error) {
	data, err := os.ReadFile(credentialsPath())
	if err != nil {
		return nil, err
	}
	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("parse credentials: %w", err)
	}
	if creds.Token == "" {
		return nil, fmt.Errorf("credentials token is empty")
	}
	if !creds.ExpiresAt.IsZero() && time.Now().After(creds.ExpiresAt) {
		return nil, fmt.Errorf("credentials expired")
	}
	return &creds, nil
}

func deleteCredentials() error {
	if err := os.Remove(credentialsPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func parseTime(value string) time.Time {
	t, err := time.Parse(time.RFC3339, value)
	if err == nil {
		return t
	}
	return time.Time{}
}
