package main

import (
	"fmt"
	"html"
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
	"notbot/internal/schedule"
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
	oisMu             sync.Mutex
	remindersActive   atomic.Bool
)

const (
	pollerStaleAfter = 10 * time.Minute
	maxLoginAttempts = 5
)

type loginFunction func(*http.Client, *config.Config) (auth.LoginResult, error)

func main() {
	log.SetOutput(os.Stdout)
	cfg := config.Load()
	isDersSecmeActive = cfg.DersSecmeActive
	client := session.New(cfg.UserAgent)
	scheduleStatePath := ""
	if cfg.StateFile != "" {
		scheduleStatePath = cfg.StateFile + ".schedule"
	}
	program, err := newScheduleService(scheduleStatePath,
		func() ([]schedule.Entry, error) { return fetchAuthenticatedSchedule(client, cfg) },
		func(message string) error {
			paused, active := controlState()
			return notify.SendSchedule(cfg.TelegramToken, cfg.TelegramChatID, message, paused, active, true)
		},
	)
	if err != nil {
		log.Fatalf("ders uyarı durumu okunamadı: %v", err)
	}
	remindersActive.Store(program.Enabled())
	location, err := time.LoadLocation("Europe/Istanbul")
	if err != nil {
		log.Fatalf("Europe/Istanbul saat dilimi yüklenemedi: %v", err)
	}
	go func() {
		if err := program.Check(time.Now().In(location)); err != nil {
			log.Printf("ders uyarı ilk kontrolü başarısız: %v", err)
		}
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			if err := program.Check(time.Now().In(location)); err != nil {
				log.Printf("ders uyarı kontrolü başarısız: %v", err)
			}
		}
	}()

	log.Printf("Bot başladı. Kontrol aralığı: %s", cfg.PollInterval)

	msgStr := fmt.Sprintf("🤖 OIS Checker Bot Başladı!\n⏱️ Kontrol Aralığı: %.0f dakika\nNotlarını kontrol etmeye başlıyorum...", cfg.PollInterval.Minutes())
	if err := notify.SendMenu(cfg.TelegramToken, cfg.TelegramChatID, msgStr, isPaused, isDersSecmeActive, remindersEnabled()); err != nil {
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
			notify.SendMenu(cfg.TelegramToken, chatID, fmt.Sprintf("👋 Hoşgeldin! Aşağıdaki menüden istediklerine direkt ulaşabilirsin:\n_(Şu anki rutin kontrol aralığı: %.0f dakikada bir)_", cfg.PollInterval.Minutes()), paused, active, remindersEnabled())

		case "/program", "cmd_schedule":
			if cbqID != "" {
				notify.AnswerCallback(cfg.TelegramToken, cbqID, "Ders programı OİS'ten alınıyor...")
			}
			go func() {
				message, err := program.Show()
				if err != nil {
					log.Printf("ders programı alınamadı: %v", err)
					_ = notify.SendHTML(cfg.TelegramToken, chatID, "⚠️ Ders programı şu an alınamadı. Lütfen biraz sonra tekrar deneyin.\n\n"+html.EscapeString(err.Error()))
					return
				}
				paused, active := controlState()
				if err := notify.SendSchedule(cfg.TelegramToken, chatID, message, paused, active, remindersEnabled()); err != nil {
					log.Printf("ders programı Telegram gönderim hatası: %v", err)
				}
			}()

		case "cmd_reminders_on", "cmd_reminders_off":
			enabled := cmd == "cmd_reminders_on"
			if err := program.SetEnabled(enabled); err != nil {
				log.Printf("ders uyarı tercihi kaydedilemedi: %v", err)
				if cbqID != "" {
					notify.AnswerCallback(cfg.TelegramToken, cbqID, "Tercih kaydedilemedi; tekrar deneyin.")
				}
				return
			}
			remindersActive.Store(enabled)
			if cbqID != "" {
				status := "Ders uyarıları kapatıldı."
				if enabled {
					status = "Ders uyarıları açıldı."
				}
				notify.AnswerCallback(cfg.TelegramToken, cbqID, status)
			}
			paused, active := controlState()
			message := "🔕 *Ders uyarıları kapatıldı.*"
			if enabled {
				message = "🔔 *Ders uyarıları açıldı.* Ders başlamadan 15 dakika önce haber vereceğim."
			}
			_ = notify.SendMenu(cfg.TelegramToken, chatID, message, paused, active, enabled)
			if enabled {
				go func() {
					if err := program.Check(time.Now().In(location)); err != nil {
						log.Printf("ders uyarısı ilk kontrol başarısız: %v", err)
						_ = notify.SendHTML(cfg.TelegramToken, chatID, "⚠️ Ders uyarıları açık, ancak program şu an OİS'ten alınamadı. Bot yeniden deneyecek.")
					}
				}()
			}

		case "cmd_pause":
			cacheMu.Lock()
			isPaused = true
			active := isDersSecmeActive
			cacheMu.Unlock()
			if cbqID != "" {
				notify.AnswerCallback(cfg.TelegramToken, cbqID, "Tarama duraklatıldı.")
			}
			notify.SendMenu(cfg.TelegramToken, chatID, "⏸ *Bot Duraklatıldı.*\nArkaplanda not kontrolü yapılmayacak. Yeniden başlatmak için menüden Devam Et tuşuna basabilirsin.", true, active, remindersEnabled())

		case "cmd_resume":
			cacheMu.Lock()
			isPaused = false
			active := isDersSecmeActive
			cacheMu.Unlock()
			if cbqID != "" {
				notify.AnswerCallback(cfg.TelegramToken, cbqID, "Tarama sürdürülüyor.")
			}
			notify.SendMenu(cfg.TelegramToken, chatID, "▶️ *Bot Devam Ediyor.*\nArkaplanda OIS kontrol döngüsü aktif edildi.", false, active, remindersEnabled())

		case "cmd_ders_secme_on":
			cacheMu.Lock()
			isDersSecmeActive = true
			paused := isPaused
			cacheMu.Unlock()
			if cbqID != "" {
				notify.AnswerCallback(cfg.TelegramToken, cbqID, "Ders seçme takibi AKTİF!")
			}
			notify.SendMenu(cfg.TelegramToken, chatID, "📋 *Ders Seçme Takibi Aktif Edildi.*\nDers kayıt süreci başladığında anında haber vereceğim.", paused, true, remindersEnabled())

		case "cmd_ders_secme_off":
			cacheMu.Lock()
			isDersSecmeActive = false
			dersSecmeNotified = false // kapatılınca durumu da sıfırla
			paused := isPaused
			cacheMu.Unlock()
			if cbqID != "" {
				notify.AnswerCallback(cfg.TelegramToken, cbqID, "Ders seçme takibi KAPATILDI.")
			}
			notify.SendMenu(cfg.TelegramToken, chatID, "🚫 *Ders Seçme Takibi Kapatıldı.*", paused, false, remindersEnabled())

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
			notify.SendMenu(cfg.TelegramToken, chatID, stats, paused, active, remindersEnabled())
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
				notify.SendMenu(cfg.TelegramToken, chatID, msgBuilder.String(), paused, active, remindersEnabled())
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
	if err := notify.SendMenu(cfg.TelegramToken, cfg.TelegramChatID, msgBuilder.String(), paused, active, remindersEnabled()); err != nil {
		log.Printf("İlk not durumu Telegram gönderim hatası: %v", err)
	}
}

