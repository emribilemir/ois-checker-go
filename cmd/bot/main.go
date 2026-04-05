package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"notbot/config"
	"notbot/internal/auth"
	"notbot/internal/diff"
	"notbot/internal/notify"
	"notbot/internal/scraper"
	"notbot/internal/session"
)

var (
	cacheMu       sync.RWMutex
	cachedCourses []scraper.Course
	isPaused      bool
)

func main() {
	cfg := config.Load()
	client := session.New(cfg.UserAgent)

	log.Printf("Bot başladı. Kontrol aralığı: %s", cfg.PollInterval)

	msgStr := fmt.Sprintf("🤖 OIS Checker Bot Başladı!\n⏱️ Kontrol Aralığı: %.0f dakika\nNotlarını kontrol etmeye başlıyorum...", cfg.PollInterval.Minutes())
	if err := notify.SendMenu(cfg.TelegramToken, cfg.TelegramChatID, msgStr); err != nil {
		log.Printf("Başlangıç Telegram mesajı hatası: %v", err)
	}

	// Telegram Callback dinleyicisini arkaplanda başlat
	go notify.StartPoller(cfg.TelegramToken, func(cmd, chatID, cbqID string) {
		// Sadece yapılandırılmış yöneticiye cevap ver
		if chatID != cfg.TelegramChatID {
			return
		}

		switch cmd {
		case "/start":
			notify.SendMenu(cfg.TelegramToken, chatID, fmt.Sprintf("👋 Hoşgeldin! Aşağıdaki menüden istediklerine direkt ulaşabilirsin:\n_(Şu anki rutin kontrol aralığı: %.0f dakikada bir)_", cfg.PollInterval.Minutes()))
		
		case "cmd_pause":
			cacheMu.Lock()
			isPaused = true
			cacheMu.Unlock()
			if cbqID != "" {
				notify.AnswerCallback(cfg.TelegramToken, cbqID, "Tarama duraklatıldı.")
			}
			notify.SendTelegram(cfg.TelegramToken, chatID, "⏸ *Bot Duraklatıldı.*\nArkaplanda not kontrolü yapılmayacak. Yeniden başlatmak için menüden Devam Et tuşuna basabilirsin.")
		
		case "cmd_resume":
			cacheMu.Lock()
			isPaused = false
			cacheMu.Unlock()
			if cbqID != "" {
				notify.AnswerCallback(cfg.TelegramToken, cbqID, "Tarama sürdürülüyor.")
			}
			notify.SendTelegram(cfg.TelegramToken, chatID, "▶️ *Bot Devam Ediyor.*\nArkaplanda OIS kontrol döngüsü aktif edildi.")
		
		case "cmd_restart":
			if cbqID != "" {
				notify.AnswerCallback(cfg.TelegramToken, cbqID, "Sistem baştan başlatılıyor!")
			}
			notify.SendTelegram(cfg.TelegramToken, chatID, "🔄 *Sistem Kapatılıp Yeniden Başlatılıyor...*")
			// Exit komutunu asenkron yapıp 3 saniye bekletiyoruz ki
			// Telegram'a offset UpdateID bildirimi (Ack) iletilebilsin. 
			// Aksi takdirde sonsuz yeniden başlama döngüsüne (Restart Loop) gireriz.
			go func() {
				time.Sleep(3 * time.Second)
				os.Exit(0)
			}()

		case "cmd_stats":
			if cbqID != "" {
				notify.AnswerCallback(cfg.TelegramToken, cbqID, "Sistem bilgileri getiriliyor...")
			}
			stats := notify.GetSystemStats(cfg.PollInterval)
			notify.SendMenu(cfg.TelegramToken, chatID, stats)
		case "cmd_grades":
			cacheMu.RLock()
			courses := cachedCourses
			cacheMu.RUnlock()

			if len(courses) == 0 {
				if cbqID != "" {
					notify.AnswerCallback(cfg.TelegramToken, cbqID, "Henüz sistem notları çekmedi, 1 dakika bekle!")
				}
				notify.SendTelegram(cfg.TelegramToken, chatID, "⚠️ Henüz sisteme giriş sağlanıp notlar okunmadı. Lütfen 30-40 saniye botun captcha'yı geçmesini bekleyin.")
			} else {
				if cbqID != "" {
					notify.AnswerCallback(cfg.TelegramToken, cbqID, "Notların hazır!")
				}
				var msgBuilder strings.Builder
				msgBuilder.WriteString("📖 *O Anki Hafızadaki Notların:*\n")

				for _, c := range courses {
					msgBuilder.WriteString(fmt.Sprintf("\n📚 *%s*\n", c.Name))
					for _, comp := range c.Components {
						weight := ""
						if comp.Weight != "" {
							weight = fmt.Sprintf(" (%s)", comp.Weight)
						}
						msgBuilder.WriteString(fmt.Sprintf("   • %s%s: *%s*\n", comp.Name, weight, comp.Score))
					}
				}
				notify.SendMenu(cfg.TelegramToken, chatID, msgBuilder.String())
			}
		}
	})

	ticker := time.NewTicker(cfg.PollInterval)
	defer ticker.Stop()

	firstSuccessNotified := false

	// İlk döngü
	courses, success := run(client, cfg)
	if success {
		cacheMu.Lock()
		cachedCourses = courses
		cacheMu.Unlock()
		if !firstSuccessNotified {
			sendInitialGrades(cfg, courses)
			firstSuccessNotified = true
		}
	}

	// Rutin döngü
	for range ticker.C {
		cacheMu.RLock()
		paused := isPaused
		cacheMu.RUnlock()

		if paused {
			log.Println("Sistem duraklatıldığı için kontrol es geçiliyor...")
			continue
		}

		courses, success := run(client, cfg)
		if success {
			cacheMu.Lock()
			cachedCourses = courses
			cacheMu.Unlock()
			if !firstSuccessNotified {
				sendInitialGrades(cfg, courses)
				firstSuccessNotified = true
			}
		}

		// Her kontrol bittikten sonra RAM'deki çöpü (Garbage Collector) 
		// agresif olarak temizleyip işletim sistemine iade et 
		// (Memory Leak algısını önlemek için)
		runtime.GC()
		debug.FreeOSMemory()
	}
}

