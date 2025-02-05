package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"log"
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

func ollamaMessages(messages []models.Message) ([]api.Message, error) {
	msgs := make([]api.Message, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == models.RoleUser {
			if len(msg.Contents) != 1 {
				return nil, fmt.Errorf("user message should only contain one content, got %d", len(msg.Contents))
			}
			msgs = append(msgs, api.Message{
				Role:    string(msg.Role),
				Content: msg.Contents[0].Text,
			})
			continue
		}

		for _, ct := range msg.Contents {
			switch ct.Type {
			case models.ContentTypeText:
				if ct.Text != "" {
					msgs = append(msgs, api.Message{
						Role:    string(msg.Role),
						Content: ct.Text,
					})
				}
			case models.ContentTypeCallTool:
				args := make(map[string]any)
				if err := json.Unmarshal(ct.ToolInput, &args); err != nil {
					return nil, fmt.Errorf("error unmarshaling tool input: %w", err)
				}
				msgs = append(msgs, api.Message{
					Role: string(msg.Role),
					ToolCalls: []api.ToolCall{
						{
							Function: api.ToolCallFunction{
								Name:      ct.ToolName,
								Arguments: args,
							},
						},
					},
				})
			case models.ContentTypeToolResult:
				msgs = append(msgs, api.Message{
					Role:    "tool",
					Content: string(ct.ToolResult),
				})
			}
		}
	}
	return msgs, nil
}

// Chat implements the LLM interface by streaming responses from the Ollama model. It accepts a context
// for cancellation and a slice of messages representing the conversation history. The function returns
// an iterator that yields response chunks as strings and potential errors. The response is streamed
// incrementally, allowing for real-time processing of model outputs.
func (o Ollama) Chat(
	ctx context.Context,
	messages []models.Message,
	tools []mcp.Tool,
) iter.Seq2[models.Content, error] {
	return func(yield func(models.Content, error) bool) {
		msgs, err := ollamaMessages(messages)
		if err != nil {
			yield(models.Content{}, fmt.Errorf("error creating ollama messages: %w", err))
			return
		}

		msgs = slices.Insert(msgs, 0, api.Message{
			Role:    "system",
			Content: o.systemPrompt,
		})

		oTools := make([]api.Tool, len(tools))
		for i, tool := range tools {
			var params struct {
				Type       string   `json:"type"`
				Required   []string `json:"required"`
				Properties map[string]struct {
					Type        string   `json:"type"`
					Description string   `json:"description"`
					Enum        []string `json:"enum,omitempty"`
				} `json:"properties"`
			}
			if err := json.Unmarshal([]byte(tool.InputSchema), &params); err != nil {
				yield(models.Content{}, fmt.Errorf("error unmarshaling tool input schema: %w", err))
				return
			}
			oTool := api.Tool{
				Type: "function",
				Function: api.ToolFunction{
					Name:        tool.Name,
					Description: tool.Description,
					Parameters:  params,
				},
			}

			if err := json.Unmarshal([]byte(tool.InputSchema), &oTool.Function.Parameters); err != nil {
				yield(models.Content{}, fmt.Errorf("error unmarshaling tool input schema: %w", err))
				return
			}
			oTools[i] = oTool
		}

		t := true
		req := api.ChatRequest{
			Model:    o.model,
			Messages: msgs,
			Stream:   &t,
			Tools:    oTools,
		}

		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		if err := o.client.Chat(ctx, &req, func(res api.ChatResponse) error {
			if res.Message.Content != "" {
				if !yield(models.Content{
					Type: models.ContentTypeText,
					Text: res.Message.Content,
				}, nil) {
					cancel()
					return nil
				}
			}
			if len(res.Message.ToolCalls) > 0 {
				args, err := json.Marshal(res.Message.ToolCalls[0].Function.Arguments)
				if err != nil {
					return fmt.Errorf("error marshaling tool arguments: %w", err)
				}
				if len(res.Message.ToolCalls) > 1 {
					log.Printf("Received %d tool calls, but only the first one is supported", len(res.Message.ToolCalls))
					log.Printf("%+v", res.Message.ToolCalls)
				}
				if !yield(models.Content{
					Type:      models.ContentTypeCallTool,
					ToolName:  res.Message.ToolCalls[0].Function.Name,
					ToolInput: args,
				}, nil) {
					cancel()
				}
			}
			return nil
		}); err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			yield(models.Content{}, fmt.Errorf("error sending request: %w", err))
			return
		}
	}
}

// GenerateTitle generates a title for a given message using the Ollama API. It sends a single message to the
// Ollama API and returns the first response content as the title. The context can be used to cancel ongoing
// requests.
func (o Ollama) GenerateTitle(ctx context.Context, message string) (string, error) {
	msgs := []api.Message{
		{
			Role:    "system",
			Content: o.systemPrompt,
		},
		{
			Role:    "user",
			Content: message,
		},
	}
	f := false
	req := api.ChatRequest{
		Model:    o.model,
		Messages: msgs,
		Stream:   &f,
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
