package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"net/http"

	"github.com/MegaGrindStone/mcp-web-ui/internal/models"
	"github.com/tmaxmax/go-sse"
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
	Tools       []anthropicTool    `json:"tools,omitempty"`
	Temperature float64            `json:"temperature"`
	Stream      bool               `json:"stream"`
}

type anthropicMessage struct {
	Role    string                    `json:"role"`
	Content []anthropicMessageContent `json:"content"`
}

type anthropicMessageContent struct {
	Type string `json:"type"`

	Text string `json:"text,omitempty"`
}

type anthropicContentBlockDelta struct {
	Type  string `json:"type"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		PartialJSON string `json:"partial_json"`
	} `json:"delta"`
}

type anthropicError struct {
	Type  string `json:"type"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
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

// Chat streams responses from the Anthropic API for a given sequence of messages. It processes system
// messages separately and returns an iterator that yields response chunks and potential errors. The
// context can be used to cancel ongoing requests. Refer to models.Message for message structure details.
func (a Anthropic) Chat(
	ctx context.Context,
	systemMessage string,
	messages []models.Message,
) iter.Seq2[models.Content, error] {
	return func(yield func(models.Content, error) bool) {
		resp, err := a.doRequest(ctx, systemMessage, messages)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			yield(models.Content{}, fmt.Errorf("error sending request: %w", err))
			return
		}
		defer resp.Body.Close()

		for ev, err := range sse.Read(resp.Body, nil) {
			if err != nil {
				yield(models.Content{}, fmt.Errorf("error reading response: %w", err))
				return
			}
			switch ev.Type {
			case "error":
				var e anthropicError
				if err := json.Unmarshal([]byte(ev.Data), &e); err != nil {
					yield(models.Content{}, fmt.Errorf("error unmarshaling error: %w", err))
					return
				}
				yield(models.Content{}, fmt.Errorf("anthropic error %s: %s", e.Error.Type, e.Error.Message))
				return
			case "message_stop":
				return
			case "content_block_delta":
				var res anthropicContentBlockDelta
				if err := json.Unmarshal([]byte(ev.Data), &res); err != nil {
					yield(models.Content{}, fmt.Errorf("error unmarshaling block delta: %w", err))
					return
				}
				if !yield(models.Content{
					Type: models.ContentTypeText,
					Text: res.Delta.Text,
				}, nil) {
					return
				}
			default:
			}
		}
	}
}

func (a Anthropic) doRequest(
	ctx context.Context,
	systemMessage string,
	messages []models.Message,
) (*http.Response, error) {
	msgs := make([]anthropicMessage, 0, len(messages))
	for _, msg := range messages {
		contents := make([]anthropicMessageContent, 0, len(msg.Contents))
		for _, ct := range msg.Contents {
			if ct.Type == models.ContentTypeText {
				contents = append(contents, anthropicMessageContent{
					Type: "text",
					Text: ct.Text,
				})
			}
		}
		msgs = append(msgs, anthropicMessage{
			Role:    msg.Role,
			Content: contents,
		})
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
		return nil, fmt.Errorf("error marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		anthropicAPIEndpoint+"/messages", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", a.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	return a.client.Do(req)
}
