# Ortodoxa Gudstjänster

En webbtjänst som samlar gudstjänstscheman från östortodoxa församlingar i Sverige i en gemensam kalender.

**https://ortodoxagudstjanster.se**

## Funktioner

- Samlar scheman från flera östortodoxa församlingar i Stockholm
- Webbgränssnitt för att bläddra bland kommande gudstjänster
- Filtrera efter församling
- ICS-kalenderflöde för prenumeration i Google Kalender, Apple Kalender m.fl.
- JSON-API för programmatisk åtkomst

## Arkitektur

```
┌──────────────────────────────────────────────────────────┐
│ Insamling (var 6:e timme)                                │
│ Cloud Scheduler → Cloud Run Job → Scraping → Firestore   │
└──────────────────────────────────────────────────────────┘
                                          ↓
┌──────────────────────────────────────────────────────────┐
│ Webbserver                                               │
│ ortodoxagudstjanster.se → Cloud Run → Läs Firestore      │
└──────────────────────────────────────────────────────────┘
```

Data samlas in av ett schemalagt jobb som kör alla scrapers och sparar resultaten i Firestore. Webbservern läser sedan från Firestore för snabb responstid.

## Församlingar

- Finska Ortodoxa Församlingen (Helige Nikolai)
- St. Georgios Cathedral (Grekisk-ortodoxa)
- Heliga Anna av Novgorod
- Kristi Förklarings Ortodoxa Församling (Rysk-ortodoxa)
- Sankt Sava (Serbisk-ortodoxa)

## Användning

### Webbgränssnitt

Besök [ortodoxagudstjanster.se](https://ortodoxagudstjanster.se) för att bläddra bland kommande gudstjänster. Du kan filtrera efter församling och expandera varje gudstjänst för detaljer.

### Kalenderprenumeration

Prenumerera på ICS-flödet för att få gudstjänsterna i din kalenderapp:

```
https://ortodoxagudstjanster.se/calendar.ics
```

Du kan exkludera specifika församlingar med parametern `exclude`:

```
https://ortodoxagudstjanster.se/calendar.ics?exclude=St.%20Georgios%20Cathedral
```

#### Lägga till i Google Kalender

1. Öppna [Google Calendar](https://calendar.google.com) i webbläsaren
2. Klicka på **+** bredvid "Andra kalendrar" i vänstermenyn
3. Välj **Från webbadress** / **From URL**
4. Klistra in URL:en: `https://ortodoxagudstjanster.se/calendar.ics`
5. Klicka på **Lägg till kalender**

Kalendern synkroniseras automatiskt med nya gudstjänster.

### JSON-API

```
GET /services
```

Returnerar alla kommande gudstjänster som JSON.

## Utveckling

Kräver Go 1.25+.

```bash
# Installera beroenden
go mod download

# Kör webbserver lokalt (kräver Firestore-åtkomst)
export GCP_PROJECT_ID=ortodoxa-gudstjanster
export FIRESTORE_COLLECTION=services
go run ./cmd/server

# Kör insamlingsjobb lokalt (kräver GCS och OpenAI API)
export GCP_PROJECT_ID=ortodoxa-gudstjanster
export FIRESTORE_COLLECTION=services
export GCS_BUCKET=ortodoxa-gudstjanster-ortodoxa-store
export OPENAI_API_KEY=din-nyckel
go run ./cmd/ingest
```

Servern startar på http://localhost:8080.

### Miljövariabler

#### Webbserver

| Variabel | Beskrivning | Standard |
|----------|-------------|----------|
| `PORT` | Serverport | `8080` |
| `GCP_PROJECT_ID` | GCP-projekt-ID | (krävs) |
| `FIRESTORE_COLLECTION` | Firestore-collection | `services` |
| `SMTP_HOST` | SMTP-server för feedback-mejl | - |
| `SMTP_PORT` | SMTP-port | - |
| `SMTP_USER` | SMTP-användarnamn | - |
| `SMTP_PASS` | SMTP-lösenord | - |
| `SMTP_TO` | Mottagare för feedback | - |

#### Insamlingsjobb

| Variabel | Beskrivning | Standard |
|----------|-------------|----------|
| `GCP_PROJECT_ID` | GCP-projekt-ID | (krävs) |
| `FIRESTORE_COLLECTION` | Firestore-collection | `services` |
| `GCS_BUCKET` | GCS-bucket för Vision API-cache | (krävs) |
| `OPENAI_API_KEY` | Krävs för OCR-baserade scrapers | (krävs) |

### Köra med Docker

```bash
docker build -t ortodoxa-gudstjanster .

# Kör webbserver
docker run -p 8080:8080 \
  -e GCP_PROJECT_ID=ortodoxa-gudstjanster \
  -e FIRESTORE_COLLECTION=services \
  ortodoxa-gudstjanster ./server

# Kör insamlingsjobb
docker run \
  -e GCP_PROJECT_ID=ortodoxa-gudstjanster \
  -e FIRESTORE_COLLECTION=services \
  -e GCS_BUCKET=ortodoxa-gudstjanster-ortodoxa-store \
  -e OPENAI_API_KEY=din-nyckel \
  ortodoxa-gudstjanster ./ingest
```

### Tester

```bash
OPENAI_API_KEY=din-nyckel go test ./...
```

### Hjälpskript

Se [scripts/README.md](scripts/README.md) för verktyg som inspekterar Firestore-data.

## Driftsättning

Tjänsten körs på Google Cloud Run. Infrastruktur hanteras med Terraform i `terraform/`.

```bash
cd terraform
terraform init
terraform apply
```

Se [terraform/README.md](terraform/README.md) för detaljerade instruktioner.

## Mer information

Se [CLAUDE.md](CLAUDE.md) för fullständig teknisk dokumentation.
