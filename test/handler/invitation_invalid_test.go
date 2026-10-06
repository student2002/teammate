// invitation_invalid_test.go verifies an unknown invitation token maps to 401
// instead of surfacing the wrapped ErrNoRows as a 500.
package handler_test

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestAcceptInvitationUnknownToken(t *testing.T) {
	ts, client := setupAgentTestRouter(t)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/auth/accept-invitation",
		strings.NewReader(`{"invitation_token":"deadbeef"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unknown invitation token: expected 401, got %d, body: %s", resp.StatusCode, body)
	}
}
