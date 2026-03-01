# Scripts

Hjälpskript för att inspektera och hantera data.

## inspect-firestore.go

Verktyg för att visa innehållet i Firestore `services`-collectionen.

### Användning

```bash
# Visa antal gudstjänster per församling
go run scripts/inspect-firestore.go -count

# Visa 10 dokument (standard)
go run scripts/inspect-firestore.go

# Filtrera på församling
go run scripts/inspect-firestore.go -source="St. Georgios Cathedral" -limit=5

# Visa alla dokument
go run scripts/inspect-firestore.go -limit=0

# Använd annat projekt/collection
go run scripts/inspect-firestore.go -project=mitt-projekt -collection=min-collection -count
```

### Flaggor

| Flagga | Beskrivning | Standard |
|--------|-------------|----------|
| `-project` | GCP-projekt-ID | `ortodoxa-gudstjanster` |
| `-collection` | Firestore-collection | `services` |
| `-source` | Filtrera på församling | (alla) |
| `-limit` | Max antal dokument att visa (0 = alla) | `10` |
| `-count` | Visa bara antal per församling | `false` |

### Exempel på utdata

```
# Med -count
Services per source:
--------------------
Sankt Sava                                    338
Kristi Förklarings Ortodoxa Församling        35
Finska Ortodoxa Församlingen                  25
St. Georgios Cathedral                        20
Heliga Anna av Novgorod                       12
--------------------
TOTAL                                         430

# Utan -count
--- Document: 03b76c822751d0932ee705933062a03e ---
{
  "batch_id": "20260301-182219",
  "date": "2026-02-22",
  "day_of_week": "Söndag",
  "language": "Grekiska, svenska",
  "location": "Stockholm, St. Georgios Cathedral, Birger Jarlsgatan 92",
  "occasion": "FÖRLÅTELSENS UPPSTÄNDDELSEDAG",
  "service_name": "Morgongudstjänst",
  "source": "St. Georgios Cathedral",
  "source_url": "https://gomos.se/en/category/schedule/",
  "time": "09:00"
}
```

### Krav

- Go 1.25+
- GCP-autentisering (t.ex. `gcloud auth application-default login`)
