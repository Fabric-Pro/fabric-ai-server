package anthropic

import (
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/danielmiessler/fabric/internal/plugins"
)

// NewClientWithCredentials creates an Anthropic client with explicit API credentials.
// This is used for delegated mode where credentials come from token exchange.
func NewClientWithCredentials(apiKey string, baseUrl string) *Client {
	ret := &Client{}

	ret.PluginBase = &plugins.PluginBase{
		Name:          "Anthropic",
		EnvNamePrefix: "ANTHROPIC",
	}

	ret.ApiKey = &plugins.SetupQuestion{Setting: &plugins.Setting{Value: apiKey}}
	ret.ApiBaseURL = &plugins.SetupQuestion{Setting: &plugins.Setting{Value: baseUrl}}
	ret.UseOAuth = &plugins.SetupQuestion{Setting: &plugins.Setting{Value: "false"}}

	ret.maxTokens = 4096
	ret.defaultRequiredUserMessage = "Hi"
	ret.models = []string{
		string(anthropic.ModelClaude3_7SonnetLatest), string(anthropic.ModelClaude3_7Sonnet20250219),
		string(anthropic.ModelClaude3_5HaikuLatest), string(anthropic.ModelClaude3_5Haiku20241022),
		string(anthropic.ModelClaude3OpusLatest), string(anthropic.ModelClaude_3_Opus_20240229),
		string(anthropic.ModelClaude_3_Haiku_20240307),
		string(anthropic.ModelClaudeOpus4_20250514), string(anthropic.ModelClaudeSonnet4_20250514),
		string(anthropic.ModelClaudeOpus4_1_20250805),
		string(anthropic.ModelClaudeSonnet4_5),
		string(anthropic.ModelClaudeSonnet4_5_20250929),
		string(anthropic.ModelClaudeOpus4_5_20251101),
		string(anthropic.ModelClaudeOpus4_5),
		string(anthropic.ModelClaudeHaiku4_5),
		string(anthropic.ModelClaudeHaiku4_5_20251001),
	}

	ret.modelBetas = map[string][]string{
		string(anthropic.ModelClaudeSonnet4_20250514):   {"context-1m-2025-08-07"},
		string(anthropic.ModelClaudeSonnet4_5):          {"context-1m-2025-08-07"},
		string(anthropic.ModelClaudeSonnet4_5_20250929): {"context-1m-2025-08-07"},
	}

	// Build client with options
	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if baseUrl != "" {
		opts = append(opts, option.WithBaseURL(baseUrl))
	} else {
		opts = append(opts, option.WithBaseURL(defaultBaseUrl))
	}

	ret.client = anthropic.NewClient(opts...)

	return ret
}