func run(client *http.Client, cfg *config.Config) ([]scraper.Course, bool) {
	oisMu.Lock()
	defer oisMu.Unlock()
	atomic.AddInt64(&checkCount, 1)
	loginOK, failureReason := tryLogin(client, cfg, auth.Login, time.Sleep)
	if !loginOK {
		reason := fmt.Sprintf("%d denemede giriş sağlanamadı (son hata: %s)", maxLoginAttempts, failureReason)
		if failureReason == "invalid_credentials" {
			reason = "OIS kullanıcı adı veya şifresi reddedildi"
		}
		recordCheckFailure(reason)
		log.Printf("login sağlanamadı, bu döngü atlanıyor: %s", reason)
		return nil, false
	}
	return runAuthenticated(client, cfg)
}

func tryLogin(client *http.Client, cfg *config.Config, login loginFunction, pause func(time.Duration)) (bool, string) {
	lastReason := "login_rejected"
	for attempt := 0; attempt < maxLoginAttempts; attempt++ {
		log.Printf("OIS'e giriş deneniyor (Deneme %d/%d)...", attempt+1, maxLoginAttempts)
		result, err := login(client, cfg)
		if err != nil {
			log.Printf("login hata: %v", err)
			lastReason = err.Error()
			if attempt+1 < maxLoginAttempts {
				pause(2 * time.Second)
			}
			continue
		}
		if result.Success {
			return true, ""
		}
		lastReason = result.Reason
		if result.Reason == "invalid_credentials" {
			log.Printf("login başarısız [%d/%d] (%s), tekrar denenmeyecek", attempt+1, maxLoginAttempts, result.Reason)
			return false, lastReason
		}
		log.Printf("login başarısız [%d/%d] (%s), tekrar deneniyor...", attempt+1, maxLoginAttempts, result.Reason)
		if attempt+1 < maxLoginAttempts {
			pause(1 * time.Second)
		}
	}
	return false, lastReason
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
	if err := notify.SendMenu(cfg.TelegramToken, cfg.TelegramChatID, msg, paused, active, remindersEnabled()); err != nil {
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
		if len(changes) > 0 {
			msg := formatSelectionChanges(changes)
			if err := notify.SendTelegram(cfg.TelegramToken, cfg.TelegramChatID, msg); err != nil {
				log.Printf("ders listesi Telegram bildirim hata; sonraki kontrolde yeniden denenecek: %v", err)
			} else if err := dersecme.SaveSelectionState(status.SelectedCourses, stateFile); err != nil {
				log.Printf("ders listesi state kaydetme hata: %v", err)
			}
		}
	}
	if status.Open && cfg.StateFile != "" {
		checkElectivePools(client, cfg, status.PoolPaths, status.SelectedCourses)
	}
}

