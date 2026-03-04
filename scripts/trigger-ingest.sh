#!/usr/bin/env bash
set -euo pipefail

PROJECT=ortodoxa-gudstjanster
REGION=europe-north1
JOB=ortodoxa-gudstjanster-ingest

echo "Triggering ingestion job..."
gcloud run jobs execute "$JOB" --region="$REGION" --project="$PROJECT" --wait
