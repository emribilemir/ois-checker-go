# Lightweight OIS Checker (Go)

A lightweight Go-based headless scraper to authenticate, fetch grades, and notify students via Telegram when their OIS portal updates. 
Built fully native with Go and executed locally without needing robust web browser runtimes or heavy CGO headers.

## Features
- **Ultra-Lightweight**: Built on Go HTTP clients & raw HTML parsing.
- **Tesseract Native Run**: Uses mathematical morphology (erode/dilate) to automatically preprocess and solve image CAPTCHAs.
- **Interactive Telegram UI**: On-demand grade lookups, metrics overview, and runtime control via Telegram Callback buttons.
- **Docker Ready**: Self-contained configuration supporting persistent data mounting (`/data`).

## Installation (Docker)

1. Rename `.env.example` to `.env`.
2. Fill out your internal `.env` credentials (ensure NO trailing spaces):
   ```
   UNIVERSITY_USER=24000...
   UNIVERSITY_PASS=YourPass
   TELEGRAM_TOKEN=1234:ABC...
   TELEGRAM_CHAT_ID=102...
   ```
3. Run the container:
   ```bash
   docker-compose up --build -d
   ```

## Local Development
If running locally (Windows or Linux), ensure `tesseract` is installed and mapped to your system `$PATH`, then simply build via:
```bash
go run cmd/bot/main.go
```

## Render

Deploy this application as a Docker **Web Service**, not as a Cron Job. The bot
is a continuous process and intentionally does not exit after a single check.

- Set `DERS_SECME_ACTIVE=true` to enable course-selection tracking after every restart.
- Set `POLL_INTERVAL_SECONDS=300` for five-minute OIS checks.
- Use `/` as the health-check path.
- Free Render web services still require an external HTTP request at least once
  every 15 minutes to avoid idle spin-down. A 5-10 minute uptime check is suitable.
- `/data/state.json` is ephemeral on a free instance and can be lost on restart.
