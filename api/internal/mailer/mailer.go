// Package mailer provides an interface and implementations for sending
// transactional email. Use NewResend to create a production sender backed
// by the Resend HTTP API.
package mailer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// Mailer sends transactional emails.
type Mailer interface {
	// SendMagicLink emails a login link to the given recipient.
	SendMagicLink(ctx context.Context, to, name, magicLinkURL string) error
}

// ResendMailer sends transactional email via the Resend HTTP API.
// See https://resend.com/docs/api-reference/emails/send-email.
type ResendMailer struct {
	apiKey     string
	httpClient *http.Client
}

// NewResend creates a ResendMailer using the provided API key.
func NewResend(apiKey string) *ResendMailer {
	return &ResendMailer{
		apiKey:     apiKey,
		httpClient: &http.Client{},
	}
}

// SendMagicLink delivers a magic-link email to to via Resend.
func (m *ResendMailer) SendMagicLink(ctx context.Context, to, name, magicLinkURL string) error {
	body := map[string]any{
		"from":    "Wrappr <noreply@wrappr.io>",
		"to":      []string{to},
		"subject": "Your Wrappr login link",
		"html":    magicLinkHTML(name, magicLinkURL),
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("mailer: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("mailer: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+m.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("mailer: send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("mailer: unexpected status %d from Resend", resp.StatusCode)
	}

	return nil
}

// magicLinkHTML returns a minimal HTML email body containing a login button.
func magicLinkHTML(name, magicLinkURL string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"></head>
<body style="font-family:sans-serif;max-width:480px;margin:0 auto;padding:24px">
  <h2>Hi %s,</h2>
  <p>Click the button below to log in to Wrappr. The link expires in 1 hour.</p>
  <p>
    <a href="%s"
       style="display:inline-block;padding:12px 24px;background:#1a1a2e;color:#fff;
              border-radius:6px;text-decoration:none;font-weight:bold">
      Log in to Wrappr
    </a>
  </p>
  <p style="color:#666;font-size:12px">
    If you did not request this email, you can safely ignore it.
  </p>
</body>
</html>`, name, magicLinkURL)
}
