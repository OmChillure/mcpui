package handlers

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"iter"
	"log"
	"net/http"
	"slices"
	"strings"
	"time"

	mcpwebui "github.com/MegaGrindStone/mcp-web-ui"
	"github.com/MegaGrindStone/mcp-web-ui/internal/models"
	"github.com/google/uuid"
	"github.com/tmaxmax/go-sse"
	"github.com/yuin/goldmark"
)

type LLM interface {
	Chat(ctx context.Context, messages []models.Message) iter.Seq2[string, error]
}

type Store interface {
	Chats(ctx context.Context) ([]models.Chat, error)
	AddChat(ctx context.Context, chat models.Chat) (string, error)
	SetChatTitle(ctx context.Context, chatID string, title string) error

	Messages(ctx context.Context, chatID string) ([]models.Message, error)
	AddMessages(ctx context.Context, chatID string, messages []models.Message) error
	UpdateMessage(ctx context.Context, chatID string, message models.Message) error
}

type Home struct {
	sseSrv    *sse.Server
	templates *template.Template

	llm   LLM
	store Store
}

type homePageData struct {
	Chats         []models.Chat
	Messages      []models.Message
	CurrentChatID string
}

const chatsSSETopic = "chats"

var (
	chatsSSEType    = sse.Type("chats")
	messagesSSEType = sse.Type("messages")
)

func NewHome(llm LLM, store Store) (Home, error) {
	tmpl, err := template.ParseFS(
		mcpwebui.TemplateFS,
		"templates/layout/*.html",
		"templates/pages/*.html",
		"templates/partials/*.html",
	)
	if err != nil {
		return Home{}, err
	}

	return Home{
		sseSrv: &sse.Server{
			OnSession: func(s *sse.Session) (sse.Subscription, bool) {
				topics := []string{sse.DefaultTopic, chatsSSETopic}

				messageID := s.Req.URL.Query().Get("message_id")
				if messageID != "" {
					topics = append(topics, messageIDTopic(messageID))
				}

				return sse.Subscription{
					Client:      s,
					LastEventID: s.LastEventID,
					Topics:      topics,
				}, true
			},
		},
		templates: tmpl,
		llm:       llm,
		store:     store,
	}, nil
}

func messageIDTopic(messageID string) string {
	return fmt.Sprintf("message-%s", messageID)
}

