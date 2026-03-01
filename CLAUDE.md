# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

A Go web service that serves church service calendar data from multiple Orthodox churches in Sweden. Data is scraped periodically by an ingestion job and stored in Firestore. The web service reads from Firestore. Deployed on Google Cloud Run at https://ortodoxagudstjanster.se.

## System Architecture

```
┌──────────────────────────────────────────────────────────┐
│ Ingestion (every 6 hours)                                │
│ Cloud Scheduler → Cloud Run Job → Scrape → Firestore     │
└──────────────────────────────────────────────────────────┘
                                          ↓
┌──────────────────────────────────────────────────────────┐
│ Web Service                                              │
│ ortodoxagudstjanster.se → Cloud Run → Read Firestore     │
└──────────────────────────────────────────────────────────┘
```

- **Web Service** (`cmd/server`): Serves the calendar UI and API, reads from Firestore
- **Ingestion Job** (`cmd/ingest`): Runs all scrapers, stores results in Firestore
- **Cloud Scheduler**: Triggers ingestion every 6 hours (`0 */6 * * *`)

## Development Setup

Requires Go 1.25+.

```bash
go mod download
```

## Running Locally

### Web Server (requires Firestore access)

```bash
export GCP_PROJECT_ID=ortodoxa-gudstjanster
export FIRESTORE_COLLECTION=services
go run ./cmd/server
```

The server starts on port 8080 (configurable via `PORT` env var).

### Ingestion Job (requires GCS and OpenAI API)

```bash
export GCP_PROJECT_ID=ortodoxa-gudstjanster
export FIRESTORE_COLLECTION=services
export GCS_BUCKET=ortodoxa-gudstjanster-ortodoxa-store
export OPENAI_API_KEY=your-api-key
go run ./cmd/ingest
```

### Environment Variables

**Web Server:**
- `PORT` - Server port (default: 8080)
- `GCP_PROJECT_ID` - GCP project ID (required)
- `FIRESTORE_COLLECTION` - Firestore collection name (default: `services`)
- `SMTP_HOST` - SMTP server hostname (e.g., `smtp.gmail.com`)
- `SMTP_PORT` - SMTP server port (e.g., `587`)
- `SMTP_USER` - SMTP username/email
- `SMTP_PASS` - SMTP password (use app password for Gmail)
- `SMTP_TO` - Email address to receive feedback notifications

**Ingestion Job:**
- `GCP_PROJECT_ID` - GCP project ID (required)
- `FIRESTORE_COLLECTION` - Firestore collection name (default: `services`)
- `GCS_BUCKET` - GCS bucket for Vision API results cache (required)
- `OPENAI_API_KEY` - Required for scrapers that use OpenAI Vision API

## Running with Docker

```bash
docker build -t ortodoxa-gudstjanster .

# Run web server
docker run -p 8080:8080 \
  -e GCP_PROJECT_ID=ortodoxa-gudstjanster \
  -e FIRESTORE_COLLECTION=services \
  ortodoxa-gudstjanster ./server

# Run ingestion
docker run \
  -e GCP_PROJECT_ID=ortodoxa-gudstjanster \
  -e FIRESTORE_COLLECTION=services \
  -e GCS_BUCKET=ortodoxa-gudstjanster-ortodoxa-store \
  -e OPENAI_API_KEY=your-api-key \
  ortodoxa-gudstjanster ./ingest
```

## Endpoints

- `GET /` - Web UI showing the calendar
- `GET /services` - JSON API returning all services
- `GET /calendar.ics` - ICS calendar feed (supports `?exclude=` filter)
- `GET /feedback` - Feedback form page
- `POST /feedback` - Submit feedback (sends email via SMTP)
- `GET /health` - Health check endpoint

## Project Structure

```
ortodoxa-gudstjanster/
├── cmd/
│   ├── server/main.go       # Web server entry point (reads from Firestore)
│   └── ingest/main.go       # Ingestion job entry point (scrapes → Firestore)
├── internal/
│   ├── model/service.go     # ChurchService data model
│   ├── firestore/client.go  # Firestore client for storing/retrieving services
│   ├── scraper/
│   │   ├── scraper.go       # Scraper interface, registry, HTTP helpers
│   │   ├── finska.go        # Finska Ortodoxa scraper (HTML parsing)
│   │   ├── gomos.go         # St. Georgios scraper (Vision API OCR)
│   │   ├── heligaanna.go    # Heliga Anna scraper (HTML parsing)
│   │   ├── ryska.go         # Kristi Förklarings scraper (Vision API)
│   │   └── srpska.go        # Sankt Sava scraper (recurring events)
│   ├── cache/cache.go       # HTTP response cache (used by scrapers)
│   ├── store/
│   │   ├── store.go         # Store interface and local file implementation
│   │   └── gcs.go           # Google Cloud Storage implementation
│   ├── vision/openai.go     # OpenAI Vision API client
│   └── web/
│       ├── handler.go       # HTTP handlers (uses ServiceFetcher interface)
│       └── templates/       # Embedded HTML templates
├── scripts/
│   └── inspect-firestore.go # CLI tool to inspect Firestore contents
├── terraform/               # Infrastructure as code (Google Cloud)
│   ├── main.tf              # Provider config
│   ├── variables.tf         # Input variables
│   ├── storage.tf           # GCS bucket, Artifact Registry
│   ├── secrets.tf           # Secret Manager resources
│   ├── cloudrun.tf          # Cloud Run service, domain mapping
│   ├── firestore.tf         # Firestore database and indexes
│   ├── job.tf               # Cloud Run Job for ingestion
│   ├── scheduler.tf         # Cloud Scheduler for periodic ingestion
│   ├── iam.tf               # Service accounts, permissions
│   └── outputs.tf           # Service URL, bucket name, job names
├── run.sh                   # Docker build and run script
├── run_tests.sh             # Test runner for scraper tests
└── Dockerfile               # Multi-stage Alpine build (builds both binaries)
```

