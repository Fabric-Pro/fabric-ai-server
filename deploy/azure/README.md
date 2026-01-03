# Azure Container Apps Deployment

Deploy Fabric AI as a stateless, horizontally scalable container on Azure Container Apps.

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    Azure Container Apps                          │
├─────────────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐             │
│  │   Fabric    │  │   Fabric    │  │   Fabric    │  (replicas) │
│  │   App       │  │   App       │  │   App       │             │
│  │  Port 8080  │  │  Port 8080  │  │  Port 8080  │             │
│  └─────────────┘  └─────────────┘  └─────────────┘             │
│         │                │                │                     │
│         └────────────────┼────────────────┘                     │
│                          ▼                                      │
│         ┌────────────────────────────────┐                     │
│         │      Azure Key Vault           │                     │
│         │   (API Keys & Secrets)         │                     │
│         └────────────────────────────────┘                     │
└─────────────────────────────────────────────────────────────────┘
```

## Features

- **Stateless**: No persistent storage required, scales horizontally
- **Built-in Patterns**: All patterns bundled in the container image
- **Health Checks**: `/health` and `/ready` endpoints for orchestration
- **Secure**: API keys stored in Azure Key Vault
- **Auto-scaling**: Based on HTTP request concurrency

## Prerequisites

1. Azure CLI installed and logged in (`az login`)
2. Docker installed
3. Azure subscription

## Quick Deploy

```bash
# Set your configuration
export RESOURCE_GROUP="fabric-ai-rg"
export LOCATION="eastus"
export ACR_NAME="fabricairegistry"  # Must be globally unique

# Run the deployment script
./deploy.sh
```

## Manual Deployment

### 1. Create Azure Resources

```bash
# Create resource group
az group create --name fabric-ai-rg --location eastus

# Create Container Registry
az acr create --resource-group fabric-ai-rg --name fabricairegistry --sku Basic --admin-enabled true

# Create Container Apps Environment
az containerapp env create --name fabric-ai-env --resource-group fabric-ai-rg --location eastus
```

### 2. Build and Push Docker Image

```bash
# Navigate to project root
cd /path/to/fabric-ai

# Build the image
docker build -t fabricairegistry.azurecr.io/fabric:latest -f scripts/docker/Dockerfile .

# Login to ACR
az acr login --name fabricairegistry

# Push the image
docker push fabricairegistry.azurecr.io/fabric:latest
```

### 3. Create Key Vault and Secrets

```bash
# Create Key Vault
az keyvault create --name fabric-ai-kv --resource-group fabric-ai-rg --location eastus

# Add secrets
az keyvault secret set --vault-name fabric-ai-kv --name OPENAI-API-KEY --value "sk-..."
az keyvault secret set --vault-name fabric-ai-kv --name ANTHROPIC-API-KEY --value "sk-ant-..."
az keyvault secret set --vault-name fabric-ai-kv --name GEMINI-API-KEY --value "..."
```

### 4. Deploy Container App

```bash
az containerapp create \
    --name fabric-ai \
    --resource-group fabric-ai-rg \
    --environment fabric-ai-env \
    --image fabricairegistry.azurecr.io/fabric:latest \
    --registry-server fabricairegistry.azurecr.io \
    --target-port 8080 \
    --ingress external \
    --min-replicas 1 \
    --max-replicas 10 \
    --cpu 0.5 \
    --memory 1Gi \
    --env-vars "GIN_MODE=release" "OPENAI_API_KEY=secretref:openai-key"
```

## Environment Variables

| Variable | Description | Required |
|----------|-------------|----------|
| `OPENAI_API_KEY` | OpenAI API key | If using OpenAI |
| `ANTHROPIC_API_KEY` | Anthropic API key | If using Anthropic |
| `GEMINI_API_KEY` | Google Gemini API key | If using Gemini |
| `GROQ_API_KEY` | Groq API key | If using Groq |
| `MISTRAL_API_KEY` | Mistral API key | If using Mistral |
| `CUSTOM_PATTERNS_DIRECTORY` | Path to custom patterns | Optional |
| `GIN_MODE` | Gin framework mode (`release` recommended) | Optional |

## API Endpoints

| Endpoint | Description |
|----------|-------------|
| `GET /health` | Health check |
| `GET /ready` | Readiness check |
| `POST /chat` | Chat completion (SSE streaming) |
| `GET /patterns/names` | List available patterns |
| `GET /models/names` | List available models |
| `GET /swagger/*` | API documentation |

## Testing the Deployment

```bash
# Get the app URL
APP_URL=$(az containerapp show --name fabric-ai --resource-group fabric-ai-rg --query "properties.configuration.ingress.fqdn" -o tsv)

# Health check
curl https://$APP_URL/health

# List patterns
curl https://$APP_URL/patterns/names

# Chat completion (example)
curl -X POST https://$APP_URL/chat \
  -H "Content-Type: application/json" \
  -d '{
    "prompts": [{
      "userInput": "Summarize the key points of effective communication",
      "vendor": "openai",
      "model": "gpt-4o-mini",
      "patternName": "summarize"
    }]
  }'
```

## Scaling

The container app automatically scales based on HTTP concurrency (default: 50 concurrent requests per replica).

To modify scaling:

```bash
az containerapp update \
    --name fabric-ai \
    --resource-group fabric-ai-rg \
    --min-replicas 2 \
    --max-replicas 20
```

## Monitoring

View logs:

```bash
az containerapp logs show --name fabric-ai --resource-group fabric-ai-rg --follow
```

## Cost Optimization

- Use `--min-replicas 0` to scale to zero when idle (adds cold start latency)
- Choose appropriate CPU/memory based on workload
- Consider reserved instances for predictable workloads
