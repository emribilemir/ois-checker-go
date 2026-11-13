package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"runtime/debug"
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
	"notbot/internal/view"
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
		func(message notify.Message) error {
			return notify.Send(cfg.TelegramToken, cfg.TelegramChatID, message)
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
	view.Location = location
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

	if err := notify.SetCommands(cfg.TelegramToken, notify.Commands); err != nil {
		log.Printf("Telegram komut menüsü ayarlanamadı: %v", err)
	}
	if err := notify.Send(cfg.TelegramToken, cfg.TelegramChatID, view.Started(currentStatus(cfg))); err != nil {
		log.Printf("Başlangıç Telegram mesajı hatası: %v", err)
	}

	// Render bir Web Service'in PORT üzerinde dinlemesini bekler. Dışarıdan düzenli
	// ping almak ücretsiz servisin uyumasını önler; tarama döngüsü kilitlenirse bu
	// endpoint 503 döndürerek Render sağlık kontrolünün örneği yeniden başlatmasını sağlar.
	if port := os.Getenv("PORT"); port != "" {
		go func() {
			http.Handle("/", healthHandler(cfg.PollInterval))
			log.Printf("Render Web Service için PORT %s dinleniyor...", port)
			if err := http.ListenAndServe(":"+port, nil); err != nil {
				log.Printf("HTTP Sunucu hatası: %v", err)
			}
		}()
	}

	// Telegram komut ve düğme dinleyicisini arkaplanda başlat
	b := &bot{cfg: cfg, client: client, program: program}
	go notify.StartPoller(cfg.TelegramToken, b.handle)

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

// staleThreshold, uzun kontrol aralıklarında sağlıklı bir döngünün iki tur
// arasında 503 almasını önler.
func staleThreshold(pollInterval time.Duration) time.Duration {
	if limit := 2 * pollInterval; limit > pollerStaleAfter {
		return limit
	}
	return pollerStaleAfter
}

func healthHandler(pollInterval time.Duration) http.Handler {
	staleAfter := staleThreshold(pollInterval)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		statusMu.RLock()
		checkedAt := lastCheckAt
		statusMu.RUnlock()
		if checkedAt.IsZero() {
			checkedAt = processStartedAt
		}
		if time.Since(checkedAt) > staleAfter {
			http.Error(w, "OIS polling loop is stalled", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OIS Bot is fully awake and running!"))
	})
}

func sendInitialGrades(cfg *config.Config, courses []scraper.Course) {
	message := view.Grades(courses, "OİS'e giriş yapıldı, takip sürüyor", time.Now())
	message.Silent = true
	if err := notify.Send(cfg.TelegramToken, cfg.TelegramChatID, message); err != nil {
		log.Printf("İlk not durumu Telegram gönderim hatası: %v", err)
	}
}

func run(client *http.Client, cfg *config.Config) ([]scraper.Course, bool) {
	oisMu.Lock()
	defer oisMu.Unlock()
	atomic.AddInt64(&checkCount, 1)
	if paused, active := controlState(); paused && !active {
		// Ne not ne ders seçme taranıyor: OİS'e boşuna CAPTCHA'lı giriş yapma.
		recordCheckSuccess()
		log.Println("Not taraması duraklatıldı ve ders seçme takibi kapalı; OİS'e giriş atlandı")
		return nil, true
	}
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
	if err := notify.Send(cfg.TelegramToken, cfg.TelegramChatID, view.GradeChanges(changes)); err != nil {
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

func gradesUnavailableMessage() notify.Message {
	statusMu.RLock()
	reason := lastCheckError
	checkedAt := lastCheckAt
	statusMu.RUnlock()
	return view.GradesUnavailable(reason, checkedAt)
}

func currentStatus(cfg *config.Config) view.Status {
	paused, active := controlState()
	statusMu.RLock()
	defer statusMu.RUnlock()
	return view.Status{
		Paused: paused, CourseSelection: active, Reminders: remindersEnabled(),
		Interval: cfg.PollInterval, LastCheck: lastCheckAt, LastError: lastCheckError, Now: time.Now(),
	}
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
		msg := view.CourseSelectionOpened(status.Signal, status.SelectedCourses, cfg.UniversityURL, time.Now())
		if err := notify.Send(cfg.TelegramToken, cfg.TelegramChatID, msg); err != nil {
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
			if err := notify.Send(cfg.TelegramToken, cfg.TelegramChatID, view.SelectionChanges(changes)); err != nil {
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
			if err := notify.Send(cfg.TelegramToken, cfg.TelegramChatID, view.PoolAvailable(available)); err != nil {
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
