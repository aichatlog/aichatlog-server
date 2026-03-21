package llm

import "fmt"

// ExtractionResult is the structured output from LLM knowledge extraction.
type ExtractionResult struct {
	Summary      string           `json:"summary"`
	WorkLogEntry string           `json:"work_log_entry"`
	TechSolutions []TechSolution  `json:"tech_solutions,omitempty"`
	Concepts     []Concept        `json:"concepts,omitempty"`
	Prompts      []PromptTemplate `json:"prompts,omitempty"`
}

type TechSolution struct {
	Title   string   `json:"title"`
	Problem string   `json:"problem"`
	Solution string  `json:"solution"`
	Code    string   `json:"code,omitempty"`
	Gotchas []string `json:"gotchas,omitempty"`
	Tags    []string `json:"tags,omitempty"`
}

type Concept struct {
	Title         string   `json:"title"`
	Explanation   string   `json:"explanation"`
	WhyItMatters  string   `json:"why_it_matters"`
	Related       []string `json:"related,omitempty"`
}

type PromptTemplate struct {
	Title        string `json:"title"`
	PromptText   string `json:"prompt_text"`
	WhenToUse    string `json:"when_to_use"`
	WhyEffective string `json:"why_effective"`
}

// Adapter is the interface for LLM providers.
type Adapter interface {
	// Name returns the adapter identifier (e.g. "anthropic", "openai").
	Name() string

	// Extract sends a prompt to the LLM and returns the raw response text.
	Extract(systemPrompt, userPrompt string) (string, error)
}

// Config holds LLM adapter configuration.
type Config struct {
	Adapter  string         `json:"adapter"` // "anthropic", "openai", "none"
	Anthropic *AnthropicConfig `json:"anthropic,omitempty"`
	OpenAI   *OpenAIConfig    `json:"openai,omitempty"`

	// Extraction settings
	MinWords              int     `json:"min_words"`
	ModelUpgradeThreshold int     `json:"model_upgrade_threshold"`
	MaxMonthlyBudget      float64 `json:"max_monthly_budget"`
}

// AnthropicConfig for Claude API.
type AnthropicConfig struct {
	APIKey string `json:"api_key"`
	Model  string `json:"model"`  // default: claude-haiku-4-5-20251001
	BigModel string `json:"big_model"` // for long conversations: claude-sonnet-4-20250514
}

// OpenAIConfig for OpenAI-compatible APIs.
type OpenAIConfig struct {
	APIKey  string `json:"api_key"`
	BaseURL string `json:"base_url"` // default: https://api.openai.com/v1
	Model   string `json:"model"`    // default: gpt-4o-mini
	BigModel string `json:"big_model"` // default: gpt-4o
}

// NewAdapter creates an LLM adapter from configuration.
func NewAdapter(cfg *Config) (Adapter, error) {
	if cfg == nil {
		return nil, nil
	}
	switch cfg.Adapter {
	case "anthropic":
		if cfg.Anthropic == nil {
			return nil, fmt.Errorf("anthropic config is required")
		}
		return NewAnthropicAdapter(cfg.Anthropic), nil
	case "openai":
		if cfg.OpenAI == nil {
			return nil, fmt.Errorf("openai config is required")
		}
		return NewOpenAIAdapter(cfg.OpenAI), nil
	case "", "none":
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown LLM adapter: %s", cfg.Adapter)
	}
}
