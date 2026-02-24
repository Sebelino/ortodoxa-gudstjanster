# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

A Go web service that scrapes and serves church service calendar data from the Finnish Orthodox Congregation in Sweden (ortodox-finsk.se).

## Development Setup

Requires Go 1.25+.

```bash
go mod download
```

## Running Locally

```bash
go run .
```

The server starts on port 8080 (configurable via `PORT` env var).

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

- `main.go` - HTTP server with route handlers
- `scraper.go` - Calendar scraping logic using goquery
- `templates/index.html` - Embedded HTML template for the web UI
- `Dockerfile` - Multi-stage build producing a minimal Alpine-based image

The `ChurchService` struct represents a calendar entry with fields: date, day_of_week, service_name, location, time, occasion, notes.

The scraper parses `section.calendar` containing `div.calendar-item` elements from the source website.
