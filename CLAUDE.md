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

Cache is stored in a temp directory by default (configurable via `CACHE_DIR` env var).

## Running with Docker

```bash
docker build -t church-services .
docker run -p 8080:8080 church-services
```

## Endpoints

- `GET /` - Web UI showing the calendar
- `GET /services` - JSON API returning all services
- `GET /health` - Health check endpoint

## Architecture

```
church-services/
├── cmd/server/main.go       # Entry point, wires up dependencies
├── internal/
│   ├── model/service.go     # ChurchService data model
│   ├── scraper/
│   │   ├── scraper.go       # Scraper interface and registry
│   │   ├── finska.go        # Finska Ortodoxa scraper (HTML)
│   │   └── gomos.go         # St. Georgios scraper (OCR)
│   ├── cache/cache.go       # Disk-based caching layer
│   └── web/
│       ├── handler.go       # HTTP handlers
│       └── templates/       # Embedded HTML templates
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

Results are cached to disk with a 30-minute TTL. Cache files are stored as JSON in the `CACHE_DIR` directory.
