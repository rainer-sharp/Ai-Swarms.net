// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Rainer Sestig. Kommerzielle Lizenz: siehe COMMERCIAL.md.

// Package backend spricht lokale Inferenz-Server über die OpenAI-kompatible
// Schnittstelle an (llama.cpp, Ollama, vLLM, LM Studio, MLX).
package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/protocol"
)

type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

func New(baseURL, apiKey string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		HTTP:    &http.Client{Timeout: 10 * time.Minute},
	}
}

type ChatRequest struct {
	Model       string                 `json:"model"`
	Messages    []protocol.ChatMessage `json:"messages"`
	MaxTokens   int                    `json:"max_tokens,omitempty"`
	Temperature *float64               `json:"temperature,omitempty"`
	Stream      bool                   `json:"stream"`
	Tools       json.RawMessage        `json:"tools,omitempty"`
	ToolChoice  json.RawMessage        `json:"tool_choice,omitempty"`
}

type ChatResponse struct {
	Content          string
	ToolCalls        []protocol.ToolCall
	PromptTokens     int
	CompletionTokens int
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("backend %s%s: %s: %s", c.BaseURL, path, resp.Status, strings.TrimSpace(string(msg)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	req.Stream = false
	var raw struct {
		Choices []struct {
			Message      protocol.ChatMessage `json:"message"`
			FinishReason string               `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := c.do(ctx, http.MethodPost, "/chat/completions", req, &raw); err != nil {
		return ChatResponse{}, err
	}
	if len(raw.Choices) == 0 {
		return ChatResponse{}, fmt.Errorf("backend lieferte keine antwort")
	}
	// Leere Antworten sind Fehlschläge, damit der Orchestrator ausweicht.
	// Typisch bei Reasoning-Modellen: Das Nachdenken verbraucht max_tokens.
	// Ein Werkzeug-Aufruf ohne Text ist dagegen eine vollständige Antwort.
	if strings.TrimSpace(raw.Choices[0].Message.Content) == "" && len(raw.Choices[0].Message.ToolCalls) == 0 {
		if raw.Choices[0].FinishReason == "length" {
			return ChatResponse{}, fmt.Errorf("leere antwort: token-limit (max_tokens %d) erreicht, bevor eine antwort kam", req.MaxTokens)
		}
		// Nur Metadaten, nie Inhalte: gpt-oss etwa antwortet leer, wenn die
		// Nachrichten auf den Assistenten enden.
		last := ""
		if len(req.Messages) > 0 {
			last = req.Messages[len(req.Messages)-1].Role
		}
		return ChatResponse{}, fmt.Errorf("backend lieferte eine leere antwort (finish_reason %q, %d tokens, %d nachrichten, letzte rolle %q)",
			raw.Choices[0].FinishReason, raw.Usage.CompletionTokens, len(req.Messages), last)
	}
	return ChatResponse{
		Content:          raw.Choices[0].Message.Content,
		ToolCalls:        raw.Choices[0].Message.ToolCalls,
		PromptTokens:     raw.Usage.PromptTokens,
		CompletionTokens: raw.Usage.CompletionTokens,
	}, nil
}

// Models listet die vom Backend angebotenen Modelle.
func (c *Client) Models(ctx context.Context) ([]string, error) {
	var raw struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/models", nil, &raw); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(raw.Data))
	for _, m := range raw.Data {
		out = append(out, m.ID)
	}
	return out, nil
}
