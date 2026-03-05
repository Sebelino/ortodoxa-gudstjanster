#!/usr/bin/env bash
set -euo pipefail

PROJECT=ortodoxa-gudstjanster
REGION=europe-north1
JOB=ortodoxa-gudstjanster-ingest
LIMIT=${1:-5}

gcloud run jobs executions list \
  --job="$JOB" \
  --region="$REGION" \
  --project="$PROJECT" \
  --limit="$LIMIT"
