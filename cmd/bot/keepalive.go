package main

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"notbot/internal/telemetry"
)

// keepAliveEvery Render'ın ücretsiz servisi uyutma süresinden (15 dk) çok kısa
// olmalı: bir istek aksasa bile sıradaki iki istek 15 dakikayı doldurmadan gider.
const keepAliveEvery = 5 * time.Minute

// keepAliveRetries ve keepAliveRetryDelay başarısız isteğin hemen tekrarıdır.
var (
	keepAliveRetries    = 2
	keepAliveRetryDelay = 15 * time.Second
)

// startKeepAlive botun kendi dış adresine düzenli istek atar. Render ücretsiz
// servisi 15 dakika dışarıdan HTTP isteği almazsa uyutur; Telegram ve OİS
// yoklamaları dışarıya giden istek olduğu için uyanık tutmaz. Adres, Render'ın
// verdiği RENDER_EXTERNAL_URL'dir; yerelde boş olduğu için hiçbir şey yapmaz.
//
// wanted her turda uyanık kalınıp kalınmayacağını söyler; false ise istek
// atılmaz ve Render servisi uyutur. onSleep, uyanık dönemden uykuya geçerken
// bir kez çağrılır. Dönen fonksiyon döngüyü durdurur (testler için).
func startKeepAlive(baseURL string, every time.Duration, client *http.Client, wanted func(time.Time) (bool, string), onSleep func(reason string)) (stop func()) {
	if baseURL == "" {
		return func() {}
	}
	target := strings.TrimRight(baseURL, "/") + "/"
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		// Bot dönem dışında uyandırıldıysa (ör. saatlik GitHub işi) uyku bildirimi tekrar gitmesin.
		wasAwake, _ := wanted(time.Now())
		for {
			select {
			case <-done:
				return
			case now := <-ticker.C:
				awake, reason := wanted(now)
				telemetry.Set("awake", reason)
				if awake {
					pingOnce(client, target)
				} else if wasAwake && onSleep != nil {
					onSleep(reason)
				}
				wasAwake = awake
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(done) }) }
}

func pingOnce(client *http.Client, target string) {
	for attempt := 0; ; attempt++ {
		resp, err := client.Get(target)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				telemetry.Inc("keepalive_ok")
				return
			}
			err = fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		telemetry.Inc("keepalive_error")
		telemetry.Note("keepalive_error", fmt.Sprintf("deneme %d: %v", attempt+1, err))
		log.Printf("uyanık tutma isteği başarısız (deneme %d): %v", attempt+1, err)
		if attempt >= keepAliveRetries {
			return
		}
		time.Sleep(keepAliveRetryDelay)
	}
}
