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
# Production Image
# =============================================================================
FROM alpine:latest

LABEL org.opencontainers.image.description="Fabric AI - Stateless container for AI augmentation. Patterns bundled, configurable via environment variables."
LABEL org.opencontainers.image.source="https://github.com/danielmiessler/fabric"

# Install runtime dependencies
RUN apk add --no-cache ca-certificates yt-dlp curl \
    && mkdir -p /root/.config/fabric/patterns \
    && mkdir -p /root/.config/fabric/contexts \
    && mkdir -p /root/.config/fabric/sessions \
    && mkdir -p /root/.config/fabric/strategies

# Copy the binary
COPY --from=builder /fabric /usr/local/bin/fabric

# Bundle patterns into the image (read-only, no download needed)
COPY --from=builder /src/data/patterns /root/.config/fabric/patterns

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

EXPOSE 8080

# Health check for container orchestration
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8080/health || exit 1

ENTRYPOINT ["fabric"]
CMD ["--serve", "--address", ":8080"]
