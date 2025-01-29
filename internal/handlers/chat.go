package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/MegaGrindStone/go-mcp"
	"github.com/MegaGrindStone/mcp-web-ui/internal/models"
	"github.com/google/uuid"
	"github.com/tmaxmax/go-sse"
)

type chat struct {
	ID    string
	Title string

	Active bool
}

type message struct {
	ID        string
	Role      string
	Content   string
	Timestamp time.Time

	StreamingState string
}

// SSE event types for real-time updates.
var (
	chatsSSEType    = sse.Type("chats")
	messagesSSEType = sse.Type("messages")
)

func callToolError(err error) json.RawMessage {
	ctErr := struct {
		Error string `json:"error"`
	}{
		Error: err.Error(),
	}

	res, _ := json.Marshal(ctErr)
	return res
}

// HandleChats processes chat interactions through HTTP POST requests,
// managing both new chat creation and message handling. It accepts user messages through form data,
// creates appropriate chat contexts, and initiates asynchronous processing for AI responses and chat title generation.
//
// The handler expects a "message" form field and an optional "chat_id" field.
// If no chat_id is provided, it creates a new chat session. The handler streams AI responses through
// Server-Sent Events (SSE) and updates the UI accordingly through template rendering.
//
// The function returns appropriate HTTP error responses for invalid methods, missing required fields,
// or internal processing errors. For successful requests, it renders either a complete chatbox template
// for new chats or individual message templates for existing chats.
func (m Main) HandleChats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	msg := r.FormValue("message")
	if msg == "" {
		http.Error(w, "Message is required", http.StatusBadRequest)
		return
	}

	var err error

	chatID := r.FormValue("chat_id")
	// We track if this is a new chat to determine the appropriate template rendering strategy
	isNewChat := false
	if chatID == "" {
		chatID, err = m.newChat()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		isNewChat = true
	}

	// We create two messages: user's input and a placeholder for AI response
	um := models.Message{
		ID:   uuid.New().String(),
		Role: models.RoleUser,
		Contents: []models.Content{
			{
				Type: models.ContentTypeText,
				Text: msg,
			},
		},
		Timestamp: time.Now(),
	}
	userMsgID, err := m.store.AddMessage(r.Context(), chatID, um)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Initialize empty AI message to be streamed later
	am := models.Message{
		ID:        uuid.New().String(),
		Role:      models.RoleAssistant,
		Timestamp: time.Now(),
	}
	aiMsgID, err := m.store.AddMessage(r.Context(), chatID, am)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	messages, err := m.store.Messages(r.Context(), chatID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Start async processes for chat response and title generation
	go m.chat(chatID, messages)

	if isNewChat {
		go m.generateChatTitle(chatID, msg)

		// For new chats, we prepare all messages with appropriate streaming states
		msgs := make([]message, len(messages))
		for i := range messages {
			// Mark only the AI message as "loading", others as "ended"
			streamingState := "ended"
			if messages[i].ID == aiMsgID {
				streamingState = "loading"
			}
			msgs[i] = message{
				ID:             messages[i].ID,
				Role:           string(messages[i].Role),
				Content:        models.RenderContents(messages[i].Contents),
				Timestamp:      messages[i].Timestamp,
				StreamingState: streamingState,
			}
		}

		data := homePageData{
			CurrentChatID: chatID,
			Messages:      msgs,
		}
		err = m.templates.ExecuteTemplate(w, "chatbox", data)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	err = m.templates.ExecuteTemplate(w, "user_message", message{
		ID:             userMsgID,
		Role:           string(um.Role),
		Content:        models.RenderContents(um.Contents),
		Timestamp:      um.Timestamp,
		StreamingState: "ended",
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	err = m.templates.ExecuteTemplate(w, "ai_message", message{
		ID:             aiMsgID,
		Role:           string(am.Role),
		Content:        models.RenderContents(am.Contents),
		Timestamp:      am.Timestamp,
		StreamingState: "loading",
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (m Main) newChat() (string, error) {
	newChat := models.Chat{
		ID: uuid.New().String(),
	}
	newChatID, err := m.store.AddChat(context.Background(), newChat)
	if err != nil {
		return "", fmt.Errorf("failed to add chat: %w", err)
	}
	newChat.ID = newChatID

	divs, err := m.chatDivs(newChat.ID)
	if err != nil {
		return "", fmt.Errorf("failed to create chat divs: %w", err)
	}

	msg := sse.Message{
		Type: chatsSSEType,
	}
	msg.AppendData(divs)

	if err := m.sseSrv.Publish(&msg, chatsSSETopic); err != nil {
		return "", fmt.Errorf("failed to publish chats: %w", err)
	}

	return newChat.ID, nil
}

func (m Main) chat(chatID string, messages []models.Message) {
	// Ensure SSE connection cleanup on function exit
	defer func() {
		e := &sse.Message{Type: sse.Type("closeMessage")}
		e.AppendData("bye")
		_ = m.sseSrv.Publish(e)
	}()

	aiMsg := messages[len(messages)-1]
	contentIdx := -1

	for {
		it := m.llm.Chat(context.Background(), messages, m.tools)
		aiMsg.Contents = append(aiMsg.Contents, models.Content{
			Type: models.ContentTypeText,
			Text: "",
		})
		contentIdx++
		callTool := false

		for content, err := range it {
			msg := sse.Message{
				Type: messagesSSEType,
			}
			if err != nil {
				msg.AppendData(err.Error())
				_ = m.sseSrv.Publish(&msg, messageIDTopic(aiMsg.ID))
				return
			}

			switch content.Type {
			case models.ContentTypeText:
				aiMsg.Contents[contentIdx].Text += content.Text
			case models.ContentTypeCallTool:
				callTool = true
				aiMsg.Contents = append(aiMsg.Contents, content)
				contentIdx++
			case models.ContentTypeToolResult:
				log.Printf("Content type tool results is not allowed")
				return
			}

			if err := m.store.UpdateMessage(context.Background(), chatID, aiMsg); err != nil {
				log.Printf("Failed to save message content: %v", err)
				return
			}

			msg.AppendData(fmt.Sprintf("<zero-md><script type=\"text/markdown\">%s</script></zero-md>",
				models.RenderContents(aiMsg.Contents)))
			if err := m.sseSrv.Publish(&msg, messageIDTopic(aiMsg.ID)); err != nil {
				log.Printf("Failed to publish message: %v", err)
				return
			}

			if callTool {
				break
			}
		}

		if !callTool {
			break
		}

		callToolContent := aiMsg.Contents[len(aiMsg.Contents)-1]

		toolResContent := models.Content{
			Type:       models.ContentTypeToolResult,
			CallToolID: callToolContent.CallToolID,
		}

		clientIdx, ok := m.toolsMap[callToolContent.ToolName]
		if !ok {
			toolResContent.ToolResult = callToolError(fmt.Errorf("tool %s is not found", callToolContent.ToolName))
			toolResContent.CallToolFailed = true
			aiMsg.Contents = append(aiMsg.Contents, toolResContent)
			contentIdx++
			messages[len(messages)-1] = aiMsg
			continue
		}

		toolRes, err := m.mcpClients[clientIdx].CallTool(context.Background(), mcp.CallToolParams{
			Name:      callToolContent.ToolName,
			Arguments: callToolContent.ToolInput,
		})
		if err != nil {
			toolResContent.ToolResult = callToolError(fmt.Errorf("tool call failed: %w", err))
			toolResContent.CallToolFailed = true
			aiMsg.Contents = append(aiMsg.Contents, toolResContent)
			contentIdx++
			messages[len(messages)-1] = aiMsg
			continue
		}

		resContent, err := json.Marshal(toolRes.Content)
		if err != nil {
			toolResContent.ToolResult = callToolError(fmt.Errorf("failed to marshal content: %w", err))
			toolResContent.CallToolFailed = true
			aiMsg.Contents = append(aiMsg.Contents, toolResContent)
			contentIdx++
			messages[len(messages)-1] = aiMsg
			continue
		}

		toolResContent.ToolResult = resContent
		toolResContent.CallToolFailed = toolRes.IsError
		aiMsg.Contents = append(aiMsg.Contents, toolResContent)
		contentIdx++
		messages[len(messages)-1] = aiMsg
	}
}

func (m Main) generateChatTitle(chatID string, message string) {
	title, err := m.titleGenerator.GenerateTitle(context.Background(), message)
	if err != nil {
		log.Printf("Error generating chat title: %v", err)
		return
	}

	updatedChat := models.Chat{
		ID:    chatID,
		Title: title,
	}
	if err := m.store.UpdateChat(context.Background(), updatedChat); err != nil {
		log.Printf("Failed to update chat title: %v", err)
		return
	}

	divs, err := m.chatDivs(chatID)
	if err != nil {
		log.Printf("Failed to generate chat title: %v", err)
		return
	}

	msg := sse.Message{
		Type: chatsSSEType,
	}
	msg.AppendData(divs)
	if err := m.sseSrv.Publish(&msg, chatsSSETopic); err != nil {
		log.Printf("Failed to publish chats: %v", err)
	}
}

func (m Main) chatDivs(activeID string) (string, error) {
	chats, err := m.store.Chats(context.Background())
	if err != nil {
		return "", fmt.Errorf("failed to get chats: %w", err)
	}

	var sb strings.Builder
	for _, ch := range chats {
		err := m.templates.ExecuteTemplate(&sb, "chat_title", chat{
			ID:     ch.ID,
			Title:  ch.Title,
			Active: ch.ID == activeID,
		})
		if err != nil {
			return "", fmt.Errorf("failed to execute chat_title template: %w", err)
		}
	}
	return sb.String(), nil
}
