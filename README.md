# Ortodoxa Gudstjänster

En webbtjänst som samlar gudstjänstscheman från östortodoxa församlingar i Sverige i en gemensam kalender.

**https://ortodoxagudstjanster.se**

## Funktioner

- Samlar scheman från flera östortodoxa församlingar i Stockholm
- Webbgränssnitt för att bläddra bland kommande gudstjänster
- Filtrera efter församling
- ICS-kalenderflöde för prenumeration i Google Kalender, Apple Kalender m.fl.
- JSON-API för programmatisk åtkomst

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

# Kör lokalt
go run ./cmd/server
```

Servern startar på http://localhost:8080.

### Miljövariabler

| Variabel | Beskrivning | Standard |
|----------|-------------|----------|
| `PORT` | Serverport | `8080` |
| `CACHE_DIR` | Katalog för HTTP-cache | `cache/` |
| `STORE_DIR` | Lokal cache för Vision API-resultat | `disk/` |
| `GCS_BUCKET` | GCS-bucket för Vision API-resultat (produktion) | - |
| `OPENAI_API_KEY` | Krävs för OCR-baserade scrapers | - |
| `SMTP_HOST` | SMTP-server för feedback-mejl | - |
| `SMTP_PORT` | SMTP-port | - |
| `SMTP_USER` | SMTP-användarnamn | - |
| `SMTP_PASS` | SMTP-lösenord | - |
| `SMTP_TO` | Mottagare för feedback | - |

### Köra med Docker

```bash
docker build -t ortodoxa-gudstjanster .
docker run -p 8080:8080 -e OPENAI_API_KEY=din-nyckel ortodoxa-gudstjanster
```

### Tester

```bash
OPENAI_API_KEY=din-nyckel go test ./...
```

## Driftsättning

Tjänsten körs på Google Cloud Run. Infrastruktur hanteras med Terraform i `terraform/`.

```bash
cd terraform
terraform init
terraform apply
```

Se [CLAUDE.md](CLAUDE.md) för detaljerade instruktioner.
