// Package mcpcli provides MCP CLI integration for dynamic MCP server discovery and tool execution.
//
// mcp-cli is a lightweight CLI for interacting with MCP (Model Context Protocol) servers.
// It enables dynamic discovery of MCP tools, reducing context window bloat by loading
// tool schemas on-demand rather than upfront.
//
// Requirements:
// - mcp-cli: Must be installed and available in PATH (install via: curl -fsSL https://raw.githubusercontent.com/philschmid/mcp-cli/main/install.sh | bash)
// - mcp_servers.json: Config file in current directory or ~/.config/mcp/
//
// Key features:
// - Dynamic tool discovery (mcp-cli, mcp-cli grep)
// - On-demand schema loading (mcp-cli server/tool)
// - Tool execution (mcp-cli server/tool '<json>')
// - Reduces token usage by ~99% compared to static MCP loading
package mcpcli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/danielmiessler/fabric/internal/plugins"
)

const (
	DefaultTimeout     = 30 * time.Second
	DefaultConcurrency = 5
	DefaultMaxRetries  = 3
)

// Client provides methods for interacting with mcp-cli
type Client struct {
	*plugins.PluginBase
	ConfigPath *plugins.SetupQuestion
}

// NewClient creates a new mcp-cli client
func NewClient() *Client {
	label := "MCP CLI"

	client := &Client{
		PluginBase: &plugins.PluginBase{
			Name:             label,
			SetupDescription: "MCP CLI - Dynamic discovery and execution of MCP server tools",
			EnvNamePrefix:    plugins.BuildEnvVariablePrefix(label),
		},
	}

	client.ConfigPath = client.AddSetupQuestion("Config Path (optional, uses default locations if empty)", false)

	return client
}

