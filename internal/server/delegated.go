package restapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/danielmiessler/fabric/internal/chat"
	"github.com/danielmiessler/fabric/internal/core"
	"github.com/danielmiessler/fabric/internal/domain"
	"github.com/danielmiessler/fabric/internal/plugins/ai/openai"
	"github.com/danielmiessler/fabric/internal/plugins/db/fsdb"
	"github.com/danielmiessler/fabric/internal/plugins/template"
	"github.com/danielmiessler/fabric/internal/tools/converter"
	"github.com/danielmiessler/fabric/internal/tools/jina"
	"github.com/danielmiessler/fabric/internal/tools/mcpcli"
	"github.com/danielmiessler/fabric/internal/tools/youtube"
	"github.com/gin-gonic/gin"
)

// DelegatedHandler handles requests that use secure token exchange for API keys
type DelegatedHandler struct {
	registry *core.PluginRegistry
	db       *fsdb.Db
}

// TokenExchangeResponse represents the response from Fabric's token exchange endpoint
type TokenExchangeResponse struct {
	ApiKey     string `json:"apiKey"`
	Provider   string `json:"provider"`
	Model      string `json:"model"`
	BaseUrl    string `json:"baseUrl,omitempty"`
	ExpiresIn  int    `json:"expiresIn"`
	JinaApiKey string `json:"jinaApiKey,omitempty"` // Jina AI API key for web search/scraping
}

// DelegatedChatRequest extends ChatRequest with optional AI token for delegation
type DelegatedChatRequest struct {
	Prompts            []PromptRequest `json:"prompts"`
	Language           string          `json:"language"`
	domain.ChatOptions                 // Embed the ChatOptions
}

// DelegatedTranscribeRequest represents a request to transcribe audio with delegated credentials
type DelegatedTranscribeRequest struct {
	FileContent string `json:"fileContent" binding:"required"` // Base64 encoded audio file
	FileName    string `json:"fileName" binding:"required"`    // Original filename with extension
	Model       string `json:"model,omitempty"`                // Transcription model (default: whisper-1)
	Split       bool   `json:"split,omitempty"`                // Split large files
}

// DelegatedTranscribeResponse represents the transcription result
type DelegatedTranscribeResponse struct {
	Text string `json:"text"`
}

// DelegatedYouTubeRequest represents a request to get YouTube transcript with delegated auth
type DelegatedYouTubeRequest struct {
	URL        string `json:"url" binding:"required"` // YouTube video URL
	Language   string `json:"language,omitempty"`     // Language code (default: "en")
	Timestamps bool   `json:"timestamps,omitempty"`   // Include timestamps
}