func (h Home) HandleHome(w http.ResponseWriter, r *http.Request) {
	chats, err := h.store.Chats(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	for i := range chats {
		chats[i].Active = false
	}

	currentChatID := ""
	var messages []models.Message
	if r.URL.Query().Get("chat_id") != "" {
		currentChatID = r.URL.Query().Get("chat_id")
		idx := slices.IndexFunc(chats, func(c models.Chat) bool {
			return c.ID == currentChatID
		})
		if idx >= 0 {
			chats[idx].Active = true
		}
		messages, err = h.store.Messages(r.Context(), currentChatID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	for i := range messages {
		messages[i].StreamingState = models.StreamingStateEnded
	}
	data := homePageData{
		Chats:         chats,
		Messages:      messages,
		CurrentChatID: currentChatID,
	}

	if err := h.templates.ExecuteTemplate(w, "home.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (h Home) HandleChats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	message := r.FormValue("message")
	if message == "" {
		http.Error(w, "Message is required", http.StatusBadRequest)
		return
	}

	var err error

	chatID := r.FormValue("chat_id")
	isNewChat := false
	if chatID == "" {
		chatID, err = h.newChat()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		isNewChat = true
	}

	userMsg := models.Message{
		ID:        uuid.New().String(),
		Role:      "user",
		Content:   message,
		Timestamp: time.Now(),
	}

	aiMsg := models.Message{
		ID:             uuid.New().String(),
		Role:           "assistant",
		Timestamp:      time.Now(),
		StreamingState: models.StreamingStateLoading,
	}

	if err := h.store.AddMessages(r.Context(), chatID, []models.Message{userMsg, aiMsg}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	messages, err := h.store.Messages(r.Context(), chatID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	userMsg = messages[len(messages)-2]
	aiMsg = messages[len(messages)-1]

	go h.chat(chatID, messages)

	if isNewChat {
		go h.generateChatTitle(chatID, messages)

		data := homePageData{
			CurrentChatID: chatID,
			Messages:      messages,
		}
		err = h.templates.ExecuteTemplate(w, "chatbox", data)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	err = h.templates.ExecuteTemplate(w, "user_message", userMsg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	err = h.templates.ExecuteTemplate(w, "ai_message", aiMsg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h Home) HandleSSE(w http.ResponseWriter, r *http.Request) {
	h.sseSrv.ServeHTTP(w, r)
}

func (h Home) Shutdown(ctx context.Context) error {
	e := &sse.Message{Type: sse.Type("closeChat")}
	// Adding data is necessary because spec-compliant clients
	// do not dispatch events without data.
	e.AppendData("bye")
	// Broadcast a close message so clients can gracefully disconnect.
	_ = h.sseSrv.Publish(e)

	ctx, cancel := context.WithTimeout(ctx, time.Second*5)
	defer cancel()

	// We use a context with a timeout so the program doesn't wait indefinitely
	// for connections to terminate. There may be misbehaving connections
	// which may hang for an unknown timespan, so we just stop waiting on Shutdown
	// after a certain duration.
	return h.sseSrv.Shutdown(ctx)
}

func (h Home) newChat() (string, error) {
	newChat := models.Chat{
		ID:     uuid.New().String(),
		Active: true,
	}
	newChatID, err := h.store.AddChat(context.Background(), newChat)
	if err != nil {
		return "", fmt.Errorf("failed to add chat: %w", err)
	}
	newChat.ID = newChatID

	divs, err := h.chatDivs(newChat.ID)
	if err != nil {
		return "", fmt.Errorf("failed to create chat divs: %w", err)
	}

	msg := sse.Message{
		Type: chatsSSEType,
	}
	msg.AppendData(divs)

	if err := h.sseSrv.Publish(&msg, chatsSSETopic); err != nil {
		return "", fmt.Errorf("failed to publish chats: %w", err)
	}

	return newChat.ID, nil
}

func (h Home) chat(chatID string, messages []models.Message) {
	defer func() {
		e := &sse.Message{Type: sse.Type("closeMessage")}
		e.AppendData("bye")
		_ = h.sseSrv.Publish(e)
	}()

	aiMsg := messages[len(messages)-1]

	it := h.llm.Chat(context.Background(), messages)

	for streamMsg, err := range it {
		msg := sse.Message{
			Type: messagesSSEType,
		}
		if err != nil {
			msg.AppendData(fmt.Sprintf("<p class='mb-0'>%s</p>", err.Error()))
			_ = h.sseSrv.Publish(&msg, messageIDTopic(aiMsg.ID))
			return
		}

		buf := new(bytes.Buffer)
		aiMsg.Content += streamMsg

		if err := goldmark.Convert([]byte(aiMsg.Content), buf); err != nil {
			log.Printf("Error converting markdown: %v", err)
			return
		}

		if err := h.store.UpdateMessage(context.Background(), chatID, aiMsg); err != nil {
			log.Printf("Failed to save message content: %v", err)
			return
		}

		msg.AppendData(fmt.Sprintf("<p class='mb-0'>%s</p>", buf))
		if err := h.sseSrv.Publish(&msg, messageIDTopic(aiMsg.ID)); err != nil {
			log.Printf("Failed to publish message: %v", err)
			return
		}
	}

	aiMsg.StreamingState = models.StreamingStateEnded
	if err := h.store.UpdateMessage(context.Background(), chatID, aiMsg); err != nil {
		log.Printf("Failed to save message content: %v", err)
		return
	}
}

func (h Home) generateChatTitle(chatID string, messages []models.Message) {
	var msgs []models.Message
	msgs = append(msgs, models.Message{
		Role:    "system",
		Content: "Generate a title for this chat with only one sentence with maximum 5 words.",
	})
	msgs = append(msgs, messages...)

	it := h.llm.Chat(context.Background(), msgs)

	title := ""
	for msg, err := range it {
		if err != nil {
			log.Printf("Error generating chat title: %v", err)
			return
		}
		title += msg
	}

	if err := h.store.SetChatTitle(context.Background(), chatID, title); err != nil {
		log.Printf("Failed to set chat title: %v", err)
		return
	}

	divs, err := h.chatDivs(chatID)
	if err != nil {
		log.Printf("Failed to generate chat title: %v", err)
		return
	}

	msg := sse.Message{
		Type: chatsSSEType,
	}
	msg.AppendData(divs)
	if err := h.sseSrv.Publish(&msg, chatsSSETopic); err != nil {
		log.Printf("Failed to publish chats: %v", err)
	}
}

func (h Home) chatDivs(activeID string) (string, error) {
	chats, err := h.store.Chats(context.Background())
	if err != nil {
		return "", fmt.Errorf("failed to get chats: %w", err)
	}

	var sb strings.Builder
	for _, chat := range chats {
		chat.Active = chat.ID == activeID
		err := h.templates.ExecuteTemplate(&sb, "chat_title", chat)
		if err != nil {
			return "", fmt.Errorf("failed to execute chat_title template: %w", err)
		}
	}
	return sb.String(), nil
}
