package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// Sender delivers a reply to a Telegram chat. Abstracted so the webhook
// handler is testable without a live bot token.
type Sender interface {
	SendMessage(ctx context.Context, chatID int64, text string) error
}

// HTTPSender implements Sender via Telegram's public Bot API
// (https://core.telegram.org/bots/api#sendmessage), called directly over
// net/http. A single outbound call does not warrant pulling in a full
// generated client library.
type HTTPSender struct {
	httpClient *http.Client
	apiBaseURL string // overridable for tests; defaults to https://api.telegram.org
	botToken   string
}

// NewHTTPSender constructs an HTTPSender for the given bot token.
func NewHTTPSender(botToken string) *HTTPSender {
	return &HTTPSender{
		httpClient: http.DefaultClient,
		apiBaseURL: "https://api.telegram.org",
		botToken:   botToken,
	}
}

// NewHTTPSenderForTest constructs an HTTPSender pointed at an arbitrary
// base URL (e.g. an httptest.Server) instead of the real Telegram API.
func NewHTTPSenderForTest(apiBaseURL, botToken string) *HTTPSender {
	return &HTTPSender{
		httpClient: http.DefaultClient,
		apiBaseURL: apiBaseURL,
		botToken:   botToken,
	}
}

type sendMessageRequest struct {
	ChatID int64  `json:"chat_id"`
	Text   string `json:"text"`
}

// SendMessage posts text to chatID via the sendMessage Bot API method.
func (s *HTTPSender) SendMessage(ctx context.Context, chatID int64, text string) error {
	body, err := json.Marshal(sendMessageRequest{ChatID: chatID, Text: text})
	if err != nil {
		return fmt.Errorf("telegram: marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/bot%s/sendMessage", s.apiBaseURL, s.botToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("telegram: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("telegram: send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram: sendMessage returned status %d", resp.StatusCode)
	}
	return nil
}