// DelegatedYouTubeResponse represents the YouTube transcript response
type DelegatedYouTubeResponse struct {
	Transcript  string `json:"transcript"`
	VideoId     string `json:"videoId"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
}

// DelegatedScrapeRequest represents a request to scrape a URL with delegated auth
type DelegatedScrapeRequest struct {
	URL string `json:"url" binding:"required"` // URL to scrape
}

// DelegatedScrapeResponse represents the scraped content response
type DelegatedScrapeResponse struct {
	Content string `json:"content"`
	URL     string `json:"url"`
}

// DelegatedSearchRequest represents a request to search the web with delegated auth
type DelegatedSearchRequest struct {
	Question string `json:"question" binding:"required"` // Search question
}

// DelegatedSearchResponse represents the search results response
type DelegatedSearchResponse struct {
	Content  string `json:"content"`
	Question string `json:"question"`
}

// DelegatedPatternRequest represents a request to get a pattern with optional variable substitution
type DelegatedPatternRequest struct {
	Input     string            `json:"input,omitempty"`     // Input for variable substitution
	Variables map[string]string `json:"variables,omitempty"` // Pattern variables
}

// DelegatedPatternResponse represents the pattern content response
type DelegatedPatternResponse struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Pattern     string `json:"pattern"`
}

// =============================================================================
// YouTube Extended Endpoints - Request/Response Types
// =============================================================================

// DelegatedYouTubeMetadataRequest represents a request for YouTube video metadata
type DelegatedYouTubeMetadataRequest struct {
	URL string `json:"url" binding:"required"` // YouTube video URL
}

// DelegatedYouTubeMetadataResponse represents the YouTube metadata response
type DelegatedYouTubeMetadataResponse struct {
	VideoId      string   `json:"videoId"`
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	ChannelTitle string   `json:"channelTitle"`
	PublishedAt  string   `json:"publishedAt"`
	Duration     int      `json:"duration"` // Duration in minutes
	ViewCount    uint64   `json:"viewCount"`
	LikeCount    uint64   `json:"likeCount"`
	Tags         []string `json:"tags,omitempty"`
}

// DelegatedYouTubeCommentsRequest represents a request for YouTube video comments
type DelegatedYouTubeCommentsRequest struct {
	URL string `json:"url" binding:"required"` // YouTube video URL
}

// DelegatedYouTubeCommentsResponse represents the YouTube comments response
type DelegatedYouTubeCommentsResponse struct {
	VideoId  string   `json:"videoId"`
	Comments []string `json:"comments"`
	Count    int      `json:"count"`
}

// DelegatedYouTubePlaylistRequest represents a request for YouTube playlist videos
type DelegatedYouTubePlaylistRequest struct {
	URL string `json:"url" binding:"required"` // YouTube playlist URL
}

// PlaylistVideoInfo represents basic info about a video in a playlist
type PlaylistVideoInfo struct {
	VideoId string `json:"videoId"`
	Title   string `json:"title"`
	URL     string `json:"url"`
}

// DelegatedYouTubePlaylistResponse represents the YouTube playlist response
type DelegatedYouTubePlaylistResponse struct {
	PlaylistId string              `json:"playlistId"`
	Videos     []PlaylistVideoInfo `json:"videos"`
	Count      int                 `json:"count"`
}

// =============================================================================
// HTML Readability Endpoint - Request/Response Types
// =============================================================================

// DelegatedReadabilityRequest represents a request to extract readable content from HTML
type DelegatedReadabilityRequest struct {
	HTML string `json:"html" binding:"required"` // Raw HTML content
	URL  string `json:"url,omitempty"`           // Optional source URL for context
}

// DelegatedReadabilityResponse represents the extracted readable content
type DelegatedReadabilityResponse struct {
	Content string `json:"content"` // Clean, readable text content
	URL     string `json:"url,omitempty"`
}

// =============================================================================
// Template Plugin Endpoint - Request/Response Types
// =============================================================================

// TemplatePluginRequest represents a request to apply template plugins
type TemplatePluginRequest struct {
	Template  string            `json:"template" binding:"required"` // Template with {{plugin:...}} syntax
	Variables map[string]string `json:"variables,omitempty"`         // Variables for substitution
	Input     string            `json:"input,omitempty"`             // Input text for {{input}} placeholder
}

// TemplatePluginResponse represents the processed template output
type TemplatePluginResponse struct {
	Output   string `json:"output"`   // Processed template output
	Template string `json:"template"` // Original template
}

// =============================================================================
// Strategies and Contexts Endpoint - Request/Response Types
// =============================================================================

// StrategyInfo represents a strategy with its content
type StrategyInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Prompt      string `json:"prompt"`
}

// ContextInfo represents a context with its content
type ContextInfo struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// DelegatedStrategiesResponse represents the list of available strategies
type DelegatedStrategiesResponse struct {
	Strategies []StrategyInfo `json:"strategies"`
	Count      int            `json:"count"`
}

// DelegatedContextsResponse represents the list of available contexts
type DelegatedContextsResponse struct {
	Contexts []ContextInfo `json:"contexts"`
	Count    int           `json:"count"`
}

const (
	AITokenHeader       = "X-AI-Token"
	FabricBaseURLEnvVar = "FABRIC_API_URL" // Use same env var as agents for consistency
	DefaultFabricURL    = "http://localhost:3001"
	ExchangeEndpoint    = "/api/ai/keys/exchange"
)

// NewDelegatedHandler creates a new delegated handler and registers routes
func NewDelegatedHandler(r *gin.Engine, registry *core.PluginRegistry, db *fsdb.Db) *DelegatedHandler {
	handler := &DelegatedHandler{
		registry: registry,
		db:       db,
	}

	// Register delegated endpoints (these bypass the standard API key auth)
	delegated := r.Group("/delegated")
	{
		// Chat & AI
		delegated.POST("/chat", handler.HandleDelegatedChat)
		delegated.POST("/transcribe", handler.HandleDelegatedTranscribe)

		// YouTube endpoints
		delegated.POST("/youtube/transcript", handler.HandleDelegatedYouTubeTranscript)
		delegated.POST("/youtube/metadata", handler.HandleDelegatedYouTubeMetadata)
		delegated.POST("/youtube/comments", handler.HandleDelegatedYouTubeComments)
		delegated.POST("/youtube/playlist", handler.HandleDelegatedYouTubePlaylist)

		// Web scraping & search
		delegated.POST("/scrape", handler.HandleDelegatedScrape)
		delegated.POST("/search", handler.HandleDelegatedSearch)
		delegated.POST("/readability", handler.HandleDelegatedReadability)

		// Patterns
		delegated.GET("/patterns/names", handler.HandleDelegatedPatternNames)
		delegated.GET("/patterns/:name", handler.HandleDelegatedGetPattern)
		delegated.POST("/patterns/:name/apply", handler.HandleDelegatedApplyPattern)

		// Strategies & Contexts
		delegated.GET("/strategies", handler.HandleDelegatedStrategies)
		delegated.GET("/strategies/:name", handler.HandleDelegatedGetStrategy)
		delegated.GET("/contexts", handler.HandleDelegatedContexts)
		delegated.GET("/contexts/:name", handler.HandleDelegatedGetContext)

		// Template plugins
		delegated.POST("/template/apply", handler.HandleDelegatedTemplateApply)

		// MCP CLI - Dynamic MCP server discovery and tool execution (delegated)
		delegated.POST("/mcp-cli/list", handler.HandleMcpCliList)
		delegated.POST("/mcp-cli/grep", handler.HandleMcpCliGrep)
		delegated.POST("/mcp-cli/schema", handler.HandleMcpCliSchema)
		delegated.POST("/mcp-cli/call", handler.HandleMcpCliCall)
	}

	// Non-delegated MCP CLI routes (no auth required - runs locally on server)
	// These are used by the Fabric AI client for server-side MCP tool discovery
	mcpCli := r.Group("/mcp-cli")
	{
		mcpCli.POST("/list", handler.HandleMcpCliListNoAuth)
		mcpCli.POST("/grep", handler.HandleMcpCliGrepNoAuth)
		mcpCli.POST("/schema", handler.HandleMcpCliSchemaNoAuth)
		mcpCli.POST("/call", handler.HandleMcpCliCallNoAuth)
	}

	return handler
}

// getFabricBaseURL returns the Fabric callback URL from environment or default
func getFabricBaseURL() string {
	if url := os.Getenv(FabricBaseURLEnvVar); url != "" {
		return url
	}
	return DefaultFabricURL
}

// exchangeTokenForCredentials calls back to Fabric to exchange AI token for credentials
func exchangeTokenForCredentials(token string) (*TokenExchangeResponse, error) {
	fabricURL := getFabricBaseURL()
	exchangeURL := fabricURL + ExchangeEndpoint

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", exchangeURL, strings.NewReader("{}"))
	if err != nil {
		return nil, fmt.Errorf("failed to create exchange request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(AITokenHeader, token)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange token: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read exchange response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Error string `json:"error"`
			Code  string `json:"code"`
		}
		if json.Unmarshal(body, &errResp) == nil && errResp.Error != "" {
			return nil, fmt.Errorf("token exchange failed: %s (code: %s)", errResp.Error, errResp.Code)
		}
		return nil, fmt.Errorf("token exchange failed with status %d: %s", resp.StatusCode, string(body))
	}

	var result TokenExchangeResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse exchange response: %w", err)
	}

	return &result, nil
}

// HandleDelegatedChat godoc
// @Summary Stream chat completions with delegated credentials
// @Description Stream AI responses using credentials obtained via secure token exchange
// @Tags delegated
// @Accept json
// @Produce text/event-stream
// @Param X-AI-Token header string true "AI Token for credential exchange"
// @Param request body DelegatedChatRequest true "Chat request with prompts and options"
// @Success 200 {object} StreamResponse "Streaming response"
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Router /delegated/chat [post]
func (h *DelegatedHandler) HandleDelegatedChat(c *gin.Context) {
	// Extract AI token from header
	aiToken := c.GetHeader(AITokenHeader)
	if aiToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing X-AI-Token header"})
		return
	}

	// Exchange token for credentials
	creds, err := exchangeTokenForCredentials(aiToken)
	if err != nil {
		log.Printf("Token exchange failed: %v", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": fmt.Sprintf("Token exchange failed: %v", err)})
		return
	}

	log.Printf("Token exchanged successfully - Provider: %s, Model: %s", creds.Provider, creds.Model)

	var request DelegatedChatRequest
	if err := c.BindJSON(&request); err != nil {
		log.Printf("Error binding JSON: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid request format: %v", err)})
		return
	}

	// Set headers for SSE
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")

	clientGone := c.Writer.CloseNotify()

	for i, prompt := range request.Prompts {
		select {
		case <-clientGone:
			log.Printf("Client disconnected")
			return
		default:
			// Use credentials from token exchange if not specified in prompt
			vendor := prompt.Vendor
			model := prompt.Model
			if vendor == "" {
				vendor = creds.Provider
			}
			if model == "" {
				model = creds.Model
			}

			log.Printf("Processing delegated prompt %d: Vendor=%s Model=%s Pattern=%s",
				i+1, vendor, model, prompt.PatternName)

			streamChan := make(chan string)

			go func(p PromptRequest, v, m, apiKey, baseUrl string, lang string, chatOpts domain.ChatOptions) {
				// Load and prepend strategy prompt if strategyName is set
				if p.StrategyName != "" {
					strategyFile := filepath.Join(os.Getenv("HOME"), ".config", "fabric", "strategies", p.StrategyName+".json")
					data, err := os.ReadFile(strategyFile)
					if err == nil {
						var s struct {
							Prompt string `json:"prompt"`
						}
						if json.Unmarshal(data, &s) == nil && s.Prompt != "" {
							p.UserInput = s.Prompt + "\n" + p.UserInput
						}
					}
				}

				// Build the messages for direct vendor streaming
				var messages []*chat.ChatCompletionMessage

				// Load pattern if specified
				var systemContent string
				if p.PatternName != "" {
					pattern, err := h.db.Patterns.GetApplyVariables(p.PatternName, p.Variables, p.UserInput)
					if err != nil {
						log.Printf("Error loading pattern: %v", err)
						streamChan <- fmt.Sprintf("Error: failed to load pattern: %v", err)
						close(streamChan)
						return
					}
					systemContent = pattern.Pattern
				}

				// Load context if specified
				if p.ContextName != "" {
					ctx, err := h.db.Contexts.Get(p.ContextName)
					if err != nil {
						log.Printf("Error loading context: %v", err)
						streamChan <- fmt.Sprintf("Error: failed to load context: %v", err)
						close(streamChan)
						return
					}
					systemContent = ctx.Content + systemContent
				}

				// Apply language instruction if specified
				if lang != "" && lang != "en" {
					systemContent = fmt.Sprintf("%s\n\nIMPORTANT: First, execute the instructions provided in this prompt using the user's input. Second, ensure your entire final response, including any section headers or titles generated as part of executing the instructions, is written ONLY in the %s language.", systemContent, lang)
				}

				// Add system message if we have pattern/context
				if systemContent != "" {
					messages = append(messages, &chat.ChatCompletionMessage{
						Role:    chat.ChatMessageRoleSystem,
						Content: systemContent,
					})
				}

				// Add user message
				messages = append(messages, &chat.ChatCompletionMessage{
					Role:    chat.ChatMessageRoleUser,
					Content: p.UserInput,
				})

				opts := &domain.ChatOptions{
					Model:            m,
					Temperature:      chatOpts.Temperature,
					TopP:             chatOpts.TopP,
					FrequencyPenalty: chatOpts.FrequencyPenalty,
					PresencePenalty:  chatOpts.PresencePenalty,
					Thinking:         chatOpts.Thinking,
				}

				// Use true streaming - send directly to the channel
				// The vendor's SendStream will close the channel when done
				err := h.registry.SendStreamWithCredentials(v, apiKey, baseUrl, messages, opts, streamChan)
				if err != nil {
					log.Printf("Error from SendStreamWithCredentials: %v", err)
					// Channel may already be closed by the vendor, so we can't send error
					// The error is logged for debugging
				}
			}(prompt, vendor, model, creds.ApiKey, creds.BaseUrl, request.Language, request.ChatOptions)

			for content := range streamChan {
				select {
				case <-clientGone:
					return
				default:
					var response StreamResponse
					if strings.HasPrefix(content, "Error:") {
						response = StreamResponse{
							Type:    "error",
							Format:  "plain",
							Content: content,
						}
					} else {
						response = StreamResponse{
							Type:    "content",
							Format:  detectFormat(content),
							Content: content,
						}
					}
					if err := writeSSEResponse(c.Writer, response); err != nil {
						log.Printf("Error writing response: %v", err)
						return
					}
				}
			}

			completeResponse := StreamResponse{
				Type:    "complete",
				Format:  "plain",
				Content: "",
			}
			if err := writeSSEResponse(c.Writer, completeResponse); err != nil {
				log.Printf("Error writing completion response: %v", err)
				return
			}
		}
	}
}

// HandleDelegatedTranscribe godoc
// @Summary Transcribe audio with delegated credentials
// @Description Transcribe audio file using credentials obtained via secure token exchange
// @Tags delegated
// @Accept json
// @Produce json
// @Param X-AI-Token header string true "AI Token for credential exchange"
// @Param request body DelegatedTranscribeRequest true "Transcription request"
// @Success 200 {object} DelegatedTranscribeResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /delegated/transcribe [post]
func (h *DelegatedHandler) HandleDelegatedTranscribe(c *gin.Context) {
	// Extract AI token from header
	aiToken := c.GetHeader(AITokenHeader)
	if aiToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing X-AI-Token header"})
		return
	}

	// Exchange token for credentials
	creds, err := exchangeTokenForCredentials(aiToken)
	if err != nil {
		log.Printf("Token exchange failed: %v", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": fmt.Sprintf("Token exchange failed: %v", err)})
		return
	}

	// Only OpenAI supports transcription
	if creds.Provider != "openai" && creds.Provider != "OpenAI" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Transcription requires OpenAI provider"})
		return
	}

	var request DelegatedTranscribeRequest
	if err := c.BindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid request: %v", err)})
		return
	}

	// Validate file extension
	ext := strings.ToLower(filepath.Ext(request.FileName))
	validExtensions := map[string]bool{
		".mp3": true, ".mp4": true, ".mpeg": true, ".mpga": true,
		".m4a": true, ".wav": true, ".webm": true,
	}
	if !validExtensions[ext] {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Unsupported audio format: %s", ext)})
		return
	}

	// Default model
	model := request.Model
	if model == "" {
		model = "whisper-1"
	}

	// Decode base64 file content
	fileData, err := base64.StdEncoding.DecodeString(request.FileContent)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid base64 file content"})
		return
	}

	// Check file size
	if int64(len(fileData)) > openai.MaxAudioFileSize {
		if !request.Split {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": fmt.Sprintf("File exceeds 25MB limit (%d bytes). Set split=true to enable automatic splitting.", len(fileData)),
			})
			return
		}
	}

	// Create a temporary file
	tmpFile, err := os.CreateTemp("", "fabric-transcribe-*"+ext)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create temp file"})
		return
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	if _, err := tmpFile.Write(fileData); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to write temp file"})
		return
	}
	tmpFile.Close()

	// Create OpenAI client with delegated credentials
	openaiClient := openai.NewClientWithCredentials(creds.ApiKey, creds.BaseUrl)

	// Transcribe
	text, err := openaiClient.TranscribeFile(c.Request.Context(), tmpFile.Name(), model, request.Split)
	if err != nil {
		log.Printf("Transcription error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Transcription failed: %v", err)})
		return
	}

	c.JSON(http.StatusOK, DelegatedTranscribeResponse{Text: text})
}

// HandleDelegatedYouTubeTranscript godoc
// @Summary Extract YouTube transcript with delegated auth
// @Description Extract transcript from a YouTube video using delegated authentication
// @Tags delegated
// @Accept json
// @Produce json
// @Param X-AI-Token header string true "AI Token for credential exchange"
// @Param request body DelegatedYouTubeRequest true "YouTube transcript request"
// @Success 200 {object} DelegatedYouTubeResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /delegated/youtube/transcript [post]
func (h *DelegatedHandler) HandleDelegatedYouTubeTranscript(c *gin.Context) {
	// Extract AI token from header - validates the request is authenticated
	aiToken := c.GetHeader(AITokenHeader)
	if aiToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing X-AI-Token header"})
		return
	}

	// Validate token by exchanging it (we don't need the credentials for transcript extraction)
	if _, err := exchangeTokenForCredentials(aiToken); err != nil {
		log.Printf("Token exchange failed: %v", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": fmt.Sprintf("Token exchange failed: %v", err)})
		return
	}

	var request DelegatedYouTubeRequest
	if err := c.BindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid request: %v", err)})
		return
	}

	if request.URL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "url is required"})
		return
	}

	language := request.Language
	if language == "" {
		language = "en"
	}

	// Use the registry's YouTube client
	yt := h.registry.YouTube

	var videoID, playlistID string
	var err error
	if videoID, playlistID, err = yt.GetVideoOrPlaylistId(request.URL); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if videoID == "" && playlistID != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "URL is a playlist, not a video"})
		return
	}
	if videoID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Could not extract video ID from URL"})
		return
	}

	// Try to get metadata (requires valid YouTube API key), but don't fail if unavailable
	var metadata *youtube.VideoMetadata
	var title, description string
	if metadata, err = yt.GrabMetadata(videoID); err == nil {
		title = metadata.Title
		description = metadata.Description
	} else {
		title = videoID
		description = ""
	}

	var transcript string
	if request.Timestamps {
		transcript, err = yt.GrabTranscriptWithTimestamps(videoID, language)
	} else {
		transcript, err = yt.GrabTranscript(videoID, language)
	}
	if err != nil {
		log.Printf("YouTube transcript error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to get transcript: %v", err)})
		return
	}

	c.JSON(http.StatusOK, DelegatedYouTubeResponse{
		Transcript:  transcript,
		VideoId:     videoID,
		Title:       title,
		Description: description,
	})
}

// HandleDelegatedScrape godoc
// @Summary Scrape URL content with delegated auth
// @Description Scrape webpage content using Jina AI with delegated authentication
// @Tags delegated
// @Accept json
// @Produce json
// @Param X-AI-Token header string true "AI Token for credential exchange"
// @Param request body DelegatedScrapeRequest true "Scrape request"
// @Success 200 {object} DelegatedScrapeResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /delegated/scrape [post]
func (h *DelegatedHandler) HandleDelegatedScrape(c *gin.Context) {
	// Extract AI token from header - validates the request is authenticated
	aiToken := c.GetHeader(AITokenHeader)
	if aiToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing X-AI-Token header"})
		return
	}

	// Exchange token for credentials (includes Jina API key if configured)
	creds, err := exchangeTokenForCredentials(aiToken)
	if err != nil {
		log.Printf("Token exchange failed: %v", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": fmt.Sprintf("Token exchange failed: %v", err)})
		return
	}

	var request DelegatedScrapeRequest
	if err := c.BindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid request: %v", err)})
		return
	}

	if request.URL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "url is required"})
		return
	}

	var content string

	// Use user's Jina API key if provided, otherwise fall back to registry
	if creds.JinaApiKey != "" {
		log.Printf("Using delegated Jina API key for scrape")
		content, err = jina.ScrapeURLWithApiKey(request.URL, creds.JinaApiKey)
	} else {
		log.Printf("Using registry Jina client for scrape (no delegated key)")
		content, err = h.registry.Jina.ScrapeURL(request.URL)
	}

	if err != nil {
		log.Printf("Scrape error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to scrape URL: %v", err)})
		return
	}

	c.JSON(http.StatusOK, DelegatedScrapeResponse{
		Content: content,
		URL:     request.URL,
	})
}

// HandleDelegatedSearch godoc
// @Summary Search the web with delegated auth
// @Description Search the web using Jina AI with delegated authentication
// @Tags delegated
// @Accept json
// @Produce json
// @Param X-AI-Token header string true "AI Token for credential exchange"
// @Param request body DelegatedSearchRequest true "Search request"
// @Success 200 {object} DelegatedSearchResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /delegated/search [post]
func (h *DelegatedHandler) HandleDelegatedSearch(c *gin.Context) {
	// Extract AI token from header - validates the request is authenticated
	aiToken := c.GetHeader(AITokenHeader)
	if aiToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing X-AI-Token header"})
		return
	}

	// Exchange token for credentials (includes Jina API key if configured)
	creds, err := exchangeTokenForCredentials(aiToken)
	if err != nil {
		log.Printf("Token exchange failed: %v", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": fmt.Sprintf("Token exchange failed: %v", err)})
		return
	}

	var request DelegatedSearchRequest
	if err := c.BindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid request: %v", err)})
		return
	}

	if request.Question == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "question is required"})
		return
	}

	var content string

	// Use user's Jina API key if provided, otherwise fall back to registry
	if creds.JinaApiKey != "" {
		log.Printf("Using delegated Jina API key for search")
		content, err = jina.ScrapeQuestionWithApiKey(request.Question, creds.JinaApiKey)
	} else {
		log.Printf("Using registry Jina client for search (no delegated key)")
		content, err = h.registry.Jina.ScrapeQuestion(request.Question)
	}

	if err != nil {
		log.Printf("Search error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to search: %v", err)})
		return
	}

	c.JSON(http.StatusOK, DelegatedSearchResponse{
		Content:  content,
		Question: request.Question,
	})
}

// HandleDelegatedPatternNames godoc
// @Summary List all available patterns with delegated auth
// @Description Get list of all available pattern names with delegated authentication
// @Tags delegated
// @Produce json
// @Param X-AI-Token header string true "AI Token for credential exchange"
// @Success 200 {array} string
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /delegated/patterns/names [get]
func (h *DelegatedHandler) HandleDelegatedPatternNames(c *gin.Context) {
	// Extract AI token from header - validates the request is authenticated
	aiToken := c.GetHeader(AITokenHeader)
	if aiToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing X-AI-Token header"})
		return
	}

	// Validate token by exchanging it
	if _, err := exchangeTokenForCredentials(aiToken); err != nil {
		log.Printf("Token exchange failed: %v", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": fmt.Sprintf("Token exchange failed: %v", err)})
		return
	}

	names, err := h.db.Patterns.GetNames()
	if err != nil {
		log.Printf("Error getting pattern names: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to get pattern names: %v", err)})
		return
	}

	c.JSON(http.StatusOK, names)
}

// HandleDelegatedGetPattern godoc
// @Summary Get a pattern by name with delegated auth
// @Description Retrieve a pattern's content by name with delegated authentication
// @Tags delegated
// @Produce json
// @Param X-AI-Token header string true "AI Token for credential exchange"
// @Param name path string true "Pattern name"
// @Success 200 {object} DelegatedPatternResponse
// @Failure 401 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /delegated/patterns/{name} [get]
func (h *DelegatedHandler) HandleDelegatedGetPattern(c *gin.Context) {
	// Extract AI token from header - validates the request is authenticated
	aiToken := c.GetHeader(AITokenHeader)
	if aiToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing X-AI-Token header"})
		return
	}

	// Validate token by exchanging it
	if _, err := exchangeTokenForCredentials(aiToken); err != nil {
		log.Printf("Token exchange failed: %v", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": fmt.Sprintf("Token exchange failed: %v", err)})
		return
	}

	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pattern name is required"})
		return
	}

	// Get the raw pattern content without any variable processing
	content, err := h.db.Patterns.Load(name + "/" + h.db.Patterns.SystemPatternFile)
	if err != nil {
		log.Printf("Error loading pattern %s: %v", name, err)
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Pattern not found: %s", name)})
		return
	}

	c.JSON(http.StatusOK, DelegatedPatternResponse{
		Name:    name,
		Pattern: string(content),
	})
}

// HandleDelegatedApplyPattern godoc
// @Summary Apply a pattern with variables with delegated auth
// @Description Apply a pattern with variable substitution with delegated authentication
// @Tags delegated
// @Accept json
// @Produce json
// @Param X-AI-Token header string true "AI Token for credential exchange"
// @Param name path string true "Pattern name"
// @Param request body DelegatedPatternRequest true "Pattern application request"
// @Success 200 {object} DelegatedPatternResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /delegated/patterns/{name}/apply [post]
func (h *DelegatedHandler) HandleDelegatedApplyPattern(c *gin.Context) {
	// Extract AI token from header - validates the request is authenticated
	aiToken := c.GetHeader(AITokenHeader)
	if aiToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing X-AI-Token header"})
		return
	}

	// Validate token by exchanging it
	if _, err := exchangeTokenForCredentials(aiToken); err != nil {
		log.Printf("Token exchange failed: %v", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": fmt.Sprintf("Token exchange failed: %v", err)})
		return
	}

	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pattern name is required"})
		return
	}

	var request DelegatedPatternRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid request: %v", err)})
		return
	}

	// Merge query parameters with request body variables (body takes precedence)
	variables := make(map[string]string)
	for key, values := range c.Request.URL.Query() {
		if len(values) > 0 {
			variables[key] = values[0]
		}
	}
	maps.Copy(variables, request.Variables)

	// Apply variables to the pattern
	pattern, err := h.db.Patterns.GetApplyVariables(name, variables, request.Input)
	if err != nil {
		log.Printf("Error applying pattern %s: %v", name, err)
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Failed to apply pattern: %v", err)})
		return
	}

	c.JSON(http.StatusOK, DelegatedPatternResponse{
		Name:        pattern.Name,
		Description: pattern.Description,
		Pattern:     pattern.Pattern,
	})
}

// =============================================================================
// YouTube Extended Handlers
// =============================================================================

// HandleDelegatedYouTubeMetadata godoc
// @Summary Get YouTube video metadata with delegated auth
// @Description Get comprehensive metadata for a YouTube video (requires YouTube API key)
// @Tags delegated
// @Accept json
// @Produce json
// @Param X-AI-Token header string true "AI Token for credential exchange"
// @Param request body DelegatedYouTubeMetadataRequest true "YouTube metadata request"
// @Success 200 {object} DelegatedYouTubeMetadataResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /delegated/youtube/metadata [post]
func (h *DelegatedHandler) HandleDelegatedYouTubeMetadata(c *gin.Context) {
	aiToken := c.GetHeader(AITokenHeader)
	if aiToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing X-AI-Token header"})
		return
	}

	if _, err := exchangeTokenForCredentials(aiToken); err != nil {
		log.Printf("Token exchange failed: %v", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": fmt.Sprintf("Token exchange failed: %v", err)})
		return
	}

	var request DelegatedYouTubeMetadataRequest
	if err := c.BindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid request: %v", err)})
		return
	}

	if request.URL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "url is required"})
		return
	}

	yt := h.registry.YouTube

	videoID, _, err := yt.GetVideoOrPlaylistId(request.URL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if videoID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Could not extract video ID from URL"})
		return
	}

	metadata, err := yt.GrabMetadata(videoID)
	if err != nil {
		log.Printf("YouTube metadata error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to get metadata: %v", err)})
		return
	}

	duration, _ := yt.GrabDuration(videoID)

	c.JSON(http.StatusOK, DelegatedYouTubeMetadataResponse{
		VideoId:      videoID,
		Title:        metadata.Title,
		Description:  metadata.Description,
		ChannelTitle: metadata.ChannelTitle,
		PublishedAt:  metadata.PublishedAt,
		Duration:     duration,
		ViewCount:    metadata.ViewCount,
		LikeCount:    metadata.LikeCount,
		Tags:         metadata.Tags,
	})
}

// HandleDelegatedYouTubeComments godoc
// @Summary Get YouTube video comments with delegated auth
// @Description Get comments from a YouTube video (requires YouTube API key)
// @Tags delegated
// @Accept json
// @Produce json
// @Param X-AI-Token header string true "AI Token for credential exchange"
// @Param request body DelegatedYouTubeCommentsRequest true "YouTube comments request"
// @Success 200 {object} DelegatedYouTubeCommentsResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /delegated/youtube/comments [post]
func (h *DelegatedHandler) HandleDelegatedYouTubeComments(c *gin.Context) {
	aiToken := c.GetHeader(AITokenHeader)
	if aiToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing X-AI-Token header"})
		return
	}

	if _, err := exchangeTokenForCredentials(aiToken); err != nil {
		log.Printf("Token exchange failed: %v", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": fmt.Sprintf("Token exchange failed: %v", err)})
		return
	}

	var request DelegatedYouTubeCommentsRequest
	if err := c.BindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid request: %v", err)})
		return
	}

	if request.URL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "url is required"})
		return
	}

	yt := h.registry.YouTube

	videoID, _, err := yt.GetVideoOrPlaylistId(request.URL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if videoID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Could not extract video ID from URL"})
		return
	}

	comments, err := yt.GrabComments(videoID)
	if err != nil {
		log.Printf("YouTube comments error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to get comments: %v", err)})
		return
	}

	c.JSON(http.StatusOK, DelegatedYouTubeCommentsResponse{
		VideoId:  videoID,
		Comments: comments,
		Count:    len(comments),
	})
}

// HandleDelegatedYouTubePlaylist godoc
// @Summary Get YouTube playlist videos with delegated auth
// @Description Get all videos in a YouTube playlist (requires YouTube API key)
// @Tags delegated
// @Accept json
// @Produce json
// @Param X-AI-Token header string true "AI Token for credential exchange"
// @Param request body DelegatedYouTubePlaylistRequest true "YouTube playlist request"
// @Success 200 {object} DelegatedYouTubePlaylistResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /delegated/youtube/playlist [post]
func (h *DelegatedHandler) HandleDelegatedYouTubePlaylist(c *gin.Context) {
	aiToken := c.GetHeader(AITokenHeader)
	if aiToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing X-AI-Token header"})
		return
	}

	if _, err := exchangeTokenForCredentials(aiToken); err != nil {
		log.Printf("Token exchange failed: %v", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": fmt.Sprintf("Token exchange failed: %v", err)})
		return
	}

	var request DelegatedYouTubePlaylistRequest
	if err := c.BindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid request: %v", err)})
		return
	}

	if request.URL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "url is required"})
		return
	}

	yt := h.registry.YouTube

	_, playlistID, err := yt.GetVideoOrPlaylistId(request.URL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if playlistID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "URL is not a playlist"})
		return
	}

	videos, err := yt.FetchPlaylistVideos(playlistID)
	if err != nil {
		log.Printf("YouTube playlist error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to get playlist: %v", err)})
		return
	}

	var playlistVideos []PlaylistVideoInfo
	for _, v := range videos {
		playlistVideos = append(playlistVideos, PlaylistVideoInfo{
			VideoId: v.Id,
			Title:   v.Title,
			URL:     fmt.Sprintf("https://www.youtube.com/watch?v=%s", v.Id),
		})
	}

	c.JSON(http.StatusOK, DelegatedYouTubePlaylistResponse{
		PlaylistId: playlistID,
		Videos:     playlistVideos,
		Count:      len(playlistVideos),
	})
}

// =============================================================================
// HTML Readability Handler
// =============================================================================

// HandleDelegatedReadability godoc
// @Summary Extract readable content from HTML with delegated auth
// @Description Extract clean, readable text content from raw HTML using readability parser
// @Tags delegated
// @Accept json
// @Produce json
// @Param X-AI-Token header string true "AI Token for credential exchange"
// @Param request body DelegatedReadabilityRequest true "Readability request"
// @Success 200 {object} DelegatedReadabilityResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /delegated/readability [post]
func (h *DelegatedHandler) HandleDelegatedReadability(c *gin.Context) {
	aiToken := c.GetHeader(AITokenHeader)
	if aiToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing X-AI-Token header"})
		return
	}

	if _, err := exchangeTokenForCredentials(aiToken); err != nil {
		log.Printf("Token exchange failed: %v", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": fmt.Sprintf("Token exchange failed: %v", err)})
		return
	}

	var request DelegatedReadabilityRequest
	if err := c.BindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid request: %v", err)})
		return
	}

	if request.HTML == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "html is required"})
		return
	}

	content, err := converter.HtmlReadability(request.HTML)
	if err != nil {
		log.Printf("Readability error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to extract content: %v", err)})
		return
	}

	c.JSON(http.StatusOK, DelegatedReadabilityResponse{
		Content: content,
		URL:     request.URL,
	})
}

// =============================================================================
// Strategies & Contexts Handlers
// =============================================================================

// HandleDelegatedStrategies godoc
// @Summary List available strategies with delegated auth
// @Description Get list of all available strategies with their content
// @Tags delegated
// @Produce json
// @Param X-AI-Token header string true "AI Token for credential exchange"
// @Success 200 {object} DelegatedStrategiesResponse
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /delegated/strategies [get]
func (h *DelegatedHandler) HandleDelegatedStrategies(c *gin.Context) {
	aiToken := c.GetHeader(AITokenHeader)
	if aiToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing X-AI-Token header"})
		return
	}

	if _, err := exchangeTokenForCredentials(aiToken); err != nil {
		log.Printf("Token exchange failed: %v", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": fmt.Sprintf("Token exchange failed: %v", err)})
		return
	}

	var result []StrategyInfo
	for name, s := range h.registry.Strategies.Strategies {
		result = append(result, StrategyInfo{
			Name:        name,
			Description: s.Description,
			Prompt:      s.Prompt,
		})
	}

	c.JSON(http.StatusOK, DelegatedStrategiesResponse{
		Strategies: result,
		Count:      len(result),
	})
}

// HandleDelegatedGetStrategy godoc
// @Summary Get a specific strategy with delegated auth
// @Description Get the content of a specific strategy by name
// @Tags delegated
// @Produce json
// @Param X-AI-Token header string true "AI Token for credential exchange"
// @Param name path string true "Strategy name"
// @Success 200 {object} StrategyInfo
// @Failure 401 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /delegated/strategies/{name} [get]
func (h *DelegatedHandler) HandleDelegatedGetStrategy(c *gin.Context) {
	aiToken := c.GetHeader(AITokenHeader)
	if aiToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing X-AI-Token header"})
		return
	}

	if _, err := exchangeTokenForCredentials(aiToken); err != nil {
		log.Printf("Token exchange failed: %v", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": fmt.Sprintf("Token exchange failed: %v", err)})
		return
	}

	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "strategy name is required"})
		return
	}

	s, ok := h.registry.Strategies.Strategies[name]
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Strategy '%s' not found", name)})
		return
	}

	c.JSON(http.StatusOK, StrategyInfo{
		Name:        name,
		Description: s.Description,
		Prompt:      s.Prompt,
	})
}

// HandleDelegatedContexts godoc
// @Summary List available contexts with delegated auth
// @Description Get list of all available contexts with their content
// @Tags delegated
// @Produce json
// @Param X-AI-Token header string true "AI Token for credential exchange"
// @Success 200 {object} DelegatedContextsResponse
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /delegated/contexts [get]
func (h *DelegatedHandler) HandleDelegatedContexts(c *gin.Context) {
	aiToken := c.GetHeader(AITokenHeader)
	if aiToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing X-AI-Token header"})
		return
	}

	if _, err := exchangeTokenForCredentials(aiToken); err != nil {
		log.Printf("Token exchange failed: %v", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": fmt.Sprintf("Token exchange failed: %v", err)})
		return
	}

	contextNames, err := h.db.Contexts.GetNames()
	if err != nil {
		log.Printf("Error listing contexts: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to list contexts: %v", err)})
		return
	}

	var result []ContextInfo
	for _, name := range contextNames {
		content, err := h.db.Contexts.Get(name)
		if err != nil {
			continue
		}
		result = append(result, ContextInfo{
			Name:    name,
			Content: content.Content,
		})
	}

	c.JSON(http.StatusOK, DelegatedContextsResponse{
		Contexts: result,
		Count:    len(result),
	})
}

// HandleDelegatedGetContext godoc
// @Summary Get a specific context with delegated auth
// @Description Get the content of a specific context by name
// @Tags delegated
// @Produce json
// @Param X-AI-Token header string true "AI Token for credential exchange"
// @Param name path string true "Context name"
// @Success 200 {object} ContextInfo
// @Failure 401 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /delegated/contexts/{name} [get]
func (h *DelegatedHandler) HandleDelegatedGetContext(c *gin.Context) {
	aiToken := c.GetHeader(AITokenHeader)
	if aiToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing X-AI-Token header"})
		return
	}

	if _, err := exchangeTokenForCredentials(aiToken); err != nil {
		log.Printf("Token exchange failed: %v", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": fmt.Sprintf("Token exchange failed: %v", err)})
		return
	}

	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "context name is required"})
		return
	}

	context, err := h.db.Contexts.Get(name)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Context '%s' not found", name)})
		return
	}

	c.JSON(http.StatusOK, ContextInfo{
		Name:    name,
		Content: context.Content,
	})
}

// =============================================================================
// Template Plugin Handler
// =============================================================================

// HandleDelegatedTemplateApply godoc
// @Summary Apply template plugins with delegated auth
// @Description Process a template string with Fabric's template plugins (datetime, text, file, etc.)
// @Tags delegated
// @Accept json
// @Produce json
// @Param X-AI-Token header string true "AI Token for credential exchange"
// @Param request body TemplatePluginRequest true "Template processing request"
// @Success 200 {object} TemplatePluginResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /delegated/template/apply [post]
func (h *DelegatedHandler) HandleDelegatedTemplateApply(c *gin.Context) {
	aiToken := c.GetHeader(AITokenHeader)
	if aiToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing X-AI-Token header"})
		return
	}

	if _, err := exchangeTokenForCredentials(aiToken); err != nil {
		log.Printf("Token exchange failed: %v", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": fmt.Sprintf("Token exchange failed: %v", err)})
		return
	}

	var request TemplatePluginRequest
	if err := c.BindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid request: %v", err)})
		return
	}

	if request.Template == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "template is required"})
		return
	}

	output, err := template.ApplyTemplate(request.Template, request.Variables, request.Input)
	if err != nil {
		log.Printf("Template processing error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to process template: %v", err)})
		return
	}

	c.JSON(http.StatusOK, TemplatePluginResponse{
		Output:   output,
		Template: request.Template,
	})
}

// =============================================================================
// MCP CLI Handlers - Dynamic MCP Server Discovery and Tool Execution
// =============================================================================

// McpCliListRequest represents a request to list MCP servers
type McpCliListRequest struct {
	WithDescriptions bool `json:"withDescriptions,omitempty"`
}

// McpCliListResponse represents the list of MCP servers and tools
type McpCliListResponse struct {
	Servers []McpCliServerInfo `json:"servers"`
	Count   int                `json:"count"`
}

// McpCliServerInfo represents an MCP server with its tools
type McpCliServerInfo struct {
	Name  string           `json:"name"`
	Tools []McpCliToolInfo `json:"tools"`
}

// McpCliToolInfo represents an MCP tool
type McpCliToolInfo struct {
	Name        string                 `json:"name"`
	Server      string                 `json:"server"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"inputSchema,omitempty"`
}

// McpCliGrepRequest represents a request to search for tools
type McpCliGrepRequest struct {
	Pattern          string `json:"pattern" binding:"required"`
	WithDescriptions bool   `json:"withDescriptions,omitempty"`
}

// McpCliGrepResponse represents the search results
type McpCliGrepResponse struct {
	Tools []McpCliToolInfo `json:"tools"`
	Count int              `json:"count"`
}

// McpCliSchemaRequest represents a request to get a tool's schema
type McpCliSchemaRequest struct {
	ServerTool string `json:"serverTool" binding:"required"` // Format: server/tool
}

// McpCliSchemaResponse represents a tool's schema
type McpCliSchemaResponse struct {
	Name        string                 `json:"name"`
	Server      string                 `json:"server"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"inputSchema,omitempty"`
}

// McpCliCallRequest represents a request to call an MCP tool
type McpCliCallRequest struct {
	ServerTool string                 `json:"serverTool" binding:"required"` // Format: server/tool
	Arguments  map[string]interface{} `json:"arguments,omitempty"`
}

// McpCliCallResponse represents the result of calling an MCP tool
type McpCliCallResponse struct {
	Content interface{} `json:"content"`
	IsError bool        `json:"isError,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// HandleMcpCliList godoc
// @Summary List all MCP servers and tools
// @Description List all available MCP servers and their tools using mcp-cli
// @Tags delegated,mcp-cli
// @Accept json
// @Produce json
// @Param X-AI-Token header string true "AI Token for credential exchange"
// @Param request body McpCliListRequest false "List options"
// @Success 200 {object} McpCliListResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /delegated/mcp-cli/list [post]
func (h *DelegatedHandler) HandleMcpCliList(c *gin.Context) {
	aiToken := c.GetHeader(AITokenHeader)
	if aiToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing X-AI-Token header"})
		return
	}

	if _, err := exchangeTokenForCredentials(aiToken); err != nil {
		log.Printf("Token exchange failed: %v", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": fmt.Sprintf("Token exchange failed: %v", err)})
		return
	}

	var request McpCliListRequest
	// Ignore binding errors - use defaults if no body
	_ = c.BindJSON(&request)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	servers, err := mcpcli.ListServers(ctx, "", request.WithDescriptions)
	if err != nil {
		log.Printf("MCP CLI list error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to list MCP servers: %v", err)})
		return
	}

	// Convert to response format
	responseServers := make([]McpCliServerInfo, len(servers))
	for i, s := range servers {
		tools := make([]McpCliToolInfo, len(s.Tools))
		for j, t := range s.Tools {
			tools[j] = McpCliToolInfo{
				Name:        t.Name,
				Server:      t.Server,
				Description: t.Description,
				InputSchema: t.InputSchema,
			}
		}
		responseServers[i] = McpCliServerInfo{
			Name:  s.Name,
			Tools: tools,
		}
	}

	c.JSON(http.StatusOK, McpCliListResponse{
		Servers: responseServers,
		Count:   len(responseServers),
	})
}

