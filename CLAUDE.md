# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

A Go web service that scrapes and serves church service calendar data from multiple Orthodox churches in Sweden.

## Development Setup

Requires Go 1.25+.

```bash
go mod download
```

## Running Locally

```bash
go run ./cmd/server
```

The server starts on port 8080 (configurable via `PORT` env var).

Environment variables:
- `PORT` - Server port (default: 8080)
- `CACHE_DIR` - Directory for HTTP response cache (default: `cache/`)
- `STORE_DIR` - Directory for Vision API results cache (default: `disk/`)
- `OPENAI_API_KEY` - Required for scrapers that use OpenAI Vision API (Gomos, Ryska)
- `SMTP_HOST` - SMTP server hostname (e.g., `smtp.gmail.com`)
- `SMTP_PORT` - SMTP server port (e.g., `587`)
- `SMTP_USER` - SMTP username/email
- `SMTP_PASS` - SMTP password (use app password for Gmail)
- `SMTP_TO` - Email address to receive feedback notifications

## Running with Docker

```bash
docker build -t ortodoxa-gudstjanster .
docker run -p 8080:8080 ortodoxa-gudstjanster
```

## Endpoints

- `GET /` - Web UI showing the calendar
- `GET /services` - JSON API returning all services
- `GET /calendar.ics` - ICS calendar feed (supports `?exclude=` filter)
- `GET /feedback` - Feedback form page
- `POST /feedback` - Submit feedback (sends email via SMTP)
- `GET /health` - Health check endpoint

## Architecture

```
ortodoxa-gudstjanster/
├── cmd/server/main.go       # Entry point, wires up dependencies
├── internal/
│   ├── model/service.go     # ChurchService data model
│   ├── scraper/
│   │   ├── scraper.go       # Scraper interface, registry, HTTP helpers
│   │   ├── finska.go        # Finska Ortodoxa scraper (HTML parsing)
│   │   ├── gomos.go         # St. Georgios scraper (Vision API OCR)
│   │   ├── heligaanna.go    # Heliga Anna scraper (HTML parsing)
│   │   ├── ryska.go         # Kristi Förklarings scraper (Vision API)
│   │   └── srpska.go        # Sankt Sava scraper (recurring events)
│   ├── cache/cache.go       # HTTP response cache (30-min TTL)
│   ├── store/store.go       # Persistent store for Vision API results
│   ├── vision/openai.go     # OpenAI Vision API client
│   └── web/
│       ├── handler.go       # HTTP handlers
│       └── templates/       # Embedded HTML templates
├── run.sh                   # Docker build and run script
├── run_tests.sh             # Test runner for scraper tests
└── Dockerfile               # Multi-stage Alpine build
```

### Adding a New Scraper

1. Create a new file in `internal/scraper/` (e.g., `mychurch.go`)
2. Implement the `Scraper` interface:
   ```go
   type Scraper interface {
       Name() string
       Fetch(ctx context.Context) ([]model.ChurchService, error)
   }
   ```
3. Register it in `cmd/server/main.go`:
   ```go
   registry.Register(scraper.NewMyChurchScraper())
   ```

### Caching

Two caching layers:
- **HTTP Cache** (`CACHE_DIR`): 30-minute TTL for scraped HTML responses
- **Store** (`STORE_DIR`): Permanent cache for Vision API results, keyed by image checksum (SHA256)

### Vision API Integration

Some scrapers (Gomos, Ryska) use OpenAI Vision API to extract schedules from images or text:
- `ExtractSchedule`: OCR from schedule images (gpt-4o)
- `ExtractScheduleFromText`: Parse schedule from extracted text (gpt-4o-mini)
- `CompareScheduleImages`: Detect duplicate schedules in different languages (gpt-4o-mini)

The Gomos scraper filters duplicate images (same schedule in Swedish/Greek) before processing.

## Testing

```bash
./run_tests.sh
```

Requires `OPENAI_API_KEY` in `gitignore/apikey.txt`. Tests verify each scraper returns February 2026 events.
