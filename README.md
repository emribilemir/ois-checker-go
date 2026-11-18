# Lightweight OIS Checker (Go)

A lightweight Go-based headless scraper to authenticate, fetch grades, and notify students via Telegram when their OIS portal updates. 
Built fully native with Go and executed locally without needing robust web browser runtimes or heavy CGO headers.

## Features
- **Ultra-Lightweight**: Built on Go HTTP clients & raw HTML parsing.
- **Tesseract OCR Ensemble**: Compares multiple live-site-tuned threshold variants and page-segmentation modes, then selects the strongest CAPTCHA reading by confidence and agreement. The ensemble stops early once the leading reading is confirmed by three passes with at least one confident pass; otherwise all ten passes run.
- **Session Reuse**: Each cycle first checks with one GET whether the OIS session is still open and skips the CAPTCHA login when it is. If OIS shows the login page during the cycle (for example after a browser login elsewhere), the bot logs in once and repeats the cycle. Each cycle logs its duration as `[tur] ... sürdü`.
- **Interactive Telegram UI**: A 2x2 menu (Notlar, Program, Sınavlar, Ayarlar) plus `/menu`, `/notlar`, `/program`, `/sinav`, `/ayarlar` in Telegram's command menu. Button presses update the same message instead of adding new ones.
- **Rich Messages**: Messages are sent as Telegram rich messages (headings, tables, collapsible days, checklists). If Telegram rejects a rich message, the same content is sent as a plain HTML card. All message layouts live in `internal/view`. Startup messages are silent; grade, course-selection, elective-pool and class alerts ring.
- **Weekly Timetable**: Fetches the current OIS course schedule on demand and displays each day in a mobile-friendly Telegram message with instructor names and clear online-class markers.
- **Exam Calendar**: On demand (📝 Sınavlar button or `/sinav`), reads `/ogrenciler/belge/sinavprogrami` (falling back to exam links visible in the OIS menu), reads it by its column headers and lists upcoming exams by day with time, type and room. If the page cannot be read, the reply lists the tried paths, menu link names and table headers (never row contents) so the parser can be adjusted.
- **No Missed Grades Across Restarts**: Render's free disk is wiped on every restart, so the grade state file is lost. The pinned settings message also keeps short 6-character marks of the published grades (hashes of course code, component and score; no names or scores) and the bot's last-alive time. After a restart the bot compares the first grade read with these marks and sends a normal "Yeni not" alert for anything published while it was down. The bot no longer posts a startup message or a startup grade list. `/status` records how long the previous run was down, and panics in the polling, Telegram and reminder loops are recovered and reported instead of killing the process.
- **Settings Survive Deploys**: Grade checks, course-selection tracking and class reminders are saved in a small message the bot pins in the Telegram chat ("⚙️ Bot ayarları"). On startup the bot reads it with `getChat`; on every toggle it edits the same message. If the pinned message is missing or replaced by another pin, the environment defaults apply until the next toggle.
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

- `DERS_SECME_ACTIVE` is only the default before the first toggle; afterwards the pinned settings message wins.
- Selected-course snapshots are stored next to `STATE_FILE` with a `.derssecme` suffix. Additions and removals remain detectable across restarts when that storage is persistent.
- Elective pool snapshots are stored next to `STATE_FILE` with an `.electives` suffix. `ELECTIVE_POOL_PATHS` defaults to the observed Bölüm Seçmeli pool path; add other comma-separated pool paths when available. The bot also discovers pool paths from visible "Ders Seç" buttons.
- Set `POLL_INTERVAL_SECONDS=300` for five-minute OIS checks.
- Use `/` as the health-check path.
- `/status` returns JSON with the deployed commit, uptime, settings, last check, login / session / OCR / Telegram counters and the last 60 events. It holds no grades, course names or credentials, and numbers of six or more digits are masked, so it can stay public.
- The `/` health check returns `503` when no polling cycle has completed for ten minutes (or two poll intervals, whichever is longer), allowing Render to restart a stuck instance.
- **⚙️ Ayarlar → Not taramasını durdur** pauses grade checks only; course-selection tracking remains active when enabled separately. When grade checks are paused and course-selection tracking is off, the bot skips the OIS login entirely.
- **📅 Program** düğmesi veya `/program` güncel programı OİS'ten alır. Her gün açılır bir bölümdür; bugün (ya da sıradaki dersli gün) açık gelir, online dersler 🌐 ile gösterilir.
- **📝 Sınavlar** düğmesi veya `/sinav` komutu OİS'teki sınav takvimini getirir: bugünden sonraki sınavlar gün başlıklı bir tabloda saat, sınav türü ve derslikle listelenir; geçmiş sınavlar ve hoca / açıklama bilgisi açılır bölümdedir. Sayfa okunamazsa bot nereye baktığını ve ne gördüğünü yazar.
- **⚙️ Ayarlar → Ders uyarılarını aç / kapat** düğmesi ders başlamadan 15 dakika önce gönderilen uyarıları yönetir. Uyarılar ilk kurulumda kapalıdır ve not taraması düğmesinden bağımsızdır. Açıkken program her 30 dakikada bir yenilenir ve uyarı saati dakikada bir kontrol edilir (Europe/Istanbul). OİS'ten program alınamazsa uyarılar son alınan programla sürer ve yeni deneme 5 dakika sonra yapılır.
- Uyarı tercihi sabit ayar mesajında da tutulur, deploy sonrası korunur. Aynı ders için gönderilmiş uyarılar `STATE_FILE.schedule` dosyasındadır.
- Free Render web services spin down after 15 minutes without inbound HTTP traffic; Telegram and OIS polling are outbound and do not count. The bot keeps itself awake only when it is useful: inside the exam, grade and course-registration windows in `internal/awake/windows.txt` (taken from the Atlas academic calendar), while course selection is seen open, or while class reminders are on. Then it requests its own `RENDER_EXTERNAL_URL` every 5 minutes, retrying a failed request twice; otherwise it lets Render put it to sleep and says so once in Telegram. A sleeping bot cannot wake itself, so `.github/workflows/wake.yml` checks the same file every 5 minutes (GitHub thins scheduled runs heavily: the hourly schedule ran 14 times in 4 days) and wakes the bot when a window is active. Render also stops free instances on its own; a stopped instance stays down until the next request, so an external uptime monitor pinging `/` every 5 minutes is the most reliable addition. Update `windows.txt` when the next academic calendar is published.
- **⚙️ Ayarlar → 😴 Uyanık kalma** overrides the calendar from Telegram: +1 week / +2 weeks / +1 month from the current end, "Bugün bitsin" to end today, or `/uyanik 20.12` for an exact date (`/uyanik takvim` returns to the calendar). The choice is saved in the pinned settings message. A calendar window that started before the chosen end is cut by it; a window that starts later wakes the bot as usual.
- Telegram updates that piled up while the bot was down (sleep or deploy) are skipped at startup, so old button presses are not applied hours later.
- Render can restart a Free instance at any time. Toggles are restored from the pinned settings message after each restart.
- `/data/state.json` is ephemeral on a free instance and can be lost on restart.
