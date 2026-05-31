// Package buttondown provides a client for the Buttondown email API.
package buttondown

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const apiURL = "https://api.buttondown.email/v1/subscribers"

type payload struct {
	Email string   `json:"email_address"`
	Tags  []string `json:"tags,omitempty"`
}

// Client is a Buttondown API client.
type Client struct {
	apiKey string
	http   *http.Client
}

// NewClient returns a Client authenticated with apiKey.
func NewClient(apiKey string) *Client {
	return &Client{
		apiKey: apiKey,
		http:   &http.Client{Timeout: 10 * time.Second},
	}
}

// Subscribe adds email to the Buttondown list with optional tags.
// Returns nil on success or if the address is already subscribed (409).
// No-ops silently if the client was created with an empty apiKey.
func (c *Client) Subscribe(email string, tags []string) error {
	if c.apiKey == "" {
		return nil
	}

	body, err := json.Marshal(payload{Email: email, Tags: tags})
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Token "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req) //nolint:gosec // G704: URL is the host-configured Buttondown API endpoint, not request-derived — no SSRF
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	// 201 = created, 409 = already subscribed — both are fine
	if resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusConflict {
		return nil
	}
	rbody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	return fmt.Errorf("status %d: %s", resp.StatusCode, rbody)
}
