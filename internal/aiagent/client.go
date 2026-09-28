// Package aiagent talks to an OpenAI-compatible chat completions endpoint
// (e.g. a GLM model served through a router such as tokenrouter.com) to
// power the dashboard's RAG chatbot.
package aiagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type Message struct {
	Role    string `json:"role"` // "system" | "user" | "assistant"
	Content string `json:"content"`
}

type Client struct {
	BaseURL, APIKey, Model string
	HTTP                   *http.Client
}

func NewClient() *Client {
	return &Client{
		BaseURL: strings.TrimRight(os.Getenv("AI_AGENT_BASE_URL"), "/"),
		APIKey:  os.Getenv("AI_AGENT_API_KEY"),
		Model:   envOr("AI_AGENT_MODEL", "z-ai/glm-4.6"),
		HTTP:    &http.Client{Timeout: 45 * time.Second},
	}
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func (c *Client) Configured() bool { return c.BaseURL != "" && c.APIKey != "" }

type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature"`
	MaxTokens   int       `json:"max_tokens"`
}

type chatResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Chat sends the conversation to the configured model and returns the
// assistant's reply text.
func (c *Client) Chat(ctx context.Context, messages []Message) (string, error) {
	if !c.Configured() {
		return "", fmt.Errorf("AI agent not configured: set AI_AGENT_BASE_URL and AI_AGENT_API_KEY")
	}
	body, err := json.Marshal(chatRequest{
		Model:       c.Model,
		Messages:    messages,
		Temperature: 0.3,
		MaxTokens:   600,
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("ai agent request failed: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var out chatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("ai agent returned non-JSON response (status %d): %s", resp.StatusCode, truncate(string(raw), 300))
	}
	if out.Error != nil && out.Error.Message != "" {
		return "", fmt.Errorf("ai agent error: %s", out.Error.Message)
	}
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("ai agent status %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("ai agent returned no choices")
	}
	return out.Choices[0].Message.Content, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
