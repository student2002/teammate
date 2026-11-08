// webhook.go verifies GitHub webhook delivery signatures (plan §10.2).
package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// SignatureHeader is the GitHub webhook signature header.
const SignatureHeader = "X-Hub-Signature-256"

// VerifyWebhookSignature checks the X-Hub-Signature-256 header against the
// HMAC-SHA256 of the raw body under the webhook secret. The comparison is
// constant-time (hmac.Equal). The expected header format is
// "sha256=<hex digest>".
func VerifyWebhookSignature(secret string, body []byte, signatureHeader string) bool {
	signature := strings.TrimSpace(signatureHeader)
	const prefix = "sha256="
	if !strings.HasPrefix(signature, prefix) {
		return false
	}
	digest, err := hex.DecodeString(signature[len(prefix):])
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(digest, mac.Sum(nil))
}

// SignWebhookBody computes the "sha256=<hex>" signature of body under secret;
// tests and webhook senders use it to build the signature header.
func SignWebhookBody(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return fmt.Sprintf("sha256=%s", hex.EncodeToString(mac.Sum(nil)))
}
