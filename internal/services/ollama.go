package services

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"net/http"
	"net/url"
	"slices"

	"github.com/MegaGrindStone/go-mcp"
	"github.com/MegaGrindStone/mcp-web-ui/internal/models"
	"github.com/ollama/ollama/api"
)

// Ollama provides an implementation of the LLM interface for interacting with Ollama's language models.
// It manages connections to an Ollama server instance and handles streaming chat completions.
type Ollama struct {
	host         string
	model        string
	systemPrompt string

	client *api.Client
}

// NewOllama creates a new Ollama instance with the specified host URL and model name. The host
// parameter should be a valid URL pointing to an Ollama server. If the provided host URL is invalid,
// the function will panic.
func NewOllama(host, model, systemPrompt string) Ollama {
	u, err := url.Parse(host)
	if err != nil {
		panic(err)
	}

	return Ollama{
		host:         host,
		model:        model,
		systemPrompt: systemPrompt,
		client:       api.NewClient(u, &http.Client{}),
	}
}

// Chat implements the LLM interface by streaming responses from the Ollama model. It accepts a context
// for cancellation and a slice of messages representing the conversation history. The function returns
// an iterator that yields response chunks as strings and potential errors. The response is streamed
// incrementally, allowing for real-time processing of model outputs.
func (o Ollama) Chat(ctx context.Context, messages []models.Message, _ []mcp.Tool) iter.Seq2[models.Content, error] {
	return func(yield func(models.Content, error) bool) {
		msgs := make([]api.Message, len(messages))
		for i, msg := range messages {
			msgs[i] = api.Message{
				Role:    string(msg.Role),
				Content: models.RenderContents(msg.Contents),
			}
		}
		msgs = slices.Insert(msgs, 0, api.Message{
			Role:    "system",
			Content: o.systemPrompt,
		})

		t := true
		req := api.ChatRequest{
			Model:    o.model,
			Messages: msgs,
			Stream:   &t,
		}

		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		if err := o.client.Chat(ctx, &req, func(res api.ChatResponse) error {
			if !yield(models.Content{
				Type: models.ContentTypeText,
				Text: res.Message.Content,
			}, nil) {
				cancel()
			}
			return nil
		}); err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			yield(models.Content{}, fmt.Errorf("error sending request: %w", err))
		}
	}
}

// GenerateTitle generates a title for a given message using the Ollama API. It sends a single message to the
// Ollama API and returns the first response content as the title. The context can be used to cancel ongoing
// requests.
func (o Ollama) GenerateTitle(ctx context.Context, message string) (string, error) {
	f := false
	req := api.ChatRequest{
		Model: o.model,
		Messages: []api.Message{
			{
				Role:    "user",
				Content: message,
			},
		},
		Stream: &f,
	}

	var title string

	if err := o.client.Chat(ctx, &req, func(res api.ChatResponse) error {
		title = res.Message.Content
		return nil
	}); err != nil {
		return "", fmt.Errorf("error sending request: %w", err)
	}

	return title, nil
}
