package main

import (
	"os"
	"strings"
	"sync/atomic"
	"time"

	"notbot/internal/awake"
	"notbot/internal/view"
)

// courseSelectionOpen, son kontrolde ders seçmenin açık görüldüğünü söyler.
var courseSelectionOpen atomic.Bool

// stayAwake botun şu an kendini uyanık tutup tutmayacağını ve nedenini söyler.
func stayAwake(now time.Time) (bool, string) {
	now = now.In(view.Location)
	if until, ok := awake.ActiveUntil(awake.Windows, now); ok {
		return true, "Sınav / kayıt dönemi: " + view.UntilPhrase(until) + " uyanık"
	}
	if courseSelectionOpen.Load() {
		return true, "Ders seçme açık: açık kaldıkça uyanık"
	}
	if remindersEnabled() {
		return true, "Ders uyarıları açık: uyarılar için uyanık"
	}
	if next, ok := awake.Next(awake.Windows, now); ok {
		return false, "Dönem dışı: istek gelmezse bot uyur, " + view.OnDay(next.Start) + " uyanır"
	}
	return false, "Dönem dışı: takvimde sıradaki dönem yok, bot uyur"
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
