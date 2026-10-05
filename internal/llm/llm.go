// Package llm is a minimal client for the Anthropic Messages API, used only by
// /api/ask and only when ANTHROPIC_API_KEY is set. It never logs or returns
// the key.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Defaults.
const (
	DefaultBaseURL = "https://api.anthropic.com"
	DefaultModel   = "claude-sonnet-5-5"
	APIVersion     = "2023-06-01"
)

// ErrRefused is returned when the model declines to answer.
var ErrRefused = errors.New("the model declined to answer")

// Client calls POST {BaseURL}/v1/messages.
type Client struct {
	BaseURL   string
	APIKey    string
	Model     string
	MaxTokens int
	HTTP      *http.Client
}

// FromEnv returns a client configured from the environment, or nil when
// ANTHROPIC_API_KEY is unset (the AI model stays off).
func FromEnv(getenv func(string) string) *Client {
	key := strings.TrimSpace(getenv("ANTHROPIC_API_KEY"))
	if key == "" {
		return nil
	}
	c := &Client{
		BaseURL:   DefaultBaseURL,
		APIKey:    key,
		Model:     DefaultModel,
		MaxTokens: 2048,
		HTTP:      &http.Client{Timeout: 60 * time.Second},
	}
	if m := strings.TrimSpace(getenv("AIOS_MODEL")); m != "" {
		c.Model = m
	}
	if b := strings.TrimSpace(getenv("ANTHROPIC_BASE_URL")); b != "" {
		c.BaseURL = b
	}
	return c
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type request struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	System    string    `json:"system,omitempty"`
	Messages  []message `json:"messages"`
}

type response struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Error      *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// Ask sends one user message with a system prompt and returns the text of the
// reply.
func (c *Client) Ask(ctx context.Context, system, user string) (string, error) {
	body, err := json.Marshal(request{
		Model:     c.Model,
		MaxTokens: c.MaxTokens,
		System:    system,
		Messages:  []message{{Role: "user", Content: user}},
	})
	if err != nil {
		return "", err
	}
	url := strings.TrimRight(c.BaseURL, "/") + "/v1/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("anthropic-version", APIVersion)
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	res, err := hc.Do(req)
	if err != nil {
		// The URL error carries only the URL, never headers, so it is safe to
		// surface; strip it to the cause anyway to keep messages short.
		var ue interface{ Unwrap() error }
		if errors.As(err, &ue) && ue.Unwrap() != nil {
			return "", fmt.Errorf("model request failed: %v", ue.Unwrap())
		}
		return "", fmt.Errorf("model request failed: %v", err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return "", fmt.Errorf("model response: %w", err)
	}
	var r response
	_ = json.Unmarshal(raw, &r)
	if res.StatusCode != http.StatusOK {
		msg := http.StatusText(res.StatusCode)
		if r.Error != nil && r.Error.Message != "" {
			msg = r.Error.Type + ": " + r.Error.Message
		}
		return "", fmt.Errorf("model returned %d (%s)", res.StatusCode, clip(msg, 200))
	}
	if r.StopReason == "refusal" {
		return "", ErrRefused
	}
	var parts []string
	for _, b := range r.Content {
		if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
			parts = append(parts, b.Text)
		}
	}
	text := strings.TrimSpace(strings.Join(parts, "\n"))
	if text == "" {
		return "", fmt.Errorf("model returned no text (stop reason %q)", r.StopReason)
	}
	return text, nil
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
