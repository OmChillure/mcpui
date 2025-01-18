package services

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"net/http"
	"net/url"

	"github.com/MegaGrindStone/mcp-web-ui/internal/models"
	"github.com/ollama/ollama/api"
)

// Ollama provides an implementation of the LLM interface for interacting with Ollama's language models.
// It manages connections to an Ollama server instance and handles streaming chat completions.
type Ollama struct {
	host  string
	model string

	client *api.Client
}

// NewOllama creates a new Ollama instance with the specified host URL and model name. The host
// parameter should be a valid URL pointing to an Ollama server. If the provided host URL is invalid,
// the function will panic.
func NewOllama(host, model string) Ollama {
	u, err := url.Parse(host)
	if err != nil {
		panic(err)
	}

	return Ollama{
		host:   host,
		model:  model,
		client: api.NewClient(u, &http.Client{}),
	}
}

// Chat implements the LLM interface by streaming responses from the Ollama model. It accepts a context
// for cancellation and a slice of messages representing the conversation history. The function returns
// an iterator that yields response chunks as strings and potential errors. The response is streamed
// incrementally, allowing for real-time processing of model outputs.
func (o Ollama) Chat(ctx context.Context, messages []models.Message) iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {
		msgs := make([]api.Message, len(messages))
		for i, msg := range messages {
			msgs[i] = api.Message{
				Role:    msg.Role,
				Content: msg.Content,
			}
		}

		t := true
		req := api.ChatRequest{
			Model:    o.model,
			Messages: msgs,
			Stream:   &t,
		}

		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		if err := o.client.Chat(ctx, &req, func(res api.ChatResponse) error {
			if !yield(res.Message.Content, nil) {
				cancel()
			}
			return nil
		}); err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			yield("", fmt.Errorf("error sending request: %w", err))
		}
	}
}