// HandleMcpCliGrep godoc
// @Summary Search for MCP tools by pattern
// @Description Search for tools across all MCP servers using glob pattern matching
// @Tags delegated,mcp-cli
// @Accept json
// @Produce json
// @Param X-AI-Token header string true "AI Token for credential exchange"
// @Param request body McpCliGrepRequest true "Search request"
// @Success 200 {object} McpCliGrepResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /delegated/mcp-cli/grep [post]
func (h *DelegatedHandler) HandleMcpCliGrep(c *gin.Context) {
	aiToken := c.GetHeader(AITokenHeader)
	if aiToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing X-AI-Token header"})
		return
	}

	if _, err := exchangeTokenForCredentials(aiToken); err != nil {
		log.Printf("Token exchange failed: %v", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": fmt.Sprintf("Token exchange failed: %v", err)})
		return
	}

	var request McpCliGrepRequest
	if err := c.BindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid request: %v", err)})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	tools, err := mcpcli.GrepTools(ctx, "", request.Pattern, request.WithDescriptions)
	if err != nil {
		log.Printf("MCP CLI grep error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to search tools: %v", err)})
		return
	}

	// Convert to response format
	responseTools := make([]McpCliToolInfo, len(tools))
	for i, t := range tools {
		responseTools[i] = McpCliToolInfo{
			Name:        t.Name,
			Server:      t.Server,
			Description: t.Description,
			InputSchema: t.InputSchema,
		}
	}

	c.JSON(http.StatusOK, McpCliGrepResponse{
		Tools: responseTools,
		Count: len(responseTools),
	})
}