## Scripts

### Inspect Firestore

View the contents of the Firestore `services` collection:

```bash
# Show counts per source
go run scripts/inspect-firestore.go -count

# Show 10 documents (default)
go run scripts/inspect-firestore.go

# Filter by source
go run scripts/inspect-firestore.go -source="St. Georgios Cathedral" -limit=5

# Show all documents
go run scripts/inspect-firestore.go -limit=0

# Use different project/collection
go run scripts/inspect-firestore.go -project=my-project -collection=my-collection -count
```

## Adding a New Scraper

1. Create a new file in `internal/scraper/` (e.g., `mychurch.go`)
2. Implement the `Scraper` interface:
   ```go
   type Scraper interface {
       Name() string
       Fetch(ctx context.Context) ([]model.ChurchService, error)
   }
   ```
3. Register it in `cmd/ingest/main.go`:
   ```go
   registry.Register(scraper.NewMyChurchScraper())
   ```

## Data Storage

### Firestore

Services are stored in the `services` collection with:
- Document ID: SHA256 hash of `(source, date, service_name, time)`
- Fields: `source`, `source_url`, `date`, `day_of_week`, `service_name`, `location`, `time`, `occasion`, `notes`, `language`, `batch_id`
- Composite index on `source` + `date` for efficient queries

### Vision API Cache (GCS)

Permanent cache for OpenAI Vision API results:
- Keyed by image checksum (SHA256)
- Stored in GCS bucket `ortodoxa-gudstjanster-ortodoxa-store`
- Prevents re-processing identical images

## Vision API Integration

Some scrapers (Gomos, Ryska) use OpenAI Vision API to extract schedules from images or text:
- `ExtractSchedule`: OCR from schedule images (gpt-4o)
- `ExtractScheduleFromText`: Parse schedule from extracted text (gpt-4o-mini)
- `CompareScheduleImages`: Detect duplicate schedules in different languages (gpt-4o-mini)

The Gomos scraper filters duplicate images (same schedule in Swedish/Greek) before processing.

## Testing

```bash
./run_tests.sh
```

Requires `OPENAI_API_KEY` in `gitignore/apikey.txt`. Tests verify each scraper returns events.

## Deployment (Google Cloud)

Infrastructure is managed with Terraform in `terraform/`.

### Initial Setup

```bash
cd terraform

# First time setup
terraform init
cp terraform.tfvars.example terraform.tfvars
# Edit terraform.tfvars with your project_id

# Deploy infrastructure
terraform apply
```

### Deploy New Version

```bash
# Build and push Docker image
docker build --platform linux/amd64 -t europe-north1-docker.pkg.dev/PROJECT_ID/ortodoxa-gudstjanster/ortodoxa-gudstjanster:latest .
docker push europe-north1-docker.pkg.dev/PROJECT_ID/ortodoxa-gudstjanster/ortodoxa-gudstjanster:latest

# Deploy web service
gcloud run deploy ortodoxa-gudstjanster \
  --image=europe-north1-docker.pkg.dev/PROJECT_ID/ortodoxa-gudstjanster/ortodoxa-gudstjanster:latest \
  --region=europe-north1

# The ingestion job automatically uses the latest image on next run
```

### Manually Trigger Ingestion

```bash
gcloud run jobs execute ortodoxa-gudstjanster-ingest --region=europe-north1
```

### View Ingestion Logs

```bash
gcloud logging read 'resource.type="cloud_run_job" resource.labels.job_name="ortodoxa-gudstjanster-ingest"' \
  --project=ortodoxa-gudstjanster --limit=50 --format="value(textPayload)"
```

### Secrets (Secret Manager)

Secrets must be populated manually in Google Secret Manager:
- `ortodoxa-gudstjanster-openai-api-key` - Used by ingestion job
- `ortodoxa-gudstjanster-smtp-host` - Used by web service
- `ortodoxa-gudstjanster-smtp-port` - Used by web service
- `ortodoxa-gudstjanster-smtp-user` - Used by web service
- `ortodoxa-gudstjanster-smtp-pass` - Used by web service
- `ortodoxa-gudstjanster-smtp-to` - Used by web service

### GCP Resources

| Resource | Name | Purpose |
|----------|------|---------|
| Cloud Run Service | `ortodoxa-gudstjanster` | Web server |
| Cloud Run Job | `ortodoxa-gudstjanster-ingest` | Ingestion (scraping) |
| Cloud Scheduler | `ortodoxa-gudstjanster-ingest-schedule` | Triggers ingestion every 6h |
| Firestore | `(default)` | Service data storage |
| GCS Bucket | `ortodoxa-gudstjanster-ortodoxa-store` | Vision API cache |
| Artifact Registry | `ortodoxa-gudstjanster` | Docker images |

### Service Accounts

| Account | Purpose |
|---------|---------|
| `ortodoxa-gudstjanster-sa` | Web service (Firestore read, SMTP secrets) |
| `ortodoxa-ingest-sa` | Ingestion job (Firestore write, GCS, OpenAI secret) |
| `ortodoxa-scheduler-sa` | Cloud Scheduler (invoke ingestion job) |
