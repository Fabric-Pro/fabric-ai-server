#!/bin/bash
# =============================================================================
# Azure Container Apps Deployment Script
# =============================================================================
# This script deploys Fabric AI to Azure Container Apps
#
# Prerequisites:
# - Azure CLI installed and logged in (az login)
# - Docker installed for building images
# =============================================================================

set -e

# Configuration - Update these values
RESOURCE_GROUP="${RESOURCE_GROUP:-fabric-ai-rg}"
LOCATION="${LOCATION:-eastus}"
ACR_NAME="${ACR_NAME:-fabricairegistry}"
CONTAINER_APP_ENV="${CONTAINER_APP_ENV:-fabric-ai-env}"
CONTAINER_APP_NAME="${CONTAINER_APP_NAME:-fabric-ai}"
KEY_VAULT_NAME="${KEY_VAULT_NAME:-fabric-ai-kv}"
IMAGE_TAG="${IMAGE_TAG:-latest}"

echo "=== Fabric AI Azure Deployment ==="
echo "Resource Group: $RESOURCE_GROUP"
echo "Location: $LOCATION"
echo "Container Registry: $ACR_NAME"
echo "Container App Environment: $CONTAINER_APP_ENV"
echo "Container App Name: $CONTAINER_APP_NAME"
echo ""

# Create resource group
echo "Creating resource group..."
az group create --name "$RESOURCE_GROUP" --location "$LOCATION" --output none

# Create Azure Container Registry
echo "Creating Azure Container Registry..."
az acr create \
    --resource-group "$RESOURCE_GROUP" \
    --name "$ACR_NAME" \
    --sku Basic \
    --admin-enabled true \
    --output none

# Get ACR credentials
ACR_LOGIN_SERVER=$(az acr show --name "$ACR_NAME" --query loginServer --output tsv)
ACR_USERNAME=$(az acr credential show --name "$ACR_NAME" --query username --output tsv)
ACR_PASSWORD=$(az acr credential show --name "$ACR_NAME" --query "passwords[0].value" --output tsv)

# Build and push Docker image
echo "Building and pushing Docker image..."
cd "$(dirname "$0")/../.."  # Navigate to project root

# Build the image
docker build -t "$ACR_LOGIN_SERVER/fabric:$IMAGE_TAG" -f scripts/docker/Dockerfile .

# Login to ACR
echo "$ACR_PASSWORD" | docker login "$ACR_LOGIN_SERVER" -u "$ACR_USERNAME" --password-stdin

# Push the image
docker push "$ACR_LOGIN_SERVER/fabric:$IMAGE_TAG"

# Create Key Vault for secrets
echo "Creating Key Vault..."
az keyvault create \
    --name "$KEY_VAULT_NAME" \
    --resource-group "$RESOURCE_GROUP" \
    --location "$LOCATION" \
    --enable-rbac-authorization true \
    --output none || echo "Key Vault may already exist, continuing..."

# Create Container Apps Environment
echo "Creating Container Apps Environment..."
az containerapp env create \
    --name "$CONTAINER_APP_ENV" \
    --resource-group "$RESOURCE_GROUP" \
    --location "$LOCATION" \
    --output none

# Create Container App
echo "Creating Container App..."
az containerapp create \
    --name "$CONTAINER_APP_NAME" \
    --resource-group "$RESOURCE_GROUP" \
    --environment "$CONTAINER_APP_ENV" \
    --image "$ACR_LOGIN_SERVER/fabric:$IMAGE_TAG" \
    --registry-server "$ACR_LOGIN_SERVER" \
    --registry-username "$ACR_USERNAME" \
    --registry-password "$ACR_PASSWORD" \
    --target-port 8080 \
    --ingress external \
    --min-replicas 1 \
    --max-replicas 10 \
    --cpu 0.5 \
    --memory 1Gi \
    --env-vars "GIN_MODE=release" \
    --output none

# Get the app URL
APP_URL=$(az containerapp show \
    --name "$CONTAINER_APP_NAME" \
    --resource-group "$RESOURCE_GROUP" \
    --query "properties.configuration.ingress.fqdn" \
    --output tsv)

echo ""
echo "=== Deployment Complete ==="
echo "Container App URL: https://$APP_URL"
echo ""
echo "Next steps:"
echo "1. Add API keys to Key Vault:"
echo "   az keyvault secret set --vault-name $KEY_VAULT_NAME --name OPENAI-API-KEY --value 'your-key'"
echo "   az keyvault secret set --vault-name $KEY_VAULT_NAME --name ANTHROPIC-API-KEY --value 'your-key'"
echo ""
echo "2. Configure Container App to use Key Vault secrets:"
echo "   az containerapp secret set --name $CONTAINER_APP_NAME --resource-group $RESOURCE_GROUP \\"
echo "       --secrets openai-key=keyvaultref:https://$KEY_VAULT_NAME.vault.azure.net/secrets/OPENAI-API-KEY,identityref:system"
echo ""
echo "3. Update environment variables to reference secrets:"
echo "   az containerapp update --name $CONTAINER_APP_NAME --resource-group $RESOURCE_GROUP \\"
echo "       --set-env-vars OPENAI_API_KEY=secretref:openai-key"
echo ""
echo "4. Test the deployment:"
echo "   curl https://$APP_URL/health"
echo "   curl https://$APP_URL/patterns/names"