// HandleMcpCliSchema godoc
// @Summary Get schema for an MCP tool
// @Description Get the JSON input schema for a specific MCP tool
// @Tags delegated,mcp-cli
// @Accept json
// @Produce json
// @Param X-AI-Token header string true "AI Token for credential exchange"
// @Param request body McpCliSchemaRequest true "Schema request"
// @Success 200 {object} McpCliSchemaResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /delegated/mcp-cli/schema [post]
func (h *DelegatedHandler) HandleMcpCliSchema(c *gin.Context) {
	aiToken := c.GetHeader(AITokenHeader)
	if aiToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing X-AI-Token header"})
		return
	}

	if _, err := exchangeTokenForCredentials(aiToken); err != nil {
		log.Printf("Token exchange failed: %v", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": fmt.Sprintf("Token exchange failed: %v", err)})
		return
	}

	var request McpCliSchemaRequest
	if err := c.BindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid request: %v", err)})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	tool, err := mcpcli.GetToolSchema(ctx, "", request.ServerTool)
	if err != nil {
		log.Printf("MCP CLI schema error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to get tool schema: %v", err)})
		return
	}

	c.JSON(http.StatusOK, McpCliSchemaResponse{
		Name:        tool.Name,
		Server:      tool.Server,
		Description: tool.Description,
		InputSchema: tool.InputSchema,
	})
}

