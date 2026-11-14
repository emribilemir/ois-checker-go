package main

import (
	"log"
	"net/http"
	"strings"
	"time"

	"notbot/internal/telemetry"
)

// keepAliveEvery Render'ın ücretsiz servisi uyutma süresinden (15 dk) kısa olmalı.
const keepAliveEvery = 10 * time.Minute

// startKeepAlive botun kendi dış adresine düzenli istek atar. Render ücretsiz
// servisi 15 dakika dışarıdan HTTP isteği almazsa uyutur; Telegram ve OİS
// yoklamaları dışarıya giden istek olduğu için uyanık tutmaz. Adres, Render'ın
// verdiği RENDER_EXTERNAL_URL'dir; yerelde boş olduğu için hiçbir şey yapmaz.
func startKeepAlive(baseURL string, every time.Duration, client *http.Client) {
	if baseURL == "" {
		return
	}
	target := strings.TrimRight(baseURL, "/") + "/"
	go func() {
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for range ticker.C {
			pingOnce(client, target)
		}
	}()
}

func pingOnce(client *http.Client, target string) {
	resp, err := client.Get(target)
	if err != nil {
		telemetry.Inc("keepalive_error")
		telemetry.Note("keepalive_error", "ağ hatası")
		log.Printf("uyanık tutma isteği başarısız: %v", err)
		return
	}
	resp.Body.Close()
	telemetry.Inc("keepalive_ok")
}
