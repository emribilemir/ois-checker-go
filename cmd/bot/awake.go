package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"notbot/internal/awake"
	"notbot/internal/view"
)

// courseSelectionOpen, son kontrolde ders seçmenin açık görüldüğünü söyler.
var courseSelectionOpen atomic.Bool

// awakeUntil kullanıcının Telegram'dan seçtiği uyanık kalma sonudur (gün, UTC
// gece yarısı). Sıfırsa akademik takvim geçerlidir.
var (
	awakeMu    sync.RWMutex
	awakeUntil time.Time
)

func chosenAwakeUntil() time.Time {
	awakeMu.RLock()
	defer awakeMu.RUnlock()
	return awakeUntil
}

func setChosenAwakeUntil(d time.Time) {
	awakeMu.Lock()
	awakeUntil = d
	awakeMu.Unlock()
}

func dayOf(t time.Time) time.Time {
	t = t.In(view.Location)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// awakePlan botun şu an uyanık kalıp kalmayacağını, kalacaksa hangi güne kadar
// ve neden kalacağını söyler. until yalnız tarihe bağlı nedenlerde doludur.
type awakePlan struct {
	Awake  bool
	Until  time.Time
	Reason string
	Chosen bool // kullanıcının seçimi geçerli
}

func planAwake(now time.Time) awakePlan {
	today := dayOf(now)
	chosen := chosenAwakeUntil()
	calendarAllowed := true
	if !chosen.IsZero() {
		if !today.After(chosen) {
			return awakePlan{Awake: true, Until: chosen, Chosen: true, Reason: "Senin seçimin: " + view.UntilPhrase(chosen) + " uyanık"}
		}
		// Seçim geçti: seçimden önce başlamış takvim dönemi seçimle kesilmiş sayılır;
		// seçimden sonra başlayan yeni dönem yine geçerlidir.
		if w, ok := awake.Active(awake.Windows, now.In(view.Location)); ok && !w.Start.After(chosen) {
			calendarAllowed = false
		}
	}
	if calendarAllowed {
		if until, ok := awake.ActiveUntil(awake.Windows, now.In(view.Location)); ok {
			return awakePlan{Awake: true, Until: until, Reason: "Sınav / kayıt dönemi: " + view.UntilPhrase(until) + " uyanık"}
		}
	}
	if courseSelectionOpen.Load() {
		return awakePlan{Awake: true, Reason: "Ders seçme açık: açık kaldıkça uyanık"}
	}
	if remindersEnabled() {
		return awakePlan{Awake: true, Reason: "Ders uyarıları açık: uyarılar için uyanık"}
	}
	if next, ok := awake.Next(awake.Windows, now.In(view.Location)); ok {
		return awakePlan{Reason: "Dönem dışı: istek gelmezse bot uyur, " + view.OnDay(next.Start) + " uyanır"}
	}
	return awakePlan{Reason: "Dönem dışı: takvimde sıradaki dönem yok, bot uyur"}
}

// stayAwake botun şu an kendini uyanık tutup tutmayacağını ve nedenini söyler.
func stayAwake(now time.Time) (bool, string) {
	p := planAwake(now)
	return p.Awake, p.Reason
}

// extendAwake uyanık kalma sonunu şu anki sondan (uyanık değilse bugünden) days gün ileri alır.
func extendAwake(now time.Time, days int) time.Time {
	base := dayOf(now)
	if p := planAwake(now); p.Awake && !p.Until.IsZero() && p.Until.After(base) {
		base = p.Until
	}
	return clampAwake(now, base.AddDate(0, 0, days))
}

// maxAwakeAhead seçilebilecek en uzak tarihtir; Render'ın aylık saat kotası
// zaten bir ayı tam karşılıyor, daha uzağı unutulup kota harcamasın.
const maxAwakeAhead = 120

func clampAwake(now, d time.Time) time.Time {
	if limit := dayOf(now).AddDate(0, 0, maxAwakeAhead); d.After(limit) {
		return limit
	}
	return d
}

var awakeDateRE = regexp.MustCompile(`^(\d{1,2})[./](\d{1,2})(?:[./](\d{4}))?$`)

// parseAwakeDate "20.12", "20.12.2026" ya da "2026-12-20" okur. Yılsız tarih
// geçmişte kalıyorsa gelecek yıl sayılır.
func parseAwakeDate(text string, now time.Time) (time.Time, error) {
	text = strings.TrimSpace(text)
	today := dayOf(now)
	if d, err := time.Parse("2006-01-02", text); err == nil {
		return d, nil
	}
	m := awakeDateRE.FindStringSubmatch(text)
	if m == nil {
		return time.Time{}, fmt.Errorf("tarih anlaşılmadı: %q (örnek: 20.12 ya da 20.12.2026)", text)
	}
	day, _ := strconv.Atoi(m[1])
	month, _ := strconv.Atoi(m[2])
	year := today.Year()
	if m[3] != "" {
		year, _ = strconv.Atoi(m[3])
	}
	d := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	if d.Day() != day || int(d.Month()) != month {
		return time.Time{}, fmt.Errorf("böyle bir gün yok: %q", text)
	}
	if m[3] == "" && d.Before(today) {
		d = d.AddDate(1, 0, 0)
	}
	return d, nil
}

// sleepNotice dönem bitip bot uykuya geçerken bir kez gönderilir.
func sleepNotice(reason string) string {
	return "😴 Uyanık kalma dönemi bitti. " + reason + ". Uyurken Telegram komutlarına cevap vermez; Telegram mesajı onu uyandırmaz. Arada lazım olursa botun adresini (" + publicURL() + ") tarayıcıda açmak yaklaşık bir dakikada uyandırır."
}

func publicURL() string {
	if u := strings.TrimRight(os.Getenv("RENDER_EXTERNAL_URL"), "/"); u != "" {
		return u
	}
	return "Render adresi"
}