// HandleMcpCliCall godoc
// @Summary Call an MCP tool
// @Description Execute an MCP tool with the provided arguments
// @Tags delegated,mcp-cli
// @Accept json
// @Produce json
// @Param X-AI-Token header string true "AI Token for credential exchange"
// @Param request body McpCliCallRequest true "Call request"
// @Success 200 {object} McpCliCallResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /delegated/mcp-cli/call [post]
func (h *DelegatedHandler) HandleMcpCliCall(c *gin.Context) {
	aiToken := c.GetHeader(AITokenHeader)
	if aiToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing X-AI-Token header"})
		return
	}

	if _, err := exchangeTokenForCredentials(aiToken); err != nil {
		log.Printf("Token exchange failed: %v", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": fmt.Sprintf("Token exchange failed: %v", err)})
		return
	}

	var request McpCliCallRequest
	if err := c.BindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid request: %v", err)})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 120*time.Second)
	defer cancel()

	args := request.Arguments
	if args == nil {
		args = make(map[string]interface{})
	}

	result, err := mcpcli.CallTool(ctx, "", request.ServerTool, args)
	if err != nil {
		log.Printf("MCP CLI call error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to call tool: %v", err)})
		return
	}

	c.JSON(http.StatusOK, McpCliCallResponse{
		Content: result.Content,
		IsError: result.IsError,
		Error:   result.Error,
	})
}

