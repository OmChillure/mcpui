package main

import (
	"fmt"
	"os"

	"github.com/MegaGrindStone/mcp-web-ui/internal/handlers"
	"github.com/MegaGrindStone/mcp-web-ui/internal/services"
	"gopkg.in/yaml.v3"
)

type llmConfig interface {
	LLM() (handlers.LLM, error)
}

// BaseLLMConfig contains the common fields for all LLM configurations.
type BaseLLMConfig struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
}

type config struct {
	Port string    `yaml:"port"`
	LLM  llmConfig `yaml:"llm"`
}

type ollamaConfig struct {
	BaseLLMConfig `yaml:",inline"`
	Host          string `yaml:"host"`
}

type anthropicConfig struct {
	BaseLLMConfig `yaml:",inline"`
	APIKey        string `yaml:"api_key"`
	MaxTokens     int    `yaml:"max_tokens"`
}

func (c *config) UnmarshalYAML(value *yaml.Node) error {
	var rawConfig struct {
		Port string         `yaml:"port"`
		LLM  map[string]any `yaml:"llm"`
	}

	if err := value.Decode(&rawConfig); err != nil {
		return err
	}

	c.Port = rawConfig.Port

	llmProvider, ok := rawConfig.LLM["provider"].(string)
	if !ok {
		return fmt.Errorf("llm provider is required")
	}

	llmRawYAML, err := yaml.Marshal(rawConfig.LLM)
	if err != nil {
		return err
	}

	var llm llmConfig
	switch llmProvider {
	case "ollama":
		llm = &ollamaConfig{}
	case "anthropic":
		llm = &anthropicConfig{}
	default:
		return fmt.Errorf("unknown llm provider: %s", llmProvider)
	}

	if err := yaml.Unmarshal(llmRawYAML, llm); err != nil {
		return err
	}

	c.LLM = llm

	return nil
}

func (o ollamaConfig) LLM() (handlers.LLM, error) {
	if o.Model == "" {
		return nil, fmt.Errorf("model is required")
	}

	host := o.Host
	if host == "" {
		host = os.Getenv("OLLAMA_HOST")
	}
	return services.NewOllama(host, o.Model), nil
}

func (a anthropicConfig) LLM() (handlers.LLM, error) {
	if a.Model == "" {
		return nil, fmt.Errorf("model is required")
	}
	if a.MaxTokens == 0 {
		return nil, fmt.Errorf("max_tokens is required")
	}

	apiKey := a.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	return services.NewAnthropic(apiKey, a.Model, a.MaxTokens), nil
}