func checkElectivePools(client *http.Client, cfg *config.Config, discovered []string, selected []dersecme.SelectedCourse) {
	stateFile := cfg.StateFile + ".electives"
	state, err := dersecme.LoadPoolState(stateFile)
	if err != nil {
		log.Printf("seçmeli havuz durumu okuma hata: %v", err)
		return
	}
	seen := make(map[string]bool)
	selectedCodes := make(map[string]bool, len(selected))
	for _, course := range selected {
		selectedCodes[course.Code] = true
	}
	for _, poolPath := range append(append([]string(nil), cfg.ElectivePoolPaths...), discovered...) {
		if seen[poolPath] {
			continue
		}
		seen[poolPath] = true
		courses, err := dersecme.FetchPool(client, cfg, poolPath)
		if err != nil {
			log.Printf("seçmeli havuz %s okunamadı: %v", poolPath, err)
			continue
		}
		available := dersecme.NewlyAvailable(state[poolPath], courses)
		filtered := available[:0]
		for _, course := range available {
			if !selectedCodes[course.Code] {
				filtered = append(filtered, course)
			}
		}
		available = filtered
		if len(available) > 0 {
			var msg strings.Builder
			msg.WriteString("🔔 <b>Seçmeli ders havuzunda yer var:</b>\n")
			for _, course := range available {
				msg.WriteString(fmt.Sprintf("\n• %s - %s (kalan kota: %d)", html.EscapeString(course.Code), html.EscapeString(course.Name), course.Quota))
			}
			msg.WriteString("\n\nBu havuzda online bilgisi gösterilmiyor; öğretim şeklini doğrulamak gerekiyor.")
			if err := notify.SendHTML(cfg.TelegramToken, cfg.TelegramChatID, msg.String()); err != nil {
				log.Printf("seçmeli havuz bildirim hata; sonraki kontrolde yeniden denenecek: %v", err)
				continue
			}
		}
		state[poolPath] = courses
		if err := dersecme.SavePoolState(stateFile, state); err != nil {
			log.Printf("seçmeli havuz durumu kaydetme hata: %v", err)
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