// =============================================================================
// MCP CLI Handlers - No Auth Version (for server-side use only)
// These run mcp-cli locally on the server without requiring user credentials
// =============================================================================

// HandleMcpCliListNoAuth - List MCP servers without auth
// @Summary List all MCP servers and tools (no auth)
// @Tags mcp-cli
// @Router /mcp-cli/list [post]
func (h *DelegatedHandler) HandleMcpCliListNoAuth(c *gin.Context) {
	var request McpCliListRequest
	_ = c.BindJSON(&request)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	servers, err := mcpcli.ListServers(ctx, "", request.WithDescriptions)
	if err != nil {
		log.Printf("MCP CLI list error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to list MCP servers: %v", err)})
		return
	}

	responseServers := make([]McpCliServerInfo, len(servers))
	for i, s := range servers {
		tools := make([]McpCliToolInfo, len(s.Tools))
		for j, t := range s.Tools {
			tools[j] = McpCliToolInfo{
				Name:        t.Name,
				Server:      t.Server,
				Description: t.Description,
				InputSchema: t.InputSchema,
			}
		}
		responseServers[i] = McpCliServerInfo{
			Name:  s.Name,
			Tools: tools,
		}
	}

	c.JSON(http.StatusOK, McpCliListResponse{
		Servers: responseServers,
		Count:   len(responseServers),
	})
}

