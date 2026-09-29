# Lightweight OIS Checker (Go)

A lightweight Go-based headless scraper to authenticate, fetch grades, and notify students via Telegram when their OIS portal updates. 
Built fully native with Go and executed locally without needing robust web browser runtimes or heavy CGO headers.

## Features
- **Ultra-Lightweight**: Built on Go HTTP clients & raw HTML parsing.
- **Tesseract OCR Ensemble**: Compares multiple live-site-tuned threshold variants and page-segmentation modes, then selects the strongest CAPTCHA reading by confidence and agreement.
- **Interactive Telegram UI**: On-demand grade lookups, metrics overview, and runtime control via Telegram Callback buttons.
- **Weekly Timetable**: Fetches the current OIS course schedule on demand and displays each day in a mobile-friendly Telegram message with instructor names and clear online-class markers.
- **Optional Class Reminders**: A separate Telegram toggle sends one alert 15 minutes before each class. The choice and sent-alert history are persisted next to `STATE_FILE`.
- **Reliable Course-Selection Watch**: Distinguishes the permanent menu link, a genuinely open course-selection page, a closed-period notice, and an expired/conflicting OIS session.
- **Selected-Course Change Alerts**: Includes the currently selected courses in the opening alert and reports later additions/removals without claiming whether a human or OIS made the change.
- **Elective Pool Availability Alerts**: Reads the observed Bölüm Seçmeli pool and any pool links visible on the course-selection page. Sends an alert for courses with open seats on the first scan and when a new course appears or a full course reopens. The pool page does not expose online status, so alerts explicitly say that teaching mode is unverified. The bot only sends GET requests to pool pages and never enrolls in or drops courses.
- **Self-Recovering Deploy Health**: Network calls are bounded and the HTTP health endpoint reports a stalled polling loop instead of claiming that a frozen process is healthy.
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
- Selected-course snapshots are stored next to `STATE_FILE` with a `.derssecme` suffix. Additions and removals remain detectable across restarts when that storage is persistent.
- Elective pool snapshots are stored next to `STATE_FILE` with an `.electives` suffix. `ELECTIVE_POOL_PATHS` defaults to the observed Bölüm Seçmeli pool path; add other comma-separated pool paths when available. The bot also discovers pool paths from visible "Ders Seç" buttons.
- Set `POLL_INTERVAL_SECONDS=300` for five-minute OIS checks.
- Use `/` as the health-check path.
- The `/` health check returns `503` when no polling cycle has completed for ten minutes, allowing Render to restart a stuck instance.
- Telegram's **Taramayı Durdur** button pauses grade checks only; course-selection tracking remains active when enabled separately.
- Telegram'da **📅 Ders Programım** düğmesine basarak veya `/program` yazarak güncel programı OİS'ten alabilirsiniz. Günler ve saatler telefon ekranında okunacak şekilde listelenir; online dersler 🌐 ile gösterilir.
- **🔔 Ders Uyarılarını Aç/Kapat** düğmesi ders başlamadan 15 dakika önce gönderilen uyarıları yönetir. Uyarılar ilk kurulumda kapalıdır ve not taraması düğmesinden bağımsızdır. Açıkken program her 30 dakikada bir yenilenir ve uyarı saati dakikada bir kontrol edilir (Europe/Istanbul).
- Uyarı tercihi ve aynı ders için gönderilmiş uyarılar `STATE_FILE.schedule` dosyasında tutulur. Kalıcı depolama bağlı değilse yeniden başlatma sonrasında tercih sıfırlanabilir.
- Free Render web services still require an external HTTP request at least once
  every 15 minutes to avoid idle spin-down. A 5-10 minute uptime check is suitable.
- Render can restart a Free instance at any time. The app starts course-selection tracking from `DERS_SECME_ACTIVE` again after each restart.
- `/data/state.json` is ephemeral on a free instance and can be lost on restart.
