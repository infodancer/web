// Package listmonk provides a client for Listmonk's public subscription API.
package listmonk

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type payload struct {
	Email     string   `json:"email"`
	ListUUIDs []string `json:"list_uuids"`
}

// Client posts subscriptions to Listmonk's public subscription API.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient returns a Client targeting baseURL (e.g. "http://listmonk:9000").
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

// Subscribe adds email to the list identified by listUUID.
// Returns nil on success or if the address is already subscribed (409).
// No-ops silently if baseURL is empty.
func (c *Client) Subscribe(email, listUUID string) error {
	if c.baseURL == "" {
		return nil
	}

	body, err := json.Marshal(payload{Email: email, ListUUIDs: []string{listUUID}})
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/api/public/subscription", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req) //nolint:gosec // G704: URL is the host-configured Listmonk base URL, not request-derived — no SSRF
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	// 200 = success, 409 = already subscribed — both are fine
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusConflict {
		return nil
	}
	rbody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	return fmt.Errorf("status %d: %s", resp.StatusCode, rbody)
}