// getMcpCliBinary returns the path to the mcp-cli binary.
// It checks (in order):
// 1. MCP_CLI_PATH environment variable
// 2. Standard PATH lookup
// 3. Common installation locations (for containers/Azure)
func getMcpCliBinary() (string, error) {
	// Check environment variable first
	if path := os.Getenv("MCP_CLI_PATH"); path != "" {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	// Try standard PATH lookup
	if path, err := exec.LookPath("mcp-cli"); err == nil {
		return path, nil
	}

	// Check common installation locations (useful for containers/Azure)
	commonPaths := []string{
		"/usr/local/bin/mcp-cli",
		"/usr/bin/mcp-cli",
		"/opt/mcp-cli/mcp-cli",
		"/app/mcp-cli",                    // Azure container apps
		"/home/site/wwwroot/mcp-cli",      // Azure App Service
		filepath.Join(os.Getenv("HOME"), ".local/bin/mcp-cli"),
		filepath.Join(os.Getenv("HOME"), "bin/mcp-cli"),
	}

	for _, p := range commonPaths {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	return "", fmt.Errorf("mcp-cli not found. Set MCP_CLI_PATH env var or install via: curl -fsSL https://raw.githubusercontent.com/philschmid/mcp-cli/main/install.sh | bash")
}

// IsAvailable checks if mcp-cli is installed and available
func IsAvailable() bool {
	_, err := getMcpCliBinary()
	return err == nil
}

// ToolInfo represents information about an MCP tool
type ToolInfo struct {
	Name        string                 `json:"name"`
	Server      string                 `json:"server"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"inputSchema,omitempty"`
}

// ServerInfo represents information about an MCP server
type ServerInfo struct {
	Name  string     `json:"name"`
	Tools []ToolInfo `json:"tools"`
}

// CallResult represents the result of calling an MCP tool
type CallResult struct {
	Content interface{} `json:"content"`
	IsError bool        `json:"isError,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// ListServers lists all available MCP servers and their tools
func (c *Client) ListServers(ctx context.Context, withDescriptions bool) ([]ServerInfo, error) {
	return ListServers(ctx, c.getConfigPath(), withDescriptions)
}

// GrepTools searches for tools matching a glob pattern across all servers
func (c *Client) GrepTools(ctx context.Context, pattern string, withDescriptions bool) ([]ToolInfo, error) {
	return GrepTools(ctx, c.getConfigPath(), pattern, withDescriptions)
}

// GetToolSchema gets the JSON schema for a specific tool
func (c *Client) GetToolSchema(ctx context.Context, serverTool string) (*ToolInfo, error) {
	return GetToolSchema(ctx, c.getConfigPath(), serverTool)
}

// CallTool executes an MCP tool with the given arguments
func (c *Client) CallTool(ctx context.Context, serverTool string, args map[string]interface{}) (*CallResult, error) {
	return CallTool(ctx, c.getConfigPath(), serverTool, args)
}

func (c *Client) getConfigPath() string {
	if c.ConfigPath != nil && c.ConfigPath.Value != "" {
		return c.ConfigPath.Value
	}
	return ""
}

// ListServers lists all available MCP servers and their tools (standalone function)
func ListServers(ctx context.Context, configPath string, withDescriptions bool) ([]ServerInfo, error) {
	if !IsAvailable() {
		return nil, fmt.Errorf("mcp-cli is not installed. Install via: curl -fsSL https://raw.githubusercontent.com/philschmid/mcp-cli/main/install.sh | bash")
	}

	args := []string{"--json"}
	if withDescriptions {
		args = append(args, "-d")
	}
	if configPath != "" {
		args = append(args, "-c", configPath)
	}

	output, err := runMcpCli(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list servers: %w", err)
	}

	var result struct {
		Servers []ServerInfo `json:"servers"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		// Try parsing as plain text output and convert
		return parseTextServerList(string(output), withDescriptions), nil
	}

	return result.Servers, nil
}

// GrepTools searches for tools matching a glob pattern (standalone function)
func GrepTools(ctx context.Context, configPath string, pattern string, withDescriptions bool) ([]ToolInfo, error) {
	if !IsAvailable() {
		return nil, fmt.Errorf("mcp-cli is not installed")
	}

	args := []string{"grep", pattern, "--json"}
	if withDescriptions {
		args = append(args, "-d")
	}
	if configPath != "" {
		args = append(args, "-c", configPath)
	}

	output, err := runMcpCli(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to grep tools: %w", err)
	}

	var result struct {
		Tools []ToolInfo `json:"tools"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		// Try parsing as plain text
		return parseTextToolList(string(output), withDescriptions), nil
	}

	return result.Tools, nil
}

// GetToolSchema gets the JSON schema for a specific tool (standalone function)
func GetToolSchema(ctx context.Context, configPath string, serverTool string) (*ToolInfo, error) {
	if !IsAvailable() {
		return nil, fmt.Errorf("mcp-cli is not installed")
	}

	args := []string{serverTool, "--json"}
	if configPath != "" {
		args = append(args, "-c", configPath)
	}

	output, err := runMcpCli(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get tool schema: %w", err)
	}

	var tool ToolInfo
	if err := json.Unmarshal(output, &tool); err != nil {
		// Parse text format
		return parseTextToolSchema(string(output), serverTool)
	}

	return &tool, nil
}

// CallTool executes an MCP tool with the given arguments (standalone function)
func CallTool(ctx context.Context, configPath string, serverTool string, args map[string]interface{}) (*CallResult, error) {
	if !IsAvailable() {
		return nil, fmt.Errorf("mcp-cli is not installed")
	}

	argsJSON, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal arguments: %w", err)
	}

	cmdArgs := []string{serverTool, string(argsJSON), "--json"}
	if configPath != "" {
		cmdArgs = append(cmdArgs, "-c", configPath)
	}

	output, err := runMcpCli(ctx, cmdArgs...)
	if err != nil {
		return &CallResult{
			IsError: true,
			Error:   err.Error(),
		}, nil
	}

	var result CallResult
	if err := json.Unmarshal(output, &result); err != nil {
		// Return raw output as content
		result.Content = string(output)
	}

	return &result, nil
}

// CallToolRaw executes an MCP tool and returns raw text output
func CallToolRaw(ctx context.Context, configPath string, serverTool string, args map[string]interface{}) (string, error) {
	if !IsAvailable() {
		return "", fmt.Errorf("mcp-cli is not installed")
	}

	argsJSON, err := json.Marshal(args)
	if err != nil {
		return "", fmt.Errorf("failed to marshal arguments: %w", err)
	}

	cmdArgs := []string{serverTool, string(argsJSON)}
	if configPath != "" {
		cmdArgs = append(cmdArgs, "-c", configPath)
	}

	output, err := runMcpCli(ctx, cmdArgs...)
	if err != nil {
		return "", err
	}

	return string(output), nil
}

// GetServerTools gets all tools for a specific server
func GetServerTools(ctx context.Context, configPath string, serverName string, withDescriptions bool) ([]ToolInfo, error) {
	if !IsAvailable() {
		return nil, fmt.Errorf("mcp-cli is not installed")
	}

	args := []string{serverName, "--json"}
	if withDescriptions {
		args = append(args, "-d")
	}
	if configPath != "" {
		args = append(args, "-c", configPath)
	}

	output, err := runMcpCli(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get server tools: %w", err)
	}

	var result struct {
		Tools []ToolInfo `json:"tools"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		return parseTextToolList(string(output), withDescriptions), nil
	}

	return result.Tools, nil
}

// runMcpCli executes mcp-cli with the given arguments
func runMcpCli(ctx context.Context, args ...string) ([]byte, error) {
	// Get the mcp-cli binary path
	binaryPath, err := getMcpCliBinary()
	if err != nil {
		return nil, fmt.Errorf("mcp-cli is not installed: %w", err)
	}

	// Set timeout if not already set
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultTimeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, binaryPath, args...)

	// Inherit environment with MCP-specific settings
	cmd.Env = os.Environ()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Check for specific error types
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("mcp-cli command timed out")
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			// Return stderr content for better error messages
			errMsg := strings.TrimSpace(stderr.String())
			if errMsg == "" {
				errMsg = fmt.Sprintf("exit code %d", exitErr.ExitCode())
			}
			return nil, fmt.Errorf("mcp-cli error: %s", errMsg)
		}
		return nil, fmt.Errorf("failed to run mcp-cli: %w", err)
	}

	return stdout.Bytes(), nil
}

