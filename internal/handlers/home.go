package handlers

import (
	"html/template"
	"net/http"
	"time"

	mcpwebui "github.com/MegaGrindStone/mcp-web-ui"
	"github.com/MegaGrindStone/mcp-web-ui/internal/models"
)

type Home struct {
	templates *template.Template
}

type homePageData struct {
	Messages []models.Message
}

func NewHome() (Home, error) {
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
		templates: tmpl,
	}, nil
}

func (h Home) HandleHome(w http.ResponseWriter, r *http.Request) {
	messages := []models.Message{
		{
			Role:      "user",
			Content:   "Hello, how are you?",
			Timestamp: time.Now(),
		},
		{
			Role:      "assistant",
			Content:   "I'm doing well, thank you for asking. How about you?",
			Timestamp: time.Now(),
		},
	}
	data := homePageData{
		Messages: messages,
	}

	err := h.templates.ExecuteTemplate(w, "home.html", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (h Home) HandleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse the message from form
	message := r.FormValue("message")
	if message == "" {
		http.Error(w, "Message is required", http.StatusBadRequest)
		return
	}

	// Create user message
	userMsg := models.Message{
		Role:      "user",
		Content:   message,
		Timestamp: time.Now(),
	}

	// Get AI response (implement your AI service call here)
	aiResponse := models.Message{
		Role:      "assistant",
		Content:   "This is a sample AI response",
		Timestamp: time.Now(),
	}

	// First, render the user message
	err := h.templates.ExecuteTemplate(w, "chat_message", userMsg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Then render the AI response
	err = h.templates.ExecuteTemplate(w, "chat_message", aiResponse)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}
