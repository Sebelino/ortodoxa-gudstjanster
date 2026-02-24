#!/bin/bash
set -e

IMAGE_NAME="church-services"
CONTAINER_NAME="church-services"
CACHE_DIR="${HOME}/.cache/church-services"

mkdir -p "$CACHE_DIR"

echo "Building image..."
docker build -t "$IMAGE_NAME" .

echo "Starting container on port 8080..."
echo "Cache directory: $CACHE_DIR"
if [ -t 0 ]; then
    docker run --rm -it --name "$CONTAINER_NAME" -p 8080:8080 -v "$CACHE_DIR:/app/cache" "$IMAGE_NAME"
else
    docker run --rm --name "$CONTAINER_NAME" -p 8080:8080 -v "$CACHE_DIR:/app/cache" "$IMAGE_NAME"
fi
