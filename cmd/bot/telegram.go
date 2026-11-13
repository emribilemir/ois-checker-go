package main

import (
	"log"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"notbot/config"
	"notbot/internal/notify"
	"notbot/internal/view"
)

// bot Telegram komutlarını ve düğme basışlarını karşılar. Düğmeye basılınca
// yeni mesaj yerine düğmenin bulunduğu mesaj güncellenir; yazılan komutlara
// yeni mesajla cevap verilir.
type bot struct {
	cfg     *config.Config
	client  *http.Client
	program *scheduleService
}

func (b *bot) reply(u notify.Update, m notify.Message) {
	var err error
	if u.MessageID != 0 {
		err = notify.Edit(b.cfg.TelegramToken, u.ChatID, u.MessageID, m)
	} else {
		err = notify.Send(b.cfg.TelegramToken, u.ChatID, m)
	}
	if err != nil {
		log.Printf("Telegram cevabı gönderilemedi (%s): %v", u.Command, err)
	}
}

// toast düğmenin dönen simgesini durdurur; text boş değilse kısa bilgi gösterir.
func (b *bot) toast(u notify.Update, text string) {
	if u.CallbackID != "" {
		notify.AnswerCallback(b.cfg.TelegramToken, u.CallbackID, text)
	}
}

func (b *bot) settings(u notify.Update, toast string) {
	b.toast(u, toast)
	b.reply(u, view.Settings(currentStatus(b.cfg)))
}

func (b *bot) handle(u notify.Update) {
	// Sadece yapılandırılmış sohbete cevap ver
	if u.ChatID != b.cfg.TelegramChatID {
		return
	}

	switch u.Command {
	case "/start", "/menu", "cmd_menu":
		b.toast(u, "")
		b.reply(u, view.Menu(currentStatus(b.cfg)))

	case "/ayarlar", "cmd_settings":
		b.settings(u, "")

	case "/notlar", "cmd_grades", "cmd_grades_new":
		if u.Command == "cmd_grades_new" {
			// Bildirimdeki düğme: bildirim yerinde kalsın, notlar yeni mesajla gelsin.
			u.MessageID = 0
		}
		cacheMu.RLock()
		courses := cachedCourses
		cacheMu.RUnlock()
		statusMu.RLock()
		checkedAt := lastCheckAt
		statusMu.RUnlock()
		if len(courses) == 0 {
			b.toast(u, "Notlar henüz hazır değil")
			b.reply(u, gradesUnavailableMessage())
			return
		}
		b.toast(u, "")
		b.reply(u, view.Grades(courses, "", checkedAt))

	case "/program", "cmd_schedule":
		b.toast(u, "Ders programı OİS'ten alınıyor…")
		if u.MessageID != 0 {
			b.reply(u, view.Loading("📅 Ders Programı"))
		}
		go func() {
			entries, err := b.program.Show()
			if err != nil {
				log.Printf("ders programı alınamadı: %v", err)
				b.reply(u, view.Failure("Ders programı alınamadı", err.Error(), "cmd_schedule"))
				return
			}
			b.reply(u, view.Week(entries, time.Now()))
		}()

	case "/sinav", "/sinavlar", "cmd_exams":
		b.toast(u, "Sınav takvimi OİS'ten alınıyor…")
		if u.MessageID != 0 {
			b.reply(u, view.Loading("📝 Sınav Takvimi"))
		}
		go func() {
			list, err := fetchExamCalendar(b.client, b.cfg)
			if err != nil {
				log.Printf("sınav takvimi alınamadı: %v", err)
				b.reply(u, view.Failure("Sınav takvimi alınamadı", err.Error(), "cmd_exams"))
				return
			}
			b.reply(u, view.Exams(list, time.Now()))
		}()

	case "cmd_reminders_on", "cmd_reminders_off":
		enabled := u.Command == "cmd_reminders_on"
		if err := b.program.SetEnabled(enabled); err != nil {
			log.Printf("ders uyarı tercihi kaydedilemedi: %v", err)
			b.toast(u, "Tercih kaydedilemedi; tekrar dene.")
			return
		}
		remindersActive.Store(enabled)
		if !enabled {
			b.settings(u, "Ders uyarıları kapatıldı")
			return
		}
		b.settings(u, "Ders uyarıları açıldı: dersten 15 dk önce haber vereceğim")
		go func() {
			if err := b.program.Check(time.Now()); err != nil {
				log.Printf("ders uyarısı ilk kontrol başarısız: %v", err)
				_ = notify.Send(b.cfg.TelegramToken, u.ChatID, view.Notice("⚠️ Ders uyarıları açık, ancak program şu an OİS'ten alınamadı. Bot yeniden deneyecek."))
			}
		}()

	case "cmd_pause", "cmd_resume":
		paused := u.Command == "cmd_pause"
		cacheMu.Lock()
		isPaused = paused
		cacheMu.Unlock()
		if paused {
			b.settings(u, "Not taraması durduruldu")
		} else {
			b.settings(u, "Not taraması sürüyor")
		}

	case "cmd_ders_secme_on", "cmd_ders_secme_off":
		active := u.Command == "cmd_ders_secme_on"
		cacheMu.Lock()
		isDersSecmeActive = active
		if !active {
			dersSecmeNotified = false // kapatılınca durumu da sıfırla
		}
		cacheMu.Unlock()
		if active {
			b.settings(u, "Ders seçme takibi açık")
		} else {
			b.settings(u, "Ders seçme takibi kapalı")
		}

	case "cmd_stats":
		b.toast(u, "")
		b.reply(u, view.Stats(b.cfg.PollInterval, atomic.LoadInt64(&checkCount), processStartedAt, time.Now()))

	case "cmd_restart":
		b.toast(u, "Yeniden başlatılıyor")
		b.reply(u, view.Restarting())
		// Çıkışı 3 saniye geciktir: Telegram'a bu güncellemenin alındığı (offset)
		// bildirilsin, yoksa bot açılınca aynı komutu tekrar görüp döngüye girer.
		go func() {
			time.Sleep(3 * time.Second)
			os.Exit(0)
		}()

	default:
		// Tanınmayan eski düğmeler: dönen simgeyi durdur.
		b.toast(u, "")
	}
}
