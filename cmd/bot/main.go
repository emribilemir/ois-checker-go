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
	"sync/atomic"
	"time"

	"notbot/config"
	"notbot/internal/auth"
	"notbot/internal/diff"
	"notbot/internal/notify"
	"notbot/internal/scraper"
	"notbot/internal/session"
	"notbot/internal/dersecme"
)

var (
	cacheMu       sync.RWMutex
	cachedCourses []scraper.Course
	isPaused      bool
)

func main() {
	log.SetOutput(os.Stdout)
	cfg := config.Load()
	client := session.New(cfg.UserAgent)

	log.Printf("Bot başladı. Kontrol aralığı: %s", cfg.PollInterval)

	msgStr := fmt.Sprintf("🤖 OIS Checker Bot Başladı!\n⏱️ Kontrol Aralığı: %.0f dakika\nNotlarını kontrol etmeye başlıyorum...", cfg.PollInterval.Minutes())
	if err := notify.SendMenu(cfg.TelegramToken, cfg.TelegramChatID, msgStr, isPaused, isDersSecmeActive); err != nil {
		log.Printf("Başlangıç Telegram mesajı hatası: %v", err)
	}

	// Render üzerindeki bedava "Web Service" planında botun kapanmasını engellemek için:
	// Render, uygulamanın ayaklanıp bir portu dinlemesini bekler.
	// Buraya sahte bir sunucu açıyoruz. Eğer dışarıdan bir ping gelirse '200 OK' döner.
	// Böylece UptimeRobot gibi servislerle 5 dakikada bir ping atıp 7/24 ücretsiz uyandırabilirsin.
	if port := os.Getenv("PORT"); port != "" {
		go func() {
			http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte("OIS Bot is fully awake and running!"))
			})
			log.Printf("Render Web Service için PORT %s dinleniyor...", port)
			if err := http.ListenAndServe(":"+port, nil); err != nil {
				log.Printf("HTTP Sunucu hatası: %v", err)
			}
		}()
	}

	// Telegram Callback dinleyicisini arkaplanda başlat
	go notify.StartPoller(cfg.TelegramToken, func(cmd, chatID, cbqID string) {
		// Sadece yapılandırılmış yöneticiye cevap ver
		if chatID != cfg.TelegramChatID {
			return
		}

		switch cmd {
		case "/start":
			notify.SendMenu(cfg.TelegramToken, chatID, fmt.Sprintf("👋 Hoşgeldin! Aşağıdaki menüden istediklerine direkt ulaşabilirsin:\n_(Şu anki rutin kontrol aralığı: %.0f dakikada bir)_", cfg.PollInterval.Minutes()), isPaused, isDersSecmeActive)
		
		case "cmd_pause":
			cacheMu.Lock()
			isPaused = true
			cacheMu.Unlock()
			if cbqID != "" {
				notify.AnswerCallback(cfg.TelegramToken, cbqID, "Tarama duraklatıldı.")
			}
			notify.SendMenu(cfg.TelegramToken, chatID, "⏸ *Bot Duraklatıldı.*\nArkaplanda not kontrolü yapılmayacak. Yeniden başlatmak için menüden Devam Et tuşuna basabilirsin.", true, isDersSecmeActive)
		
		case "cmd_resume":
			cacheMu.Lock()
			isPaused = false
			cacheMu.Unlock()
			if cbqID != "" {
				notify.AnswerCallback(cfg.TelegramToken, cbqID, "Tarama sürdürülüyor.")
			}
			notify.SendMenu(cfg.TelegramToken, chatID, "▶️ *Bot Devam Ediyor.*\nArkaplanda OIS kontrol döngüsü aktif edildi.", false, isDersSecmeActive)
		
		case "cmd_ders_secme_on":
			isDersSecmeActive = true
			if cbqID != "" {
				notify.AnswerCallback(cfg.TelegramToken, cbqID, "Ders seçme takibi AKTİF!")
			}
			notify.SendMenu(cfg.TelegramToken, chatID, "📋 *Ders Seçme Takibi Aktif Edildi.*\nDers kayıt süreci başladığında anında haber vereceğim.", isPaused, true)

		case "cmd_ders_secme_off":
			isDersSecmeActive = false
			dersSecmeNotified = false // kapatılınca durumu da sıfırla
			if cbqID != "" {
				notify.AnswerCallback(cfg.TelegramToken, cbqID, "Ders seçme takibi KAPATILDI.")
			}
			notify.SendMenu(cfg.TelegramToken, chatID, "🚫 *Ders Seçme Takibi Kapatıldı.*", isPaused, false)

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
				notify.SendMenu(cfg.TelegramToken, chatID, msgBuilder.String(), isPaused, isDersSecmeActive)
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
	
	if err := notify.SendMenu(cfg.TelegramToken, cfg.TelegramChatID, msgBuilder.String(), isPaused, isDersSecmeActive); err != nil {
		log.Printf("İlk not durumu Telegram gönderim hatası: %v", err)
	}
}

func run(client *http.Client, cfg *config.Config) ([]scraper.Course, bool) {
	atomic.AddInt64(&checkCount, 1)
	// Login (max 15 CAPTCHA denemesi)
	var loginOK bool
	for attempt := 0; attempt < 15; attempt++ {
		log.Printf("OIS'e giriş deneniyor (Deneme %d/15)...", attempt+1)
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
	if err := notify.SendMenu(cfg.TelegramToken, cfg.TelegramChatID, msg, isPaused, isDersSecmeActive); err != nil {
		log.Printf("telegram hata: %v", err)
	} else {
		log.Printf("bildirim gönderildi: %d değişiklik", len(changes))
	}

	// Ders secme entegrasyonu (başarılı login sonrası)
	runDersSecmeCheck(client, cfg)

	return courses, true
}

func runDersSecmeCheck(client *http.Client, cfg *config.Config) {
	if !isDersSecmeActive {
		return
	}

	found, keyword, err := dersecme.Check(client, cfg)
	if err != nil {
		log.Printf("ders seçme kontrol hata: %v", err)
	} else if found && !dersSecmeNotified {
		msg := fmt.Sprintf("🚨 *DERS SEÇME AKTİF!*\n\nOIS menüsünde \"%s\" ifadesi tespit edildi!\nHemen giriş yap: %s\n\n⏰ Tespit: %s",
			keyword, cfg.UniversityURL, time.Now().Format("02/01/2006 15:04:05"))
		notify.SendTelegram(cfg.TelegramToken, cfg.TelegramChatID, msg)
		dersSecmeNotified = true
	} else if !found {
		dersSecmeNotified = false
	}
}
