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
	Role      Role
	Contents  []Content
	Timestamp time.Time
}

// Content is a message content with its type.
type Content struct {
	Type ContentType

	// Text would be filled if Type is ContentTypeText.
	Text string

	// ToolName would be filled if Type is ContentTypeCallTool.
	ToolName string
	// ToolInput would be filled if Type is ContentTypeCallTool.
	ToolInput json.RawMessage

	// ToolResult would be filled if Type is ContentTypeToolResult. The value would be either tool result or error.
	ToolResult json.RawMessage

	// CallToolID would be filled if Type is ContentTypeCallTool or ContentTypeToolResult.
	CallToolID string
	// CallToolFailed is a flag indicating if the call tool failed.
	// This flag would be set to true if the call tool failed and Type is ContentTypeToolResult.
	CallToolFailed bool
}

// Role represents the role of a message participant.
type Role string

// ContentType represents the type of content in messages.
type ContentType string

const (
	// RoleUser represents a user message. A message with this role would only contain text content.
	RoleUser Role = "user"
	// RoleAssistant represents an assistant message. A message with this role would contain text content
	// and potentially other types of content.
	RoleAssistant Role = "assistant"

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
	idx := 0
	for idx < len(contents) {
		content := contents[idx]
		switch content.Type {
		case ContentTypeText:
			if content.Text == "" {
				idx++
				continue
			}
			sb.WriteString(content.Text)
			idx++
			if idx >= len(contents) {
				break
			}
			nextContent := contents[idx]
			if nextContent.Type != ContentTypeCallTool {
				return fmt.Sprintf("invalid content type %s at index %d, want %s", nextContent.Type, idx, ContentTypeCallTool)
			}
			sb.WriteString(fmt.Sprintf(`  
        Calling Tool: %s  
        Input: %s`, nextContent.ToolName, nextContent.ToolInput))
			idx++
			if idx >= len(contents) {
				break
			}
			nextContent = contents[idx]
			if nextContent.Type != ContentTypeToolResult {
				return fmt.Sprintf("invalid content type %s at index %d, want %s", nextContent.Type, idx, ContentTypeToolResult)
			}
			sb.WriteString(fmt.Sprintf(`  
        Result: %s  
        `, nextContent.ToolResult))
			idx++
		case ContentTypeCallTool:
			sb.WriteString(fmt.Sprintf(`Calling Tool: %s  
    Input: %s`, content.ToolName, content.ToolInput))
			idx++
			if idx >= len(contents) {
				break
			}
			nextContent := contents[idx]
			if nextContent.Type != ContentTypeToolResult {
				return fmt.Sprintf("invalid content type %s at index %d, want %s", nextContent.Type, idx, ContentTypeToolResult)
			}
			sb.WriteString(fmt.Sprintf(`  
        Result: %s  
        `, nextContent.ToolResult))
			idx++
		case ContentTypeToolResult:
			return fmt.Sprintf("unexpected content type %s at index %d", content.Type, idx)
		}
	}
	return sb.String()
}
