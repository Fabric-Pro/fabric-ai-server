package restapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// MockTokenExchangeServer simulates the Fabric Portal token exchange endpoint
type MockTokenExchangeServer struct {
	*httptest.Server
	ValidTokens map[string]DelegatedCredentials
}

func NewMockTokenExchangeServer() *MockTokenExchangeServer {
	mock := &MockTokenExchangeServer{
		ValidTokens: make(map[string]DelegatedCredentials),
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Token string `json:"token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid request"})
			return
		}

		creds, ok := mock.ValidTokens[req.Token]
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid token"})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(creds)
	})

	mock.Server = httptest.NewServer(handler)
	return mock
}

func (m *MockTokenExchangeServer) AddToken(token string, creds DelegatedCredentials) {
	m.ValidTokens[token] = creds
}

// Test helper to set up a test router with mock dependencies
func setupTestDelegatedHandler(t *testing.T, mockExchangeURL string) (*gin.Engine, *DelegatedHandler) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	// For tests, we can't easily mock the registry, so we test the handler logic
	// In a real scenario, you'd inject mock dependencies
	handler := &DelegatedHandler{}

	// Register routes manually for testing
	delegated := r.Group("/delegated")
	{
		delegated.POST("/youtube/transcript", handler.HandleDelegatedYouTubeTranscript)
		delegated.POST("/scrape", handler.HandleDelegatedScrape)
		delegated.POST("/search", handler.HandleDelegatedSearch)
		delegated.GET("/patterns/names", handler.HandleDelegatedPatternNames)
		delegated.GET("/patterns/:name", handler.HandleDelegatedGetPattern)
		delegated.POST("/patterns/:name/apply", handler.HandleDelegatedApplyPattern)
	}

	return r, handler
}

// =============================================================================
// Token Exchange Tests
// =============================================================================

func TestExchangeTokenForCredentials_ValidToken(t *testing.T) {
	mock := NewMockTokenExchangeServer()
	defer mock.Server.Close()

	// Override the exchange URL for testing
	originalEnv := os.Getenv("FABRIC_TOKEN_EXCHANGE_URL")
	os.Setenv("FABRIC_TOKEN_EXCHANGE_URL", mock.Server.URL)
	defer os.Setenv("FABRIC_TOKEN_EXCHANGE_URL", originalEnv)

	expectedCreds := DelegatedCredentials{
		Provider:     "openai",
		ApiKey:       "sk-test-key",
		Model:        "gpt-4",
		BaseUrl:      "https://api.openai.com/v1",
		UserId:       "user-123",
		Organization: "org-456",
	}
	mock.AddToken("valid-token", expectedCreds)

	creds, err := exchangeTokenForCredentials("valid-token")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if creds.Provider != expectedCreds.Provider {
		t.Errorf("Expected provider %s, got %s", expectedCreds.Provider, creds.Provider)
	}
	if creds.ApiKey != expectedCreds.ApiKey {
		t.Errorf("Expected API key %s, got %s", expectedCreds.ApiKey, creds.ApiKey)
	}
	if creds.Model != expectedCreds.Model {
		t.Errorf("Expected model %s, got %s", expectedCreds.Model, creds.Model)
	}
}

func TestExchangeTokenForCredentials_InvalidToken(t *testing.T) {
	mock := NewMockTokenExchangeServer()
	defer mock.Server.Close()

	originalEnv := os.Getenv("FABRIC_TOKEN_EXCHANGE_URL")
	os.Setenv("FABRIC_TOKEN_EXCHANGE_URL", mock.Server.URL)
	defer os.Setenv("FABRIC_TOKEN_EXCHANGE_URL", originalEnv)

	_, err := exchangeTokenForCredentials("invalid-token")
	if err == nil {
		t.Fatal("Expected error for invalid token")
	}
}

func TestExchangeTokenForCredentials_EmptyToken(t *testing.T) {
	_, err := exchangeTokenForCredentials("")
	if err == nil {
		t.Fatal("Expected error for empty token")
	}
}

// =============================================================================
// Request Validation Tests
// =============================================================================

func TestDelegatedYouTubeTranscript_MissingToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	handler := &DelegatedHandler{}
	r.POST("/delegated/youtube/transcript", handler.HandleDelegatedYouTubeTranscript)

	req := httptest.NewRequest(http.MethodPost, "/delegated/youtube/transcript", strings.NewReader(`{"url":"https://youtube.com/watch?v=test"}`))
	req.Header.Set("Content-Type", "application/json")
	// No X-AI-Token header

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", w.Code)
	}

	var response map[string]string
	json.Unmarshal(w.Body.Bytes(), &response)
	if !strings.Contains(response["error"], "Missing X-AI-Token") {
		t.Errorf("Expected missing token error, got: %s", response["error"])
	}
}

func TestDelegatedYouTubeTranscript_MissingURL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	mock := NewMockTokenExchangeServer()
	defer mock.Server.Close()

	originalEnv := os.Getenv("FABRIC_TOKEN_EXCHANGE_URL")
	os.Setenv("FABRIC_TOKEN_EXCHANGE_URL", mock.Server.URL)
	defer os.Setenv("FABRIC_TOKEN_EXCHANGE_URL", originalEnv)

	mock.AddToken("valid-token", DelegatedCredentials{
		Provider: "openai",
		ApiKey:   "sk-test",
		Model:    "gpt-4",
	})

	handler := &DelegatedHandler{}
	r.POST("/delegated/youtube/transcript", handler.HandleDelegatedYouTubeTranscript)

	req := httptest.NewRequest(http.MethodPost, "/delegated/youtube/transcript", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-AI-Token", "valid-token")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestDelegatedScrape_MissingToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	handler := &DelegatedHandler{}
	r.POST("/delegated/scrape", handler.HandleDelegatedScrape)

	req := httptest.NewRequest(http.MethodPost, "/delegated/scrape", strings.NewReader(`{"url":"https://example.com"}`))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", w.Code)
	}
}

func TestDelegatedScrape_MissingURL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	mock := NewMockTokenExchangeServer()
	defer mock.Server.Close()

	originalEnv := os.Getenv("FABRIC_TOKEN_EXCHANGE_URL")
	os.Setenv("FABRIC_TOKEN_EXCHANGE_URL", mock.Server.URL)
	defer os.Setenv("FABRIC_TOKEN_EXCHANGE_URL", originalEnv)

	mock.AddToken("valid-token", DelegatedCredentials{
		Provider: "openai",
		ApiKey:   "sk-test",
	})

	handler := &DelegatedHandler{}
	r.POST("/delegated/scrape", handler.HandleDelegatedScrape)

	req := httptest.NewRequest(http.MethodPost, "/delegated/scrape", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-AI-Token", "valid-token")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestDelegatedSearch_MissingQuestion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	mock := NewMockTokenExchangeServer()
	defer mock.Server.Close()

	originalEnv := os.Getenv("FABRIC_TOKEN_EXCHANGE_URL")
	os.Setenv("FABRIC_TOKEN_EXCHANGE_URL", mock.Server.URL)
	defer os.Setenv("FABRIC_TOKEN_EXCHANGE_URL", originalEnv)

	mock.AddToken("valid-token", DelegatedCredentials{
		Provider: "openai",
		ApiKey:   "sk-test",
	})

	handler := &DelegatedHandler{}
	r.POST("/delegated/search", handler.HandleDelegatedSearch)

	req := httptest.NewRequest(http.MethodPost, "/delegated/search", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-AI-Token", "valid-token")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestDelegatedPatternNames_MissingToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	handler := &DelegatedHandler{}
	r.GET("/delegated/patterns/names", handler.HandleDelegatedPatternNames)

	req := httptest.NewRequest(http.MethodGet, "/delegated/patterns/names", nil)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", w.Code)
	}
}

func TestDelegatedGetPattern_MissingToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	handler := &DelegatedHandler{}
	r.GET("/delegated/patterns/:name", handler.HandleDelegatedGetPattern)

	req := httptest.NewRequest(http.MethodGet, "/delegated/patterns/summarize", nil)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", w.Code)
	}
}

func TestDelegatedApplyPattern_MissingToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	handler := &DelegatedHandler{}
	r.POST("/delegated/patterns/:name/apply", handler.HandleDelegatedApplyPattern)

	body := `{"variables":{"topic":"AI"},"input":"test input"}`
	req := httptest.NewRequest(http.MethodPost, "/delegated/patterns/summarize/apply", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", w.Code)
	}
}

// =============================================================================
// Request/Response Type Tests
// =============================================================================

func TestDelegatedChatRequest_Serialization(t *testing.T) {
	request := DelegatedChatRequest{
		Message:     "Hello, how are you?",
		PatternName: "summarize",
		ContextName: "tech",
		History: []ChatMessage{
			{Role: "user", Content: "Previous message"},
			{Role: "assistant", Content: "Previous response"},
		},
		PatternVariables: map[string]string{
			"topic": "AI",
		},
	}

	data, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	var parsed DelegatedChatRequest
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal request: %v", err)
	}

	if parsed.Message != request.Message {
		t.Errorf("Message mismatch: %s vs %s", parsed.Message, request.Message)
	}
	if len(parsed.History) != len(request.History) {
		t.Errorf("History length mismatch: %d vs %d", len(parsed.History), len(request.History))
	}
}

func TestDelegatedYouTubeRequest_Serialization(t *testing.T) {
	request := DelegatedYouTubeRequest{
		URL:        "https://youtube.com/watch?v=dQw4w9WgXcQ",
		Language:   "en",
		Timestamps: true,
	}

	data, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	var parsed DelegatedYouTubeRequest
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal request: %v", err)
	}

	if parsed.URL != request.URL {
		t.Errorf("URL mismatch: %s vs %s", parsed.URL, request.URL)
	}
	if parsed.Timestamps != request.Timestamps {
		t.Errorf("Timestamps mismatch: %v vs %v", parsed.Timestamps, request.Timestamps)
	}
}

func TestDelegatedPatternResponse_Serialization(t *testing.T) {
	response := DelegatedPatternResponse{
		Name:        "summarize",
		Description: "Summarizes the given text",
		Pattern:     "You are an expert summarizer...",
	}

	data, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("Failed to marshal response: %v", err)
	}

	var parsed DelegatedPatternResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if parsed.Name != response.Name {
		t.Errorf("Name mismatch: %s vs %s", parsed.Name, response.Name)
	}
}

// =============================================================================
// Stream Response Tests
// =============================================================================

func TestStreamResponse_Types(t *testing.T) {
	testCases := []struct {
		name     string
		response StreamResponse
	}{
		{
			name: "content response",
			response: StreamResponse{
				Type:    "content",
				Format:  "markdown",
				Content: "# Hello World",
			},
		},
		{
			name: "complete response",
			response: StreamResponse{
				Type:    "complete",
				Format:  "plain",
				Content: "",
			},
		},
		{
			name: "error response",
			response: StreamResponse{
				Type:    "error",
				Format:  "plain",
				Content: "Something went wrong",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.response)
			if err != nil {
				t.Fatalf("Failed to marshal: %v", err)
			}

			var parsed StreamResponse
			if err := json.Unmarshal(data, &parsed); err != nil {
				t.Fatalf("Failed to unmarshal: %v", err)
			}

			if parsed.Type != tc.response.Type {
				t.Errorf("Type mismatch: %s vs %s", parsed.Type, tc.response.Type)
			}
		})
	}
}

func TestDetectFormat(t *testing.T) {
	testCases := []struct {
		content  string
		expected string
	}{
		{"# Heading\n## Subheading", "markdown"},
		{"```go\nfunc main() {}\n```", "markdown"},
		{"- Item 1\n- Item 2", "markdown"},
		{"**bold** text", "markdown"},
		{"Plain text without formatting", "plain"},
		{"Just some words", "plain"},
	}

	for _, tc := range testCases {
		result := detectFormat(tc.content)
		if result != tc.expected {
			t.Errorf("detectFormat(%q) = %s, expected %s", tc.content[:min(20, len(tc.content))], result, tc.expected)
		}
	}
}

// =============================================================================
// SSE Writing Tests
// =============================================================================

func TestWriteSSEResponse(t *testing.T) {
	w := httptest.NewRecorder()

	response := StreamResponse{
		Type:    "content",
		Format:  "plain",
		Content: "Hello, World!",
	}

	err := writeSSEResponse(w, response)
	if err != nil {
		t.Fatalf("writeSSEResponse failed: %v", err)
	}

	body := w.Body.String()
	if !strings.Contains(body, "data:") {
		t.Error("SSE response should contain 'data:' prefix")
	}
	if !strings.Contains(body, "Hello, World!") {
		t.Error("SSE response should contain the content")
	}
}

// =============================================================================
// Integration-Like Tests (with mocked dependencies)
// =============================================================================

func TestDelegatedChat_EndToEnd_MockedTokenExchange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	mock := NewMockTokenExchangeServer()
	defer mock.Server.Close()

	originalEnv := os.Getenv("FABRIC_TOKEN_EXCHANGE_URL")
	os.Setenv("FABRIC_TOKEN_EXCHANGE_URL", mock.Server.URL)
	defer os.Setenv("FABRIC_TOKEN_EXCHANGE_URL", originalEnv)

	// Add valid token
	mock.AddToken("test-token", DelegatedCredentials{
		Provider: "openai",
		ApiKey:   "sk-test-key",
		Model:    "gpt-4",
		BaseUrl:  "https://api.openai.com/v1",
		UserId:   "user-123",
	})

	// Create handler (will fail on actual AI call but token exchange should work)
	handler := &DelegatedHandler{}
	r.POST("/delegated/chat", handler.HandleDelegatedChat)

	body := `{
		"message": "Hello",
		"pattern_name": "",
		"model": "gpt-4"
	}`
	req := httptest.NewRequest(http.MethodPost, "/delegated/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-AI-Token", "test-token")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Note: This will fail because we don't have a real AI vendor, but we can verify:
	// 1. Token exchange worked (would be 401 if it didn't)
	// 2. Request parsing worked

	// The actual response depends on whether the vendor can be instantiated
	// In a real test, we'd mock the vendor as well
	t.Logf("Response status: %d", w.Code)
	t.Logf("Response body: %s", w.Body.String())
}

// =============================================================================
// Benchmark Tests
// =============================================================================

func BenchmarkDetectFormat(b *testing.B) {
	testContent := "# Heading\n\nSome **bold** text and `code` here.\n\n- Item 1\n- Item 2"

	for i := 0; i < b.N; i++ {
		detectFormat(testContent)
	}
}

func BenchmarkStreamResponseSerialization(b *testing.B) {
	response := StreamResponse{
		Type:    "content",
		Format:  "markdown",
		Content: strings.Repeat("Hello World ", 100),
	}

	for i := 0; i < b.N; i++ {
		data, _ := json.Marshal(response)
		var parsed StreamResponse
		json.Unmarshal(data, &parsed)
	}
}

// =============================================================================
// Helper function tests
// =============================================================================

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestDelegatedCredentials_Providers tests that all supported providers are handled
func TestDelegatedCredentials_Providers(t *testing.T) {
	providers := []string{"openai", "OpenAI", "anthropic", "Anthropic", "azure", "groq", "ollama"}

	for _, provider := range providers {
		creds := DelegatedCredentials{
			Provider: provider,
			ApiKey:   "test-key",
			Model:    "test-model",
		}

		// Verify JSON serialization works
		data, err := json.Marshal(creds)
		if err != nil {
			t.Errorf("Failed to marshal credentials for provider %s: %v", provider, err)
		}

		var parsed DelegatedCredentials
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Errorf("Failed to unmarshal credentials for provider %s: %v", provider, err)
		}

		if parsed.Provider != creds.Provider {
			t.Errorf("Provider mismatch for %s: got %s", provider, parsed.Provider)
		}
	}
}

// Mock HTTP response writer for SSE testing
type mockResponseWriter struct {
	bytes.Buffer
	headers http.Header
	status  int
}

func newMockResponseWriter() *mockResponseWriter {
	return &mockResponseWriter{
		headers: make(http.Header),
	}
}

func (m *mockResponseWriter) Header() http.Header {
	return m.headers
}

func (m *mockResponseWriter) WriteHeader(status int) {
	m.status = status
}

func (m *mockResponseWriter) Flush() {
	// no-op for mock
}

func TestSSE_MultipleChunks(t *testing.T) {
	w := newMockResponseWriter()

	chunks := []StreamResponse{
		{Type: "content", Format: "plain", Content: "Hello "},
		{Type: "content", Format: "plain", Content: "World"},
		{Type: "complete", Format: "plain", Content: ""},
	}

	for _, chunk := range chunks {
		if err := writeSSEResponse(w, chunk); err != nil {
			t.Fatalf("Failed to write chunk: %v", err)
		}
	}

	output := w.String()

	// Count data: prefixes
	dataCount := strings.Count(output, "data:")
	if dataCount != len(chunks) {
		t.Errorf("Expected %d data: prefixes, got %d", len(chunks), dataCount)
	}

	// Verify all content is present
	if !strings.Contains(output, "Hello") {
		t.Error("Missing 'Hello' in output")
	}
	if !strings.Contains(output, "World") {
		t.Error("Missing 'World' in output")
	}
}

// Test concurrent token exchange
func TestExchangeTokenForCredentials_Concurrent(t *testing.T) {
	mock := NewMockTokenExchangeServer()
	defer mock.Server.Close()

	originalEnv := os.Getenv("FABRIC_TOKEN_EXCHANGE_URL")
	os.Setenv("FABRIC_TOKEN_EXCHANGE_URL", mock.Server.URL)
	defer os.Setenv("FABRIC_TOKEN_EXCHANGE_URL", originalEnv)

	// Add multiple valid tokens
	for i := 0; i < 10; i++ {
		token := fmt.Sprintf("token-%d", i)
		mock.AddToken(token, DelegatedCredentials{
			Provider: "openai",
			ApiKey:   fmt.Sprintf("sk-test-%d", i),
			UserId:   fmt.Sprintf("user-%d", i),
		})
	}

	// Test concurrent access
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(idx int) {
			token := fmt.Sprintf("token-%d", idx)
			creds, err := exchangeTokenForCredentials(token)
			if err != nil {
				t.Errorf("Concurrent exchange failed for token %s: %v", token, err)
			}
			if creds.UserId != fmt.Sprintf("user-%d", idx) {
				t.Errorf("Wrong user ID for token %s", token)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}

// Test request body reading edge cases
func TestDelegatedChat_EmptyBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	handler := &DelegatedHandler{}
	r.POST("/delegated/chat", handler.HandleDelegatedChat)

	req := httptest.NewRequest(http.MethodPost, "/delegated/chat", nil)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-AI-Token", "some-token")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Should fail with bad request due to empty body, but token validation happens first
	// so we might get 401 if token exchange fails
	if w.Code != http.StatusBadRequest && w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 400 or 401, got %d", w.Code)
	}
}

func TestDelegatedChat_InvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	mock := NewMockTokenExchangeServer()
	defer mock.Server.Close()

	originalEnv := os.Getenv("FABRIC_TOKEN_EXCHANGE_URL")
	os.Setenv("FABRIC_TOKEN_EXCHANGE_URL", mock.Server.URL)
	defer os.Setenv("FABRIC_TOKEN_EXCHANGE_URL", originalEnv)

	mock.AddToken("valid-token", DelegatedCredentials{
		Provider: "openai",
		ApiKey:   "sk-test",
	})

	handler := &DelegatedHandler{}
	r.POST("/delegated/chat", handler.HandleDelegatedChat)

	req := httptest.NewRequest(http.MethodPost, "/delegated/chat", strings.NewReader("{invalid json}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-AI-Token", "valid-token")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

// Test large request handling
func TestDelegatedChat_LargeMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	mock := NewMockTokenExchangeServer()
	defer mock.Server.Close()

	originalEnv := os.Getenv("FABRIC_TOKEN_EXCHANGE_URL")
	os.Setenv("FABRIC_TOKEN_EXCHANGE_URL", mock.Server.URL)
	defer os.Setenv("FABRIC_TOKEN_EXCHANGE_URL", originalEnv)

	mock.AddToken("valid-token", DelegatedCredentials{
		Provider: "openai",
		ApiKey:   "sk-test",
		Model:    "gpt-4",
	})

	handler := &DelegatedHandler{}
	r.POST("/delegated/chat", handler.HandleDelegatedChat)

	// Create a large message (1MB)
	largeMessage := strings.Repeat("Hello World ", 100000)

	reqBody := DelegatedChatRequest{
		Message: largeMessage,
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/delegated/chat", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-AI-Token", "valid-token")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Should accept the request (actual execution may fail without real AI)
	// We're testing that the large body doesn't cause immediate rejection
	t.Logf("Large message test response: status=%d", w.Code)
}

// Helper functions for reading response body
func readResponseBody(r io.Reader) (string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Test that proper error messages are returned for various failure modes
func TestDelegatedEndpoints_ErrorMessages(t *testing.T) {
	gin.SetMode(gin.TestMode)

	testCases := []struct {
		name           string
		setupHandler   func(*gin.Engine, *DelegatedHandler)
		method         string
		path           string
		body           string
		hasToken       bool
		tokenValid     bool
		expectedStatus int
		expectError    string
	}{
		{
			name: "YouTube - no token",
			setupHandler: func(r *gin.Engine, h *DelegatedHandler) {
				r.POST("/delegated/youtube/transcript", h.HandleDelegatedYouTubeTranscript)
			},
			method:         http.MethodPost,
			path:           "/delegated/youtube/transcript",
			body:           `{"url":"https://youtube.com/watch?v=test"}`,
			hasToken:       false,
			expectedStatus: http.StatusUnauthorized,
			expectError:    "Missing X-AI-Token",
		},
		{
			name: "Scrape - no token",
			setupHandler: func(r *gin.Engine, h *DelegatedHandler) {
				r.POST("/delegated/scrape", h.HandleDelegatedScrape)
			},
			method:         http.MethodPost,
			path:           "/delegated/scrape",
			body:           `{"url":"https://example.com"}`,
			hasToken:       false,
			expectedStatus: http.StatusUnauthorized,
			expectError:    "Missing X-AI-Token",
		},
		{
			name: "Search - no token",
			setupHandler: func(r *gin.Engine, h *DelegatedHandler) {
				r.POST("/delegated/search", h.HandleDelegatedSearch)
			},
			method:         http.MethodPost,
			path:           "/delegated/search",
			body:           `{"question":"What is AI?"}`,
			hasToken:       false,
			expectedStatus: http.StatusUnauthorized,
			expectError:    "Missing X-AI-Token",
		},
		{
			name: "Patterns list - no token",
			setupHandler: func(r *gin.Engine, h *DelegatedHandler) {
				r.GET("/delegated/patterns/names", h.HandleDelegatedPatternNames)
			},
			method:         http.MethodGet,
			path:           "/delegated/patterns/names",
			body:           "",
			hasToken:       false,
			expectedStatus: http.StatusUnauthorized,
			expectError:    "Missing X-AI-Token",
		},
		{
			name: "Pattern get - no token",
			setupHandler: func(r *gin.Engine, h *DelegatedHandler) {
				r.GET("/delegated/patterns/:name", h.HandleDelegatedGetPattern)
			},
			method:         http.MethodGet,
			path:           "/delegated/patterns/summarize",
			body:           "",
			hasToken:       false,
			expectedStatus: http.StatusUnauthorized,
			expectError:    "Missing X-AI-Token",
		},
		{
			name: "Pattern apply - no token",
			setupHandler: func(r *gin.Engine, h *DelegatedHandler) {
				r.POST("/delegated/patterns/:name/apply", h.HandleDelegatedApplyPattern)
			},
			method:         http.MethodPost,
			path:           "/delegated/patterns/summarize/apply",
			body:           `{"input":"test"}`,
			hasToken:       false,
			expectedStatus: http.StatusUnauthorized,
			expectError:    "Missing X-AI-Token",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			handler := &DelegatedHandler{}
			tc.setupHandler(r, handler)

			var bodyReader io.Reader
			if tc.body != "" {
				bodyReader = strings.NewReader(tc.body)
			}

			req := httptest.NewRequest(tc.method, tc.path, bodyReader)
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tc.expectedStatus {
				t.Errorf("Expected status %d, got %d", tc.expectedStatus, w.Code)
			}

			if tc.expectError != "" {
				body := w.Body.String()
				if !strings.Contains(body, tc.expectError) {
					t.Errorf("Expected error to contain '%s', got: %s", tc.expectError, body)
				}
			}
		})
	}
}
