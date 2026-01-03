package core

import (
	"fmt"
	"strings"

	"github.com/danielmiessler/fabric/internal/chat"
	"github.com/danielmiessler/fabric/internal/domain"
	"github.com/danielmiessler/fabric/internal/plugins/ai"
	"github.com/danielmiessler/fabric/internal/plugins/ai/anthropic"
	"github.com/danielmiessler/fabric/internal/plugins/ai/dryrun"
	"github.com/danielmiessler/fabric/internal/plugins/ai/openai"
)

// GetChatterWithCredentials creates a Chatter using explicitly provided API credentials.
// This is used for delegated mode where the API key comes from token exchange.
//
// Parameters:
//   - model: The AI model to use (e.g., "gpt-4o", "claude-3-sonnet")
//   - modelContextLength: Maximum context length for the model
//   - vendorName: The AI provider name (e.g., "openai", "anthropic")
//   - apiKey: The API key obtained from token exchange
//   - baseUrl: Optional custom base URL for the API (empty string uses provider default)
//   - strategy: Optional strategy name
//   - stream: Whether to stream responses
//   - dryRun: If true, use a mock client that doesn't make real API calls
func (o *PluginRegistry) GetChatterWithCredentials(
	model string,
	modelContextLength int,
	vendorName string,
	apiKey string,
	baseUrl string,
	strategy string,
	stream bool,
	dryRun bool,
) (ret *Chatter, err error) {
	ret = &Chatter{
		db:                 o.Db,
		Stream:             stream,
		DryRun:             dryRun,
		modelContextLength: modelContextLength,
		strategy:           strategy,
	}

	if dryRun {
		ret.vendor = dryrun.NewClient()
		ret.model = model
		return ret, nil
	}

	// Create vendor client with delegated credentials
	var vendor ai.Vendor
	vendorNameLower := strings.ToLower(vendorName)

	switch vendorNameLower {
	case "openai":
		vendor = openai.NewClientWithCredentials(apiKey, baseUrl)
	case "anthropic":
		vendor = anthropic.NewClientWithCredentials(apiKey, baseUrl)
	default:
		// For other providers, try to use OpenAI-compatible client
		// Many providers (Azure, Groq, etc.) are OpenAI-compatible
		vendor = openai.NewClientWithCredentials(apiKey, baseUrl)
	}

	if vendor == nil {
		err = fmt.Errorf("failed to create vendor client for %s", vendorName)
		return nil, err
	}

	ret.vendor = vendor
	ret.model = model

	return ret, nil
}

// GetVendorWithCredentials creates a vendor client using explicitly provided API credentials.
// This is used for delegated mode where the API key comes from token exchange and
// the caller wants direct access to the vendor for streaming operations.
//
// Parameters:
//   - vendorName: The AI provider name (e.g., "openai", "anthropic")
//   - apiKey: The API key obtained from token exchange
//   - baseUrl: Optional custom base URL for the API (empty string uses provider default)
//
// Returns:
//   - vendor: The AI vendor client ready for use
//   - err: Error if vendor creation fails
func (o *PluginRegistry) GetVendorWithCredentials(
	vendorName string,
	apiKey string,
	baseUrl string,
) (vendor ai.Vendor, err error) {
	vendorNameLower := strings.ToLower(vendorName)

	switch vendorNameLower {
	case "openai":
		vendor = openai.NewClientWithCredentials(apiKey, baseUrl)
	case "anthropic":
		vendor = anthropic.NewClientWithCredentials(apiKey, baseUrl)
	default:
		// For other providers, try to use OpenAI-compatible client
		// Many providers (Azure, Groq, etc.) are OpenAI-compatible
		vendor = openai.NewClientWithCredentials(apiKey, baseUrl)
	}

	if vendor == nil {
		err = fmt.Errorf("failed to create vendor client for %s", vendorName)
		return nil, err
	}

	return vendor, nil
}

// SendStreamWithCredentials sends a chat request and streams the response directly to a channel.
// This is used for delegated mode where the caller wants true SSE streaming without
// aggregating the response.
//
// Parameters:
//   - vendorName: The AI provider name (e.g., "openai", "anthropic")
//   - apiKey: The API key obtained from token exchange
//   - baseUrl: Optional custom base URL for the API
//   - messages: The chat messages to send
//   - opts: Chat options including model, temperature, etc.
//   - responseChan: Channel to send streaming response chunks
//
// The caller is responsible for closing the responseChan after this function returns.
func (o *PluginRegistry) SendStreamWithCredentials(
	vendorName string,
	apiKey string,
	baseUrl string,
	messages []*chat.ChatCompletionMessage,
	opts *domain.ChatOptions,
	responseChan chan string,
) error {
	vendor, err := o.GetVendorWithCredentials(vendorName, apiKey, baseUrl)
	if err != nil {
		return fmt.Errorf("failed to create vendor: %w", err)
	}

	return vendor.SendStream(messages, opts, responseChan)
}
