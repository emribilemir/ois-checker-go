# --- Stage 1: Builder ---
FROM golang:1.23-alpine AS builder

WORKDIR /app
COPY go.mod ./
COPY . .
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o notbot ./cmd/bot

# --- Stage 2: Runtime ---
FROM alpine:3.20

RUN apk add --no-cache tesseract-ocr tesseract-ocr-data-eng

# Türkçe karakter yoksa eng yeterli, varsa:
# RUN apk add --no-cache tesseract-ocr-data-tur

WORKDIR /app
COPY --from=builder /app/notbot .

RUN mkdir -p /data

ENV TESSDATA_PREFIX=/usr/share/tessdata

CMD ["./notbot"]
