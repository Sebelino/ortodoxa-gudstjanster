FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ cmd/
COPY internal/ internal/
RUN CGO_ENABLED=0 GOOS=linux go build -o server ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux go build -o ingest ./cmd/ingest

FROM alpine:latest

RUN apk --no-cache add ca-certificates chromium

WORKDIR /app
COPY --from=builder /app/server .
COPY --from=builder /app/ingest .

# Create cache and store directories
RUN mkdir -p /app/cache /app/disk
ENV CACHE_DIR=/app/cache
ENV STORE_DIR=/app/disk
ENV CHROME_PATH=/usr/bin/chromium-browser

EXPOSE 8080

CMD ["./server"]
