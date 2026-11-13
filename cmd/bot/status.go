package main

import (
	"encoding/json"
	"net/http"
	"os"
	"time"

	"notbot/config"
	"notbot/internal/telemetry"
)

// statusHandler /status adresinde kişisel veri içermeyen çalışma bilgisini
// JSON olarak gösterir: sürüm, son tur, giriş / CAPTCHA / Telegram sayaçları ve
// son olaylar. Not, ders adı, kimlik bilgisi ya da sayfa içeriği yoktur.
func statusHandler(cfg *config.Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		statusMu.RLock()
		checkedAt, lastErr := lastCheckAt, lastCheckError
		statusMu.RUnlock()
		commit := os.Getenv("RENDER_GIT_COMMIT")
		if len(commit) > 7 {
			commit = commit[:7]
		}
		body := map[string]any{
			"commit":           commit,
			"started_at":       processStartedAt.UTC().Format(time.RFC3339),
			"uptime":           time.Since(processStartedAt).Round(time.Second).String(),
			"now":              time.Now().UTC().Format(time.RFC3339),
			"poll_interval":    cfg.PollInterval.String(),
			"settings":         currentSettings(),
			"last_check_at":    formatTime(checkedAt),
			"last_check_error": telemetry.Scrub(lastErr),
			"telemetry":        telemetry.Snapshot(),
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(body)
	})
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
