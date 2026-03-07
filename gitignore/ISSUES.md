# Issues

## Medium

### Non-atomic service replacement creates window of missing data
`internal/firestore/client.go:43-70` — `ReplaceServicesForScraper` deletes all old documents first, then writes new ones. Between delete and write, the web service sees zero services for that source.

### Legacy docs deleted one at a time
`internal/firestore/client.go:85-99` — Legacy cleanup iterates and deletes documents individually instead of using batch deletes like `deleteDocs` does.

### Count queries iterate all documents instead of using aggregation
`internal/firestore/client.go:161-203` — Both `CountServicesForScraper` and `CountFutureServicesForScraper` fetch every matching document just to count them. Should use Firestore's `AggregationQuery` / `Count()`.

### GCS store mutex serializes all operations
`internal/store/gcs.go:15-17` — A `sync.RWMutex` serializes all GCS reads and writes. GCS operations are independent and remote; the mutex provides no consistency benefit while hurting concurrency.

### GCS store ignores parent context
`internal/store/gcs.go:38` — `context.Background()` is used instead of the caller's context. Cache operations continue even after the parent context is cancelled.

### Ryska schedule regex only matches schedules starting with "Januari"
`internal/scraper/ryska.go:64` — The regex `(?i)(Januari\s.+?)(?:bottom of page|KRISTI FÖRKLARINGS|$)` only extracts schedules starting with "Januari". If the schedule starts in a different month, the full HTML-stripped text is returned instead.

### Heliga Anna year-rollover logic
`internal/scraper/heligaanna.go:70-75` — Past months are assigned to next year. January events still on the website in March get year 2027 instead of being recognized as 2026 past events. Also, `time.Now()` called twice (lines 42, 72) could return different values at midnight on Jan 1.

## Low

### Vision API boilerplate duplication
`internal/vision/openai.go` — 6 methods share ~60 lines of identical request/response handling. A `doJSONRequest` helper would reduce this significantly.

### ICS lines not folded per RFC 5545
Long lines in generated ICS output may exceed the 75-octet limit.

### `mapToService` never returns error
`internal/firestore/client.go:283-347` — Returns `(ChurchService, error)` but error is always nil. Missing fields silently produce zero-value structs.

### No graceful shutdown in web server
`cmd/server/main.go` — `http.ListenAndServe` with no signal handling; in-flight requests aborted on SIGTERM.
