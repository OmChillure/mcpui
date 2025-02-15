package services

// LLMParameters contains the optional configuration parameters for LLM services.
//
// Not all parameters are supported by all LLM providers. The parameters are documented in the
// corresponding LLM provider's documentation.
//
// These parameters is taken from OpenRouter documentation:
// https://openrouter.ai/docs/api-reference/parameters
// For more information obout what these parameters do, please refer to it.
type LLMParameters struct {
	Temperature       *float32       `yaml:"temperature"`
	TopP              *float32       `yaml:"topP"`
	TopK              *int           `yaml:"topK"`
	FrequencyPenalty  *float32       `yaml:"frequencyPenalty"`
	PresencePenalty   *float32       `yaml:"presencePenalty"`
	RepetitionPenalty *float32       `yaml:"repetitionPenalty"`
	MinP              *float32       `yaml:"minP"`
	TopA              *float32       `yaml:"topA"`
	Seed              *int           `yaml:"seed"`
	MaxTokens         *int           `yaml:"maxTokens"`
	LogitBias         map[string]int `yaml:"logitBias"`
	Logprobs          *bool          `yaml:"logprobs"`
	TopLogprobs       *int           `yaml:"topLogprobs"`
	Stop              []string       `yaml:"stop"`
	IncludeReasoning  *bool          `yaml:"includeReasoning"`
}
