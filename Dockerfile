FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY *.go ./
COPY templates/ templates/
RUN CGO_ENABLED=0 GOOS=linux go build -o server .

FROM alpine:latest

RUN apk --no-cache add ca-certificates tesseract-ocr tesseract-ocr-data-swe tesseract-ocr-data-eng

WORKDIR /app
COPY --from=builder /app/server .

EXPOSE 8080

CMD ["./server"]
