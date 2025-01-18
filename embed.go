package mcpwebui

import "embed"

// TemplateFS contains the embedded HTML templates used for rendering the web interface. These templates
// are organized in a directory structure that separates layouts, pages, and partial views.
//
//go:embed templates/*
var TemplateFS embed.FS

// StaticFS contains the embedded static assets such as JavaScript, CSS, and image files required for
// the web interface's functionality and styling.
//
//go:embed static/*
var StaticFS embed.FS