// HandleMcpCliGrepNoAuth - Search MCP tools without auth
// @Summary Search for MCP tools by pattern (no auth)
// @Tags mcp-cli
// @Router /mcp-cli/grep [post]
func (h *DelegatedHandler) HandleMcpCliGrepNoAuth(c *gin.Context) {
	var request McpCliGrepRequest
	if err := c.BindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid request: %v", err)})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	tools, err := mcpcli.GrepTools(ctx, "", request.Pattern, request.WithDescriptions)
	if err != nil {
		log.Printf("MCP CLI grep error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to search tools: %v", err)})
		return
	}

	responseTools := make([]McpCliToolInfo, len(tools))
	for i, t := range tools {
		responseTools[i] = McpCliToolInfo{
			Name:        t.Name,
			Server:      t.Server,
			Description: t.Description,
			InputSchema: t.InputSchema,
		}
	}

	c.JSON(http.StatusOK, McpCliGrepResponse{
		Tools: responseTools,
		Count: len(responseTools),
	})
}

// HandleMcpCliSchemaNoAuth - Get tool schema without auth
// @Summary Get schema for an MCP tool (no auth)
// @Tags mcp-cli
// @Router /mcp-cli/schema [post]
func (h *DelegatedHandler) HandleMcpCliSchemaNoAuth(c *gin.Context) {
	var request McpCliSchemaRequest
	if err := c.BindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid request: %v", err)})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	schema, err := mcpcli.GetToolSchema(ctx, "", request.ServerTool)
	if err != nil {
		log.Printf("MCP CLI schema error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to get tool schema: %v", err)})
		return
	}

	c.JSON(http.StatusOK, McpCliSchemaResponse{
		Name:        schema.Name,
		Server:      schema.Server,
		Description: schema.Description,
		InputSchema: schema.InputSchema,
	})
}

// HandleMcpCliCallNoAuth - Call MCP tool without auth
// @Summary Call an MCP tool (no auth)
// @Tags mcp-cli
// @Router /mcp-cli/call [post]
func (h *DelegatedHandler) HandleMcpCliCallNoAuth(c *gin.Context) {
	var request McpCliCallRequest
	if err := c.BindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid request: %v", err)})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 120*time.Second)
	defer cancel()

	args := request.Arguments
	if args == nil {
		args = make(map[string]interface{})
	}

	result, err := mcpcli.CallTool(ctx, "", request.ServerTool, args)
	if err != nil {
		log.Printf("MCP CLI call error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to call tool: %v", err)})
		return
	}

	c.JSON(http.StatusOK, McpCliCallResponse{
		Content: result.Content,
		IsError: result.IsError,
		Error:   result.Error,
	})
}
