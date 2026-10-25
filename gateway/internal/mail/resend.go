// Package mail sends the gateway's few emails (invites, password resets) through Resend.
package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Message is one email. Text is required; HTML is optional.
type Message struct {
	To      string
	Subject string
	Text    string
	HTML    string
}

// Sender delivers messages. A nil Sender means email isn't configured.
type Sender interface {
	Send(ctx context.Context, m Message) error
}

// Resend sends through https://resend.com's API (POST /emails).
type Resend struct {
	APIKey   string
	From     string
	Endpoint string // default https://api.resend.com/emails; tests point it at a fake
	Client   *http.Client
}

func NewResend(apiKey, from string) *Resend {
	return &Resend{APIKey: apiKey, From: from, Endpoint: "https://api.resend.com/emails", Client: &http.Client{Timeout: 15 * time.Second}}
}

type resendRequest struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Text    string   `json:"text"`
	HTML    string   `json:"html,omitempty"`
}

func (r *Resend) Send(ctx context.Context, m Message) error {
	body, err := json.Marshal(resendRequest{From: r.From, To: []string{m.To}, Subject: m.Subject, Text: m.Text, HTML: m.HTML})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.Endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+r.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "fling-gateway")
	resp, err := r.Client.Do(req)
	if err != nil {
		return fmt.Errorf("resend: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		// Resend's error body names the problem (unverified domain, bad key) without echoing the key.
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("resend: %s: %s", resp.Status, bytes.TrimSpace(detail))
	}
	return nil
}
