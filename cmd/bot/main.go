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
	"notbot/internal/dersecme"
	"notbot/internal/diff"
	"notbot/internal/notify"
	"notbot/internal/scraper"
	"notbot/internal/session"
)

var (
	cacheMu           sync.RWMutex
	cachedCourses     []scraper.Course
	isPaused          bool
	isDersSecmeActive bool
	dersSecmeNotified bool
	checkCount        int64
	statusMu          sync.RWMutex
	lastCheckError    string
	lastCheckAt       time.Time
	processStartedAt  = time.Now()
)

const pollerStaleAfter = 10 * time.Minute

func main() {
	log.SetOutput(os.Stdout)
	cfg := config.Load()
	isDersSecmeActive = cfg.DersSecmeActive
	client := session.New(cfg.UserAgent)

	log.Printf("Bot başladı. Kontrol aralığı: %s", cfg.PollInterval)

	msgStr := fmt.Sprintf("🤖 OIS Checker Bot Başladı!\n⏱️ Kontrol Aralığı: %.0f dakika\nNotlarını kontrol etmeye başlıyorum...", cfg.PollInterval.Minutes())
	if err := notify.SendMenu(cfg.TelegramToken, cfg.TelegramChatID, msgStr, isPaused, isDersSecmeActive); err != nil {
		log.Printf("Başlangıç Telegram mesajı hatası: %v", err)
	}

	// Render bir Web Service'in PORT üzerinde dinlemesini bekler. Dışarıdan düzenli
	// ping almak ücretsiz servisin uyumasını önler; tarama döngüsü kilitlenirse bu
	// endpoint 503 döndürerek Render sağlık kontrolünün örneği yeniden başlatmasını sağlar.
	if port := os.Getenv("PORT"); port != "" {
		go func() {
			http.Handle("/", healthHandler())
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
			paused, active := controlState()
			notify.SendMenu(cfg.TelegramToken, chatID, fmt.Sprintf("👋 Hoşgeldin! Aşağıdaki menüden istediklerine direkt ulaşabilirsin:\n_(Şu anki rutin kontrol aralığı: %.0f dakikada bir)_", cfg.PollInterval.Minutes()), paused, active)

		case "cmd_pause":
			cacheMu.Lock()
			isPaused = true
			active := isDersSecmeActive
			cacheMu.Unlock()
			if cbqID != "" {
				notify.AnswerCallback(cfg.TelegramToken, cbqID, "Tarama duraklatıldı.")
			}
			notify.SendMenu(cfg.TelegramToken, chatID, "⏸ *Bot Duraklatıldı.*\nArkaplanda not kontrolü yapılmayacak. Yeniden başlatmak için menüden Devam Et tuşuna basabilirsin.", true, active)

		case "cmd_resume":
			cacheMu.Lock()
			isPaused = false
			active := isDersSecmeActive
			cacheMu.Unlock()
			if cbqID != "" {
				notify.AnswerCallback(cfg.TelegramToken, cbqID, "Tarama sürdürülüyor.")
			}
			notify.SendMenu(cfg.TelegramToken, chatID, "▶️ *Bot Devam Ediyor.*\nArkaplanda OIS kontrol döngüsü aktif edildi.", false, active)

		case "cmd_ders_secme_on":
			cacheMu.Lock()
			isDersSecmeActive = true
			paused := isPaused
			cacheMu.Unlock()
			if cbqID != "" {
				notify.AnswerCallback(cfg.TelegramToken, cbqID, "Ders seçme takibi AKTİF!")
			}
			notify.SendMenu(cfg.TelegramToken, chatID, "📋 *Ders Seçme Takibi Aktif Edildi.*\nDers kayıt süreci başladığında anında haber vereceğim.", paused, true)

		case "cmd_ders_secme_off":
			cacheMu.Lock()
			isDersSecmeActive = false
			dersSecmeNotified = false // kapatılınca durumu da sıfırla
			paused := isPaused
			cacheMu.Unlock()
			if cbqID != "" {
				notify.AnswerCallback(cfg.TelegramToken, cbqID, "Ders seçme takibi KAPATILDI.")
			}
			notify.SendMenu(cfg.TelegramToken, chatID, "🚫 *Ders Seçme Takibi Kapatıldı.*", paused, false)

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
			stats := notify.GetSystemStats(cfg.PollInterval, atomic.LoadInt64(&checkCount))
			paused, active := controlState()
			notify.SendMenu(cfg.TelegramToken, chatID, stats, paused, active)
		case "cmd_grades":
			cacheMu.RLock()
			courses := cachedCourses
			paused := isPaused
			active := isDersSecmeActive
			cacheMu.RUnlock()

			if len(courses) == 0 {
				if cbqID != "" {
					notify.AnswerCallback(cfg.TelegramToken, cbqID, "Henüz sistem notları çekmedi, 1 dakika bekle!")
				}
				notify.SendTelegram(cfg.TelegramToken, chatID, gradesUnavailableMessage())
			} else {
				if cbqID != "" {
					notify.AnswerCallback(cfg.TelegramToken, cbqID, "Notların hazır!")
				}
				var msgBuilder strings.Builder

				for _, c := range courses {
					msgBuilder.WriteString(fmt.Sprintf("\n📚 *%s*\n", c.DisplayName()))
					for _, comp := range c.Components {
						weight := ""
						if comp.Weight != "" {
							weight = fmt.Sprintf(" %s", scraper.FormatWeight(comp.Weight))
						}
						msgBuilder.WriteString(fmt.Sprintf("   • %s%s: *%s*\n", comp.Name, weight, comp.Score))
					}
				}
				notify.SendMenu(cfg.TelegramToken, chatID, msgBuilder.String(), paused, active)
			}
		}
	})

	ticker := time.NewTicker(cfg.PollInterval)
	defer ticker.Stop()

	firstSuccessNotified := false

	// İlk döngü
	courses, success := run(client, cfg)
	if success && courses != nil {
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
		courses, success := run(client, cfg)
		if success && courses != nil {
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

func healthHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		statusMu.RLock()
		checkedAt := lastCheckAt
		statusMu.RUnlock()
		if checkedAt.IsZero() {
			checkedAt = processStartedAt
		}
		if time.Since(checkedAt) > pollerStaleAfter {
			http.Error(w, "OIS polling loop is stalled", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OIS Bot is fully awake and running!"))
	})
}

func sendInitialGrades(cfg *config.Config, courses []scraper.Course) {
	var msgBuilder strings.Builder
	msgBuilder.WriteString("✅ OIS'e başarıyla giriş yapıldı!\n\n*Mevcut Notların:*\n")

	for _, c := range courses {
		msgBuilder.WriteString(fmt.Sprintf("\n📚 *%s*\n", c.DisplayName()))
		for _, comp := range c.Components {
			weight := ""
			if comp.Weight != "" {
				weight = fmt.Sprintf(" %s", scraper.FormatWeight(comp.Weight))
			}
			msgBuilder.WriteString(fmt.Sprintf("   • %s%s: *%s*\n", comp.Name, weight, comp.Score))
		}
	}

	msgBuilder.WriteString("\n_(Sistem takibe devam ediyor...)_")

	paused, active := controlState()
	if err := notify.SendMenu(cfg.TelegramToken, cfg.TelegramChatID, msgBuilder.String(), paused, active); err != nil {
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
		reason := "15 denemede giriş sağlanamadı (CAPTCHA veya kimlik doğrulama hatası)"
		recordCheckFailure(reason)
		log.Println("15 denemede login sağlanamadı, bu döngü atlanıyor")
		return nil, false
	}
	return runAuthenticated(client, cfg)
}

func runAuthenticated(client *http.Client, cfg *config.Config) ([]scraper.Course, bool) {
	// Ders seçme takibi not sayfasındaki değişikliklerden ve parser hatalarından bağımsızdır.
	runDersSecmeCheck(client, cfg)

	cacheMu.RLock()
	paused := isPaused
	cacheMu.RUnlock()
	if paused {
		recordCheckSuccess()
		log.Println("Not taraması duraklatıldı; ders seçme takibi çalışmaya devam ediyor")
		return nil, true
	}

	// Not çek
	courses, err := scraper.FetchGrades(client, cfg)
	if err != nil {
		recordCheckFailure(err.Error())
		log.Printf("not çekme hata: %v", err)
		return nil, false
	}
	log.Printf("%d ders bulundu", len(courses))

	// Diff
	changed, changes, err := diff.Check(courses, cfg.StateFile)
	if err != nil {
		recordCheckFailure(err.Error())
		log.Printf("diff hata: %v", err)
		return nil, false
	}
	if !changed {
		recordCheckSuccess()
		log.Println("değişiklik yok")
		return courses, true
	}

	// Bildirim
	msg := diff.FormatMessage(changes)
	paused, active := controlState()
	if err := notify.SendMenu(cfg.TelegramToken, cfg.TelegramChatID, msg, paused, active); err != nil {
		log.Printf("telegram hata: %v", err)
	} else {
		log.Printf("bildirim gönderildi: %d değişiklik", len(changes))
	}
	recordCheckSuccess()

	return courses, true
}

func recordCheckFailure(reason string) {
	statusMu.Lock()
	lastCheckError = reason
	lastCheckAt = time.Now()
	statusMu.Unlock()
}

func recordCheckSuccess() {
	statusMu.Lock()
	lastCheckError = ""
	lastCheckAt = time.Now()
	statusMu.Unlock()
}

func gradesUnavailableMessage() string {
	statusMu.RLock()
	reason := lastCheckError
	checkedAt := lastCheckAt
	statusMu.RUnlock()

	if reason == "" {
		return "⚠️ Henüz başarılı bir not kontrolü tamamlanmadı. Bot otomatik olarak yeniden deniyor."
	}
	return fmt.Sprintf("⚠️ Henüz güncel notlar alınamadı.\n\nSon hata: %s\nSon deneme: %s\n\nBot otomatik olarak yeniden deniyor.", reason, checkedAt.Format("02/01/2006 15:04:05"))
}

func controlState() (paused, courseSelectionActive bool) {
	cacheMu.RLock()
	defer cacheMu.RUnlock()
	return isPaused, isDersSecmeActive
}

func runDersSecmeCheck(client *http.Client, cfg *config.Config) {
	cacheMu.RLock()
	active := isDersSecmeActive
	notified := dersSecmeNotified
	cacheMu.RUnlock()
	if !active {
		return
	}

	status, err := dersecme.Inspect(client, cfg)
	if err != nil {
		log.Printf("ders seçme kontrol hata: %v", err)
		return
	}

	if status.Open && !notified {
		msg := fmt.Sprintf("🚨 *DERS SEÇME AKTİF!*\n\nOIS menüsünde \"%s\" ifadesi tespit edildi!%s\n\nHemen giriş yap: %s\n\n⏰ Tespit: %s",
			status.Signal, formatCurrentSelectedCourses(status.SelectedCourses), cfg.UniversityURL, time.Now().Format("02/01/2006 15:04:05"))
		if err := notify.SendTelegram(cfg.TelegramToken, cfg.TelegramChatID, msg); err != nil {
			log.Printf("ders seçme Telegram bildirim hata; sonraki kontrolde yeniden denenecek: %v", err)
		} else {
			cacheMu.Lock()
			dersSecmeNotified = true
			cacheMu.Unlock()
		}
	} else if !status.Open {
		cacheMu.Lock()
		dersSecmeNotified = false
		cacheMu.Unlock()
	}

	if status.Open && status.SelectedCourses != nil && cfg.StateFile != "" {
		stateFile := cfg.StateFile + ".derssecme"
		changes, err := dersecme.SelectionChanges(status.SelectedCourses, stateFile)
		if err != nil {
			log.Printf("ders listesi karşılaştırma hata: %v", err)
			return
		}
		if len(changes) == 0 {
			return
		}

		msg := formatSelectionChanges(changes)
		if err := notify.SendTelegram(cfg.TelegramToken, cfg.TelegramChatID, msg); err != nil {
			log.Printf("ders listesi Telegram bildirim hata; sonraki kontrolde yeniden denenecek: %v", err)
			return
		}
		if err := dersecme.SaveSelectionState(status.SelectedCourses, stateFile); err != nil {
			log.Printf("ders listesi state kaydetme hata: %v", err)
		}
	}
}

func formatCurrentSelectedCourses(courses []dersecme.SelectedCourse) string {
	if len(courses) == 0 {
		return ""
	}
	var message strings.Builder
	message.WriteString(fmt.Sprintf("\n\n📚 *OİS'te şu an seçili %d ders var:*", len(courses)))
	for _, course := range courses {
		message.WriteString(fmt.Sprintf("\n• %s - %s", course.Code, course.Name))
	}
	return message.String()
}

func formatSelectionChanges(changes []dersecme.SelectionChange) string {
	var message strings.Builder
	message.WriteString("🔔 *OİS Ders Listen Değişti!*\n")
	for _, change := range changes {
		symbol := "➕"
		label := "Eklendi"
		if change.Type == "removed" {
			symbol = "➖"
			label = "Silindi"
		}
		message.WriteString(fmt.Sprintf("\n%s *%s:* %s - %s", symbol, label, change.Course.Code, change.Course.Name))
	}
	message.WriteString("\n\nℹ️ Bu değişikliğin senin işlemin mi yoksa OİS'in otomatik ataması mı olduğu HTML'den ayırt edilemiyor.")
	return message.String()
}
