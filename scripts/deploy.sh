#!/usr/bin/env bash
set -euo pipefail

PROJECT=ortodoxa-gudstjanster
REGION=europe-north1
IMAGE=europe-north1-docker.pkg.dev/$PROJECT/ortodoxa-gudstjanster/ortodoxa-gudstjanster:latest

echo "Building Docker image..."
docker build --platform linux/amd64 -t "$IMAGE" .

echo "Pushing image..."
docker push "$IMAGE"

echo "Deploying web service..."
gcloud run deploy ortodoxa-gudstjanster \
  --image="$IMAGE" \
  --region="$REGION" \
  --project="$PROJECT"

echo "Done. The ingestion job will use the new image on its next run."
