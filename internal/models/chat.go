package models

import (
	"bytes"
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

// RenderContents renders a slice of Content into a string. If withDetail is true, it will render the contents
// of call tools input and result wrapped with <details> tags.
func RenderContents(contents []Content, withDetail bool) string {
	var sb strings.Builder
	for _, content := range contents {
		switch content.Type {
		case ContentTypeText:
			if content.Text == "" {
				continue
			}
			sb.WriteString(content.Text)
		case ContentTypeCallTool:
			sb.WriteString("  \n\n")
			sb.WriteString(fmt.Sprintf("Calling Tool: %s  \n", content.ToolName))
			if withDetail {
				sb.WriteString("<details>  \n\n")
			}
			sb.WriteString("Input:  \n")

			var prettyJSON bytes.Buffer
			input := string(content.ToolInput)
			if err := json.Indent(&prettyJSON, content.ToolInput, "", "  "); err == nil {
				input = prettyJSON.String()
			}

			sb.WriteString(fmt.Sprintf("```json  \n%s  \n```  \n", input))
		case ContentTypeToolResult:
			sb.WriteString("  \n\n")
			sb.WriteString("Result:  \n")

			var prettyJSON bytes.Buffer
			result := string(content.ToolResult)
			if err := json.Indent(&prettyJSON, content.ToolResult, "", "  "); err == nil {
				result = prettyJSON.String()
			}
			sb.WriteString(fmt.Sprintf("```json  \n%s  \n```  \n", result))
			if withDetail {
				sb.WriteString("</details>  \n")
			}
		}
	}
	return sb.String()
}
