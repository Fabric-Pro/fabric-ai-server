# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Development Commands

### Backend (Go)

```bash
# Build CLI binary
go build -o fabric ./cmd/fabric

# Run all tests
go test ./...

# Run tests for specific package
go test ./internal/cli

# Run with coverage
go test -cover ./...

# Format code
gofmt -w .

# Generate Swagger docs (after modifying API endpoints)
swag init -g internal/server/serve.go -o docs

# Start REST API server
./fabric --serve --address :8080

# Interactive setup wizard
./fabric --setup
```

### Frontend (web/)

```bash
cd web

# Install dependencies
npm install  # or pnpm install

# Development server (proxies to backend at :8080)
npm run dev

# Type checking
npm run check

# Build for production
npm run build

# Run tests
npm run test

# Lint and format
npm run lint
npm run format
```

### Docker

```bash
# Build container image
docker build -t fabric:latest -f scripts/docker/Dockerfile .

# Run containerized
docker run -p 8080:8080 -e OPENAI_API_KEY=sk-... fabric:latest --serve
```

## Architecture Overview

Fabric is a dual-stack application: Go backend REST API + SvelteKit web frontend.

### Core Components

```
cmd/fabric/          → CLI entry point (main.go)
internal/
├── cli/             → CLI command handlers, flags (100+ flags with YAML config support)
├── core/            → PluginRegistry orchestrates all plugins, Chatter handles AI conversations
├── server/          → Gin REST API (chat.go, patterns.go, models.go, etc.)
├── plugins/
│   ├── ai/          → AI vendor implementations (14+ providers)
│   ├── db/fsdb/     → File-based storage (patterns, sessions, contexts)
│   ├── template/    → Pattern variable substitution
│   └── strategy/    → Execution strategies
├── domain/          → Shared data models (ChatRequest, ChatOptions)
├── chat/            → Chat message types
└── tools/           → YouTube, Jina web scraping, converters
web/
├── src/lib/api/     → Backend API client
├── src/lib/services/→ ChatService with SSE streaming
├── src/routes/      → SvelteKit pages
└── vite.config.ts   → Dev server proxies /api/* to :8080
data/patterns/       → 200+ AI prompt patterns (each has system.md)
```

### Plugin System

`PluginRegistry` (internal/core/plugin_registry.go) initializes and manages:
- **VendorManager**: AI providers (OpenAI, Anthropic, Gemini, Ollama, Bedrock, etc.)
- **Db**: File-based storage for patterns, sessions, contexts
- **Strategies**: Custom execution workflows

Each AI vendor implements the `Vendor` interface:
```go
type Vendor interface {
    ListModels() ([]string, error)
    SendStream([]*chat.ChatCompletionMessage, *domain.ChatOptions, chan string) error
    Send(context.Context, []*chat.ChatCompletionMessage, *domain.ChatOptions) (string, error)
}
```

### REST API

Server runs on port 8080 with Swagger docs at `/swagger/index.html`:
- `POST /chat` - SSE streaming chat completions
- `GET /patterns/names` - List available patterns
- `GET /models/names` - List available models
- `GET /health`, `/ready` - Health checks

### Data Flow

1. CLI/Web request → PluginRegistry.GetChatter() → creates Chatter with selected vendor
2. Chatter.BuildSession() → loads pattern, context, applies variables
3. Chatter.Send() → vendor.SendStream() → streams response via SSE
4. Session optionally saved to fsdb

## Key Patterns

### Adding a New AI Provider

1. Create package in `internal/plugins/ai/newvendor/`
2. Implement `Vendor` interface
3. Register in `internal/plugins/ai/vendors.go`
4. Add setup questions in plugin's `Setup()` method

### Adding a REST Endpoint

1. Create handler in `internal/server/`
2. Add Swagger annotations (see CONTRIBUTING.md)
3. Register route in `serve.go`
4. Regenerate docs: `swag init -g internal/server/serve.go -o docs`

### Pattern Structure

Each pattern in `data/patterns/[name]/system.md`:
```markdown
# IDENTITY and PURPOSE
You are an expert at...

# STEPS
- Step 1
- Step 2

# OUTPUT
- Output format requirements
```

## Configuration

- **Config file**: `~/.config/fabric/config.yaml`
- **Env file**: `~/.config/fabric/.env` (API keys)
- **Patterns**: `~/.config/fabric/patterns/`

Environment variables: `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `GEMINI_API_KEY`, `GROQ_API_KEY`, etc.

## PR Requirements

- PRs touching 50+ files without justification will be rejected
- After opening PR, generate changelog: `go run ./cmd/generate_changelog --ai-summarize --incoming-pr YOUR_PR_NUMBER`
- Use conventional commits: `feat:`, `fix:`, `docs:`, `chore:`
