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

type Ollama struct {
	host  string
	model string

	client *api.Client
}

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
