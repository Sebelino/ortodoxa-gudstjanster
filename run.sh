#!/bin/bash
set -e

IMAGE_NAME="church-services"
CONTAINER_NAME="church-services"

echo "Building image..."
docker build -t "$IMAGE_NAME" .

echo "Starting container on port 8080..."
if [ -t 0 ]; then
    docker run --rm -it --name "$CONTAINER_NAME" -p 8080:8080 "$IMAGE_NAME"
else
    docker run --rm --name "$CONTAINER_NAME" -p 8080:8080 "$IMAGE_NAME"
fi
