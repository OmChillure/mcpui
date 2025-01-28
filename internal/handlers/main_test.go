package handlers_test

import (
	"context"
	"fmt"
	"iter"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/MegaGrindStone/mcp-web-ui/internal/handlers"
	"github.com/MegaGrindStone/mcp-web-ui/internal/models"
)

type mockLLM struct {
	responses []string
	err       error
}

type mockStore struct {
	chats    []models.Chat
	messages map[string][]models.Message
	err      error
}

func TestNewMain(t *testing.T) {
	llm := &mockLLM{}
	store := &mockStore{}

	main, err := handlers.NewMain(llm, store, nil)
	if err != nil {
		t.Fatalf("NewMain() error = %v", err)
	}

	if main.Shutdown(context.Background()) != nil {
		t.Error("Shutdown() should not return error")
	}
}

func TestHandleHome(t *testing.T) {
	llm := &mockLLM{}
	store := &mockStore{
		chats: []models.Chat{
			{ID: "1", Title: "Test Chat"},
		},
		messages: map[string][]models.Message{
			"1": {{ID: "1", Role: "user", Contents: []models.Content{
				{
					Type: models.ContentTypeText,
					Text: "Hello",
				},
			}}},
		},
	}

	main, err := handlers.NewMain(llm, store, nil)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		url        string
		wantStatus int
		wantBody   string
	}{
		{
			name:       "Home page without chat",
			url:        "/",
			wantStatus: http.StatusOK,
			wantBody:   "Test Chat", // Should contain chat title
		},
		{
			name:       "Home page with chat",
			url:        "/?chat_id=1",
			wantStatus: http.StatusOK,
			wantBody:   "Hello", // Should contain message content
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.url, nil)
			w := httptest.NewRecorder()

			main.HandleHome(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("HandleHome() status = %v, want %v", w.Code, tt.wantStatus)
			}

			if !strings.Contains(w.Body.String(), tt.wantBody) {
				t.Errorf("HandleHome() body = %v, want to contain %v", w.Body.String(), tt.wantBody)
			}
		})
	}
}

func TestHandleChats(t *testing.T) {
	llm := &mockLLM{responses: []string{"AI response"}}
	store := &mockStore{
		messages: map[string][]models.Message{},
	}

	main, err := handlers.NewMain(llm, store, nil)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		method     string
		message    string
		chatID     string
		wantStatus int
	}{
		{
			name:       "Invalid method",
			method:     http.MethodGet,
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "Empty message",
			method:     http.MethodPost,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "New chat",
			method:     http.MethodPost,
			message:    "Hello",
			wantStatus: http.StatusOK,
		},
		{
			name:       "Existing chat",
			method:     http.MethodPost,
			message:    "Hello",
			chatID:     "1",
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			form := strings.NewReader(
				"message=" + tt.message + "&chat_id=" + tt.chatID,
			)
			req := httptest.NewRequest(tt.method, "/chat", form)
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()

			main.HandleChats(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("HandleChats() status = %v, want %v", w.Code, tt.wantStatus)
			}
		})
	}
}

func (m mockLLM) Chat(_ context.Context, _ string, _ []models.Message) iter.Seq2[models.Content, error] {
	return func(yield func(models.Content, error) bool) {
		if m.err != nil {
			yield(models.Content{}, m.err)
			return
		}
		for _, resp := range m.responses {
			if !yield(models.Content{
				Type: models.ContentTypeText,
				Text: resp,
			}, nil) {
				return
			}
		}
	}
}

func (m *mockStore) Chats(_ context.Context) ([]models.Chat, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.chats, nil
}

func (m *mockStore) AddChat(_ context.Context, chat models.Chat) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	m.chats = append(m.chats, chat)
	return chat.ID, nil
}

func (m *mockStore) UpdateChat(_ context.Context, chat models.Chat) error {
	idx := slices.IndexFunc(m.chats, func(c models.Chat) bool { return c.ID == chat.ID })
	if idx == -1 {
		return fmt.Errorf("chat not found")
	}
	m.chats[idx] = chat
	return m.err
}

func (m *mockStore) Messages(_ context.Context, chatID string) ([]models.Message, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.messages[chatID], nil
}

func (m *mockStore) AddMessage(_ context.Context, chatID string, msg models.Message) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	m.messages[chatID] = append(m.messages[chatID], msg)
	return msg.ID, nil
}

func (m *mockStore) UpdateMessage(_ context.Context, _ string, _ models.Message) error {
	return m.err
}
