# =============================================================================
# Fabric AI Web UI - Development/Testing Container
# =============================================================================
# This Dockerfile runs the SvelteKit web UI in development mode
# For production, build the static files and serve with nginx
# =============================================================================

FROM node:20-alpine

WORKDIR /app

# Install git (required for git-hosted dependencies like pdf-to-markdown-core)
RUN apk add --no-cache git

# Copy package files (pnpm-lock.yaml exists but we use npm for git-hosted deps compatibility)
COPY package.json ./

# Install dependencies using npm (more compatible with git-hosted packages)
# --legacy-peer-deps handles potential peer dependency conflicts
RUN npm install --legacy-peer-deps

# Copy source code
COPY . .

# Expose development port
EXPOSE 5173

# Environment variable for backend URL (set at runtime)
ENV FABRIC_BASE_URL=http://localhost:8080

# Start development server with host binding for Docker
CMD ["npm", "run", "dev", "--", "--host", "0.0.0.0"]
