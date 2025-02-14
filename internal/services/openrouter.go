package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/MegaGrindStone/go-mcp"
	"github.com/MegaGrindStone/mcp-web-ui/internal/models"
	"github.com/tmaxmax/go-sse"
)

// OpenRouter provides an implementation of the LLM interface for interacting with OpenRouter's language models.
type OpenRouter struct {
	apiKey       string
	model        string
	systemPrompt string

	client *http.Client

	logger *slog.Logger
}

type openRouterChatRequest struct {
	Model    string              `json:"model"`
	Messages []openRouterMessage `json:"messages"`
	Tools    []openRouterTool    `json:"tools,omitempty"`
	Stream   bool                `json:"stream"`
}

type openRouterMessage struct {
	Role       string                `json:"role"`
	Content    string                `json:"content,omitempty"`
	ToolCalls  []openRouterToolCalls `json:"tool_calls,omitempty"`
	ToolCallID string                `json:"tool_call_id,omitempty"`
}

type openRouterToolCalls struct {
	ID       string                     `json:"id"`
	Type     string                     `json:"type"`
	Function openRouterToolCallFunction `json:"function"`
}

type openRouterToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openRouterTool struct {
	Type     string                 `json:"type"`
	Function openRouterToolFunction `json:"function"`
}

type openRouterToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type openRouterStreamingResponse struct {
	Choices []openRouterStreamingChoice `json:"choices"`
}

type openRouterStreamingChoice struct {
	Delta openRouterMessage `json:"delta"`
}

type openRouterResponse struct {
	Choices []openRouterChoice `json:"choices"`
}

type openRouterChoice struct {
	Message openRouterMessage `json:"message"`
}

const (
	openRouterAPIEndpoint = "https://openrouter.ai/api/v1"
)

// NewOpenRouter creates a new OpenRouter instance with the specified API key, model name, and system prompt.
func NewOpenRouter(apiKey, model, systemPrompt string, logger *slog.Logger) OpenRouter {
	return OpenRouter{
		apiKey:       apiKey,
		model:        model,
		systemPrompt: systemPrompt,
		client:       &http.Client{},
		logger:       logger.With(slog.String("module", "openrouter")),
	}
}

// Chat streams responses from the OpenRouter API for a given sequence of messages. It processes system
// messages separately and returns an iterator that yields response chunks and potential errors. The
// context can be used to cancel ongoing requests. Refer to models.Message for message structure details.
func (o OpenRouter) Chat(
	ctx context.Context,
	messages []models.Message,
	tools []mcp.Tool,
) iter.Seq2[models.Content, error] {
	return func(yield func(models.Content, error) bool) {
		resp, err := o.doRequest(ctx, messages, tools, true)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			yield(models.Content{}, fmt.Errorf("error sending request: %w", err))
			return
		}
		defer resp.Body.Close()

		toolUse := false
		toolArgs := ""
		callToolContent := models.Content{
			Type: models.ContentTypeCallTool,
		}
		for ev, err := range sse.Read(resp.Body, nil) {
			if err != nil {
				yield(models.Content{}, fmt.Errorf("error reading response: %w", err))
				return
			}

			o.logger.Debug("Received event",
				slog.String("event", ev.Data),
			)

			if ev.Data == "[DONE]" {
				break
			}

			var res openRouterStreamingResponse
			if err := json.Unmarshal([]byte(ev.Data), &res); err != nil {
				yield(models.Content{}, fmt.Errorf("error unmarshaling response: %w", err))
				return
			}

			if len(res.Choices) == 0 {
				continue
			}
			choice := res.Choices[0]

			if len(choice.Delta.ToolCalls) > 0 {
				if len(choice.Delta.ToolCalls) > 1 {
					o.logger.Warn("Received multiples tool call, but only the first one is supported",
						slog.Int("count", len(choice.Delta.ToolCalls)),
						slog.String("toolCalls", fmt.Sprintf("%+v", choice.Delta.ToolCalls)),
					)
				}
				toolArgs += choice.Delta.ToolCalls[0].Function.Arguments
				if !toolUse {
					toolID := fmt.Sprintf("%s-%d", choice.Delta.ToolCalls[0].ID, time.Now().UnixMilli())
					toolUse = true
					callToolContent.ToolName = choice.Delta.ToolCalls[0].Function.Name
					callToolContent.CallToolID = toolID
				}
			}

			if choice.Delta.Content != "" {
				if !yield(models.Content{
					Type: models.ContentTypeText,
					Text: choice.Delta.Content,
				}, nil) {
					break
				}
			}
		}
		if toolUse {
			if toolArgs == "" {
				toolArgs = "{}"
			}
			o.logger.Debug("Call Tool",
				slog.String("name", callToolContent.ToolName),
				slog.String("args", toolArgs),
			)
			callToolContent.ToolInput = json.RawMessage(toolArgs)
			yield(callToolContent, nil)
		}
	}
}

