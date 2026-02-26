#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

export OPENAI_API_KEY=$(cat gitignore/apikey.txt)

go test -v ./internal/scraper/ -run 'HasFebruary2026Events' -timeout 180s
