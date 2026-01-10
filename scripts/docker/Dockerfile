# syntax=docker/dockerfile:1

# =============================================================================
# Fabric AI - Stateless Container Image
# =============================================================================
# This Dockerfile creates a stateless, horizontally scalable container image
# suitable for deployment on Azure Container Apps, Google Cloud Run, AWS ECS, etc.
#
# Features:
# - Built-in patterns bundled in the image (read-only)
# - No persistent storage required
# - Configurable via environment variables
# - Health check endpoint for orchestration
# =============================================================================

FROM golang:1.25-alpine AS builder

WORKDIR /src

# Install build dependencies
RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /fabric ./cmd/fabric

# =============================================================================
# Production Image (Debian-based for glibc compatibility with mcp-cli)
# =============================================================================
FROM debian:bookworm-slim

LABEL org.opencontainers.image.description="Fabric AI - Stateless container for AI augmentation. Patterns bundled, configurable via environment variables."
LABEL org.opencontainers.image.source="https://github.com/danielmiessler/fabric"

# Install runtime dependencies
# Using Debian for glibc compatibility (required for mcp-cli binary)
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    nodejs \
    npm \
    python3 \
    python3-pip \
    ffmpeg \
    && pip3 install --break-system-packages yt-dlp \
    && rm -rf /var/lib/apt/lists/* \
    && mkdir -p /root/.config/fabric/patterns \
    && mkdir -p /root/.config/fabric/contexts \
    && mkdir -p /root/.config/fabric/sessions \
    && mkdir -p /root/.config/fabric/strategies \
    && mkdir -p /root/.config/mcp

# Install mcp-cli for dynamic MCP server discovery
# This enables fabric_mcp_* tools for runtime MCP tool discovery
RUN MCP_CLI_VERSION="v0.1.3" \
    && curl -fsSL "https://github.com/philschmid/mcp-cli/releases/download/${MCP_CLI_VERSION}/mcp-cli-linux-x64" -o /usr/local/bin/mcp-cli \
    && chmod +x /usr/local/bin/mcp-cli \
    && /usr/local/bin/mcp-cli --version

# Create default MCP servers config (HTTP-based servers)
RUN echo '{"mcpServers":{"context7":{"url":"https://mcp.context7.com/mcp"},"deepwiki":{"url":"https://mcp.deepwiki.com/mcp"}}}' > /root/.config/mcp/mcp_servers.json

# Copy the binary
COPY --from=builder /fabric /usr/local/bin/fabric

# Bundle patterns into the image (read-only, no download needed)
COPY --from=builder /src/data/patterns /root/.config/fabric/patterns

# Bundle strategies into the image (CoT, ToT, Reflexion, etc.)
COPY --from=builder /src/data/strategies /root/.config/fabric/strategies

# Bundle contexts (personas) into the image
COPY --from=builder /src/data/contexts /root/.config/fabric/contexts

# Copy swagger docs for API documentation
COPY --from=builder /src/docs /app/docs

# Create empty .env file (credentials come from environment variables)
RUN touch /root/.config/fabric/.env

# Set working directory for swagger docs
WORKDIR /app

# Environment variables for configuration
# AI Provider credentials (set these in your deployment)
# ENV OPENAI_API_KEY=""
# ENV ANTHROPIC_API_KEY=""
# ENV GEMINI_API_KEY=""
# ENV GROQ_API_KEY=""

# Server configuration
ENV GIN_MODE=release
# API Key authentication (recommended for production)
# Set FABRIC_SERVER_API_KEY to enable API key authentication
# Clients must send this key in the X-API-Key header
# ENV FABRIC_SERVER_API_KEY=""

EXPOSE 8080

# Health check for container orchestration
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8080/health || exit 1

ENTRYPOINT ["fabric"]
CMD ["--serve", "--address", ":8080"]
