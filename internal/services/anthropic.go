package services

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"strings"

	"github.com/MegaGrindStone/mcp-web-ui/internal/models"
)

// Anthropic provides an interface to the Anthropic API for large language model interactions. It implements
// the LLM interface and handles streaming chat completions using Claude models.
type Anthropic struct {
	apiKey    string
	model     string
	maxTokens int

	client *http.Client
}

type anthropicChatRequest struct {
	Model       string             `json:"model"`
	Messages    []anthropicMessage `json:"messages"`
	System      string             `json:"system,omitempty"`
	MaxTokens   int                `json:"max_tokens,omitempty"`
	Temperature float64            `json:"temperature"`
	Stream      bool               `json:"stream"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicStreamResponse struct {
	Type  string `json:"type"`
	Delta struct {
		Text string `json:"text"`
	} `json:"delta"`
}

const (
	anthropicAPIEndpoint = "https://api.anthropic.com/v1"
)

// NewAnthropic creates a new Anthropic instance with the specified API key, model name, and maximum
// token limit. It initializes an HTTP client for API communication and returns a configured Anthropic
// instance ready for chat interactions.
func NewAnthropic(apiKey, model string, maxTokens int) Anthropic {
	return Anthropic{
		apiKey:    apiKey,
		model:     model,
		maxTokens: maxTokens,
		client:    &http.Client{},
	}
}

func extractSystemMessage(messages []models.Message) (string, []models.Message) {
	if len(messages) == 0 {
		return "", messages
	}

	if messages[0].Role == "system" {
		return messages[0].Content, messages[1:]
	}

	return "", messages
}

// Chat streams responses from the Anthropic API for a given sequence of messages. It processes system
// messages separately and returns an iterator that yields response chunks and potential errors. The
// context can be used to cancel ongoing requests. Refer to models.Message for message structure details.
func (a Anthropic) Chat(ctx context.Context, messages []models.Message) iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {
		systemMessage, ms := extractSystemMessage(messages)

		msgs := make([]anthropicMessage, len(ms))
		for i, msg := range ms {
			msgs[i] = anthropicMessage{
				Role:    msg.Role,
				Content: msg.Content,
			}
		}

		reqBody := anthropicChatRequest{
			Model:     a.model,
			Messages:  msgs,
			Stream:    true,
			System:    systemMessage,
			MaxTokens: a.maxTokens,
		}

		jsonBody, err := json.Marshal(reqBody)
		if err != nil {
			yield("", fmt.Errorf("error marshaling request: %w", err))
			return
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			anthropicAPIEndpoint+"/messages", bytes.NewBuffer(jsonBody))
		if err != nil {
			yield("", fmt.Errorf("error creating request: %w", err))
			return
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", a.apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")

		resp, err := a.client.Do(req)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			yield("", fmt.Errorf("error sending request: %w", err))
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			yield("", fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(body)))
			return
		}

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				return
			}

			var streamResp anthropicStreamResponse
			if err := json.Unmarshal([]byte(data), &streamResp); err != nil {
				if errors.Is(err, context.Canceled) {
					return
				}
				yield("", fmt.Errorf("error decoding response: %w", err))
				return
			}

			if streamResp.Type == "content_block_delta" && streamResp.Delta.Text != "" {
				if !yield(streamResp.Delta.Text, nil) {
					return
				}
			}

			if streamResp.Type == "message_stop" {
				return
			}
		}

		if err := scanner.Err(); err != nil {
			if !errors.Is(err, context.Canceled) {
				yield("", fmt.Errorf("error reading response: %w", err))
			}
		}
	}
}
