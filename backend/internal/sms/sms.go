package sms

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type Service interface {
	Send(ctx context.Context, mobile, message string) error
}

type client struct {
	baseURL string
	apiKey  string
	sender  string
	http    *http.Client
}

func NewClient(baseURL, apiKey, sender string) Service {
	return &client{baseURL: baseURL, apiKey: apiKey, sender: sender, http: &http.Client{}}
}

func (c *client) Send(ctx context.Context, mobile, message string) error {
	payload := map[string]string{
		"to":      mobile,
		"sender":  c.sender,
		"message": message,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/send", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-KEY", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("SMS gateway returned %d", resp.StatusCode)
	}
	return nil
}