// parseTextServerList parses the text output of mcp-cli (no --json flag)
func parseTextServerList(output string, withDescriptions bool) []ServerInfo {
	servers := make([]ServerInfo, 0)
	lines := strings.Split(strings.TrimSpace(output), "\n")

	var currentServer *ServerInfo
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Server name (no indentation)
		if !strings.HasPrefix(line, "•") && !strings.HasPrefix(line, "-") && !strings.HasPrefix(line, " ") {
			if currentServer != nil {
				servers = append(servers, *currentServer)
			}
			currentServer = &ServerInfo{
				Name:  line,
				Tools: make([]ToolInfo, 0),
			}
		} else if currentServer != nil && (strings.HasPrefix(line, "•") || strings.HasPrefix(line, "-")) {
			// Tool entry
			toolLine := strings.TrimPrefix(strings.TrimPrefix(line, "•"), "-")
			toolLine = strings.TrimSpace(toolLine)

			parts := strings.SplitN(toolLine, " - ", 2)
			tool := ToolInfo{
				Name:   strings.TrimSpace(parts[0]),
				Server: currentServer.Name,
			}
			if len(parts) > 1 && withDescriptions {
				tool.Description = strings.TrimSpace(parts[1])
			}
			currentServer.Tools = append(currentServer.Tools, tool)
		}
	}

	if currentServer != nil {
		servers = append(servers, *currentServer)
	}

	return servers
}

// parseTextToolList parses text output for tool lists
func parseTextToolList(output string, withDescriptions bool) []ToolInfo {
	tools := make([]ToolInfo, 0)
	lines := strings.Split(strings.TrimSpace(output), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Format: server/tool or server/tool - description
		parts := strings.SplitN(line, " - ", 2)
		namePart := strings.TrimSpace(parts[0])

		serverTool := strings.SplitN(namePart, "/", 2)
		tool := ToolInfo{
			Name: namePart,
		}
		if len(serverTool) == 2 {
			tool.Server = serverTool[0]
			tool.Name = serverTool[1]
		}
		if len(parts) > 1 && withDescriptions {
			tool.Description = strings.TrimSpace(parts[1])
		}
		tools = append(tools, tool)
	}

	return tools
}

// parseTextToolSchema parses text output for a single tool schema
func parseTextToolSchema(output string, serverTool string) (*ToolInfo, error) {
	lines := strings.Split(strings.TrimSpace(output), "\n")

	tool := &ToolInfo{}

	// Parse server/tool name
	parts := strings.SplitN(serverTool, "/", 2)
	if len(parts) == 2 {
		tool.Server = parts[0]
		tool.Name = parts[1]
	} else {
		tool.Name = serverTool
	}

	// Look for description and schema in output
	inSchema := false
	var schemaBuilder strings.Builder

	for _, line := range lines {
		if strings.HasPrefix(line, "Description:") {
			tool.Description = strings.TrimSpace(strings.TrimPrefix(line, "Description:"))
		} else if strings.HasPrefix(line, "Input Schema:") {
			inSchema = true
		} else if inSchema {
			schemaBuilder.WriteString(line)
		}
	}

	// Parse schema if found
	if schemaBuilder.Len() > 0 {
		var schema map[string]interface{}
		if err := json.Unmarshal([]byte(schemaBuilder.String()), &schema); err == nil {
			tool.InputSchema = schema
		}
	}

	return tool, nil
}

// GetDefaultConfigPath returns the default mcp-cli config path
func GetDefaultConfigPath() string {
	// Check environment variable first
	if path := os.Getenv("MCP_CONFIG_PATH"); path != "" {
		return path
	}

	// Check current directory
	if _, err := os.Stat("mcp_servers.json"); err == nil {
		return "mcp_servers.json"
	}

	// Check ~/.mcp_servers.json
	if home, err := os.UserHomeDir(); err == nil {
		homePath := filepath.Join(home, ".mcp_servers.json")
		if _, err := os.Stat(homePath); err == nil {
			return homePath
		}

		// Check ~/.config/mcp/mcp_servers.json
		configPath := filepath.Join(home, ".config", "mcp", "mcp_servers.json")
		if _, err := os.Stat(configPath); err == nil {
			return configPath
		}
	}

	return ""
}
