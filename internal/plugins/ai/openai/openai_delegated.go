package openai

import (
	"github.com/danielmiessler/fabric/internal/plugins"
	openai "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

// NewClientWithCredentials creates an OpenAI client with explicit API credentials.
// This is used for delegated mode where credentials come from token exchange.
func NewClientWithCredentials(apiKey string, baseUrl string) *Client {
	ret := &Client{
		PluginBase: &plugins.PluginBase{
			Name:          "OpenAI",
			EnvNamePrefix: "OPENAI",
		},
		ApiKey:              &plugins.SetupQuestion{Setting: &plugins.Setting{Value: apiKey}},
		ApiBaseURL:          &plugins.SetupQuestion{Setting: &plugins.Setting{Value: baseUrl}},
		ImplementsResponses: true,
	}

	// Build request options
	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if baseUrl != "" {
		opts = append(opts, option.WithBaseURL(baseUrl))
	} else {
		opts = append(opts, option.WithBaseURL("https://api.openai.com/v1"))
	}

	client := openai.NewClient(opts...)
	ret.ApiClient = &client

	return ret
}
