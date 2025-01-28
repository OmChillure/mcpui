package models

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Chat represents a conversation container in the chat system. It provides basic identification and
// labeling capabilities for organizing message threads.
type Chat struct {
	ID    string
	Title string
}

// Message represents an individual communication entry within a chat. It contains the core components
// of a chat message including its unique identifier, the participant's role, the actual content, and
// the precise time when the message was created.
type Message struct {
	ID        string
	Role      string
	Contents  []Content
	Timestamp time.Time
}

// Content is a message content with its type.
type Content struct {
	Type ContentType

	// Text would contain a normal text if Type is ContentTypeText, and it would be the tool result
	// if Type is ContentTypeToolResult, otherwise it would be empty.
	Text string

	// ToolName would be filled if Type is ContentTypeCallTool.
	ToolName string
	// ToolInput would be filled if Type is ContentTypeCallTool.
	ToolInput json.RawMessage

	// CallToolID would be filled if Type is ContentTypeCallTool or ContentTypeToolResult.
	CallToolID string
}

// ContentType represents the type of content in messages.
type ContentType string

const (
	// ContentTypeText represents text content.
	ContentTypeText ContentType = "text"
	// ContentTypeCallTool represents a call to a tool.
	ContentTypeCallTool ContentType = "call_tool"
	// ContentTypeToolResult represents the result of a tool call.
	ContentTypeToolResult ContentType = "tool_result"
)

// RenderContents renders a slice of Content into a string.
func RenderContents(contents []Content) string {
	var sb strings.Builder
	for _, content := range contents {
		switch content.Type {
		case ContentTypeText:
			sb.WriteString(content.Text)
		case ContentTypeCallTool:
			sb.WriteString(fmt.Sprintf("\nCalling Tool: %s\nInput: %s\n", content.ToolName, content.ToolInput))
		case ContentTypeToolResult:
			sb.WriteString(fmt.Sprintf("Tool: %s\nResult: %s\n", content.ToolName, content.Text))
		}
	}
	return sb.String()
}
