#!/bin/bash
set -e

IMAGE_NAME="church-services"
CONTAINER_NAME="church-services"
CACHE_DIR="${HOME}/.cache/church-services"
STORE_DIR="$(pwd)/disk"

mkdir -p "$CACHE_DIR"
mkdir -p "$STORE_DIR"

# Load API key from gitignore/apikey.txt if OPENAI_API_KEY not set
if [ -z "$OPENAI_API_KEY" ] && [ -f "gitignore/apikey.txt" ]; then
    OPENAI_API_KEY=$(cat gitignore/apikey.txt | tr -d '\n')
fi

echo "Building image..."
docker build -t "$IMAGE_NAME" .

echo "Starting container on port 8080..."
echo "Cache directory: $CACHE_DIR"
echo "Store directory: $STORE_DIR"
if [ -t 0 ]; then
    docker run --rm -it --name "$CONTAINER_NAME" -p 8080:8080 \
        -v "$CACHE_DIR:/app/cache" \
        -v "$STORE_DIR:/app/disk" \
        -e "OPENAI_API_KEY=$OPENAI_API_KEY" \
        "$IMAGE_NAME"
else
    docker run --rm --name "$CONTAINER_NAME" -p 8080:8080 \
        -v "$CACHE_DIR:/app/cache" \
        -v "$STORE_DIR:/app/disk" \
        -e "OPENAI_API_KEY=$OPENAI_API_KEY" \
        "$IMAGE_NAME"
fi