func sendInitialGrades(cfg *config.Config, courses []scraper.Course) {
	var msgBuilder strings.Builder
	msgBuilder.WriteString("✅ OIS'e başarıyla giriş yapıldı!\n\n*Mevcut Notların:*\n")

	for _, c := range courses {
		msgBuilder.WriteString(fmt.Sprintf("\n📚 *%s*\n", c.Name))
		for _, comp := range c.Components {
			weight := ""
			if comp.Weight != "" {
				weight = fmt.Sprintf(" (%s)", comp.Weight)
			}
			msgBuilder.WriteString(fmt.Sprintf("   • %s%s: *%s*\n", comp.Name, weight, comp.Score))
		}
	}

	msgBuilder.WriteString("\n_(Sistem takibe devam ediyor...)_")
	
	if err := notify.SendMenu(cfg.TelegramToken, cfg.TelegramChatID, msgBuilder.String()); err != nil {
		log.Printf("İlk not durumu Telegram gönderim hatası: %v", err)
	}
}

func run(client *http.Client, cfg *config.Config) ([]scraper.Course, bool) {
	// Login (max 15 CAPTCHA denemesi)
	var loginOK bool
	for attempt := 0; attempt < 15; attempt++ {
		result, err := auth.Login(client, cfg)
		if err != nil {
			log.Printf("login hata: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}
		if result.Success {
			loginOK = true
			break
		}
		log.Printf("login başarısız [%d/15] (%s), tekrar deneniyor...", attempt+1, result.Reason)
		time.Sleep(1 * time.Second)
	}
	if !loginOK {
		log.Println("15 denemede login sağlanamadı, bu döngü atlanıyor")
		return nil, false
	}

	// Not çek
	courses, err := scraper.FetchGrades(client, cfg)
	if err != nil {
		log.Printf("not çekme hata: %v", err)
		return nil, false
	}
	log.Printf("%d ders bulundu", len(courses))

	// Diff
	changed, changes, err := diff.Check(courses, cfg.StateFile)
	if err != nil {
		log.Printf("diff hata: %v", err)
		return nil, false
	}
	if !changed {
		log.Println("değişiklik yok")
		return courses, true
	}

	// Bildirim
	msg := diff.FormatMessage(changes)
	if err := notify.SendMenu(cfg.TelegramToken, cfg.TelegramChatID, msg); err != nil {
		log.Printf("telegram hata: %v", err)
		return courses, true
	}
	log.Printf("bildirim gönderildi: %d değişiklik", len(changes))
	return courses, true
}
