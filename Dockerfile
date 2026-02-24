FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ cmd/
COPY internal/ internal/
RUN CGO_ENABLED=0 GOOS=linux go build -o server ./cmd/server

FROM alpine:latest

RUN apk --no-cache add ca-certificates tesseract-ocr tesseract-ocr-data-swe tesseract-ocr-data-eng

WORKDIR /app
COPY --from=builder /app/server .

# Create cache directory
RUN mkdir -p /app/cache
ENV CACHE_DIR=/app/cache

EXPOSE 8080

CMD ["./server"]
