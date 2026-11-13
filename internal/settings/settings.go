// Package settings botun açma / kapama ayarlarını deploy'lar arasında saklar.
//
// Render'ın ücretsiz planında disk her deploy'da silinir. Ayarlar bu yüzden
// Telegram sohbetinde, bot tarafından sabitlenmiş küçük bir mesajda tutulur:
// açılışta getChat ile okunur, her değişiklikte aynı mesaj düzenlenir.
package settings

import (
	"strings"
	"sync"

	"notbot/internal/notify"
)

// Settings kullanıcının menüden değiştirdiği ayarlardır.
type Settings struct {
	Paused          bool
	CourseSelection bool
	Reminders       bool
}

const (
	header      = "⚙️ Bot ayarları"
	gradeLabel  = "Not taraması"
	courseLabel = "Ders seçme takibi"
	remindLabel = "Ders uyarıları"
	footer      = "Bot ayarlarını bu sabit mesajda saklar; deploy sonrası buradan okur. Silersen varsayılanlara döner."
	onMark      = "✅"
	offMark     = "▫️"
)

// Encode ayarları hem okunur hem de geri çözülebilir metne çevirir.
func Encode(s Settings) string {
	line := func(on bool, label string) string {
		if on {
			return onMark + " " + label
		}
		return offMark + " " + label
	}
	return strings.Join([]string{
		header,
		line(!s.Paused, gradeLabel),
		line(s.CourseSelection, courseLabel),
		line(s.Reminders, remindLabel),
		"",
		footer,
	}, "\n")
}

// Decode Encode'un ürettiği metni çözer; metin botun ayar mesajı değilse ok=false.
func Decode(text string) (s Settings, ok bool) {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != header {
		return Settings{}, false
	}
	found := map[string]bool{}
	for _, raw := range lines[1:] {
		line := strings.TrimSpace(raw)
		on := strings.HasPrefix(line, onMark)
		if !on && !strings.HasPrefix(line, offMark) {
			continue
		}
		switch {
		case strings.HasSuffix(line, gradeLabel):
			s.Paused, found[gradeLabel] = !on, true
		case strings.HasSuffix(line, courseLabel):
			s.CourseSelection, found[courseLabel] = on, true
		case strings.HasSuffix(line, remindLabel):
			s.Reminders, found[remindLabel] = on, true
		}
	}
	return s, len(found) == 3
}

// Store ayar mesajını okur ve günceller.
type Store struct {
	Token  string
	ChatID string

	mu        sync.Mutex
	messageID int
}

// Load sabit mesajdaki ayarları okur. Sabit mesaj yoksa ya da botun ayar mesajı
// değilse ok=false döner; çağıran o zaman varsayılanları kullanır.
func (st *Store) Load() (Settings, bool, error) {
	id, text, err := notify.PinnedMessage(st.Token, st.ChatID)
	if err != nil {
		return Settings{}, false, err
	}
	s, ok := Decode(text)
	if ok {
		st.mu.Lock()
		st.messageID = id
		st.mu.Unlock()
	}
	return s, ok, nil
}

// Save ayar mesajını günceller; mesaj yoksa ya da düzenlenemiyorsa yenisini gönderip sabitler.
func (st *Store) Save(s Settings) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	text := Encode(s)
	if st.messageID != 0 {
		err := notify.EditPlain(st.Token, st.ChatID, st.messageID, text)
		if err == nil || !notify.IsRejected(err) {
			// Başarılı ya da Telegram'a ulaşılamadı: ikinci bir ayar mesajı açma.
			return err
		}
	}
	id, err := notify.SendPinned(st.Token, st.ChatID, text)
	if id != 0 {
		st.messageID = id
	}
	return err
}