// GenerateTitle generates a title for a given message using the OpenRouter API. It sends a single message to the
// OpenRouter API and returns the first response content as the title. The context can be used to cancel ongoing
// requests.
func (o OpenRouter) GenerateTitle(ctx context.Context, message string) (string, error) {
	msgs := []models.Message{
		{
			Role: models.RoleUser,
			Contents: []models.Content{
				{
					Type: models.ContentTypeText,
					Text: message,
				},
			},
		},
	}

	resp, err := o.doRequest(ctx, msgs, nil, false)
	if err != nil {
		return "", fmt.Errorf("error sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(body))
	}

	var res openRouterResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", fmt.Errorf("error decoding response: %w", err)
	}

	if len(res.Choices) == 0 {
		return "", errors.New("no choices found")
	}

	return res.Choices[0].Message.Content, nil
}

func (o OpenRouter) doRequest(
	ctx context.Context,
	messages []models.Message,
	tools []mcp.Tool,
	stream bool,
) (*http.Response, error) {
	msgs := make([]openRouterMessage, 0, len(messages))
	for _, msg := range messages {
		for _, ct := range msg.Contents {
			switch ct.Type {
			case models.ContentTypeText:
				if ct.Text != "" {
					msgs = append(msgs, openRouterMessage{
						Role:    string(msg.Role),
						Content: ct.Text,
					})
				}
			case models.ContentTypeCallTool:
				msgs = append(msgs, openRouterMessage{
					Role: "assistant",
					ToolCalls: []openRouterToolCalls{
						{
							ID:   ct.CallToolID,
							Type: "function",
							Function: openRouterToolCallFunction{
								Name:      ct.ToolName,
								Arguments: string(ct.ToolInput),
							},
						},
					},
				})
			case models.ContentTypeToolResult:
				msgs = append(msgs, openRouterMessage{
					Role:       "tool",
					ToolCallID: ct.CallToolID,
					Content:    string(ct.ToolResult),
				})
			}
		}
	}
	msgs = slices.Insert(msgs, 0, openRouterMessage{
		Role:    "system",
		Content: o.systemPrompt,
	})

	oTools := make([]openRouterTool, len(tools))
	for i, tool := range tools {
		oTools[i] = openRouterTool{
			Type: "function",
			Function: openRouterToolFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.InputSchema,
			},
		}
	}

	reqBody := openRouterChatRequest{
		Model:    o.model,
		Messages: msgs,
		Stream:   stream,
		Tools:    oTools,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("error marshaling request: %w", err)
	}

	o.logger.Debug("Request Body", slog.String("body", string(jsonBody)))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		openRouterAPIEndpoint+"/chat/completions", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.apiKey)
	req.Header.Set("HTTP-Referer", "https://github.com/MegaGrindStone/mcp-web-ui/")
	req.Header.Set("X-Title", "MCP Web UI")

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code: %d, body: %s, request: %s", resp.StatusCode, string(body), jsonBody)
	}

	return resp, nil
}
