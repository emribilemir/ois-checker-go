// Package telemetry botun kişisel veri içermeyen çalışma bilgisini toplar.
//
// /status uç noktası bu bilgiyi JSON olarak gösterir; böylece bot sahibinin
// Render paneline girmeden giriş, CAPTCHA ve Telegram sorunları görülebilir.
// Buraya yalnız sayaçlar, süreler ve kodun ürettiği kısa hata metinleri yazılır:
// not, ders adı, kimlik bilgisi ya da OİS sayfa içeriği yazılmaz.
package telemetry

import (
	"regexp"
	"sync"
	"time"
	"unicode/utf8"
)

// Event zaman damgalı kısa bir olaydır.
type Event struct {
	At   time.Time `json:"at"`
	Kind string    `json:"kind"`
	Text string    `json:"text,omitempty"`
}

const (
	maxText   = 300
	maxEvents = 60
)

var (
	mu       sync.Mutex
	counters = map[string]int64{}
	values   = map[string]string{}
	lastByK  = map[string]Event{}
	events   []Event
	now      = time.Now
)

// Inc bir sayacı bir artırır.
func Inc(name string) {
	mu.Lock()
	counters[name]++
	mu.Unlock()
}

// Set bir göstergeyi (ör. son tur süresi) günceller.
func Set(name, value string) {
	mu.Lock()
	values[name] = truncate(value)
	mu.Unlock()
}

// Note bir olayı kaydeder: son olaylar listesine eklenir ve türünün son örneği olur.
func Note(kind, text string) {
	e := Event{At: now(), Kind: kind, Text: truncate(text)}
	mu.Lock()
	lastByK[kind] = e
	events = append(events, e)
	if len(events) > maxEvents {
		events = append([]Event(nil), events[len(events)-maxEvents:]...)
	}
	mu.Unlock()
}

// Snapshot o anki bilginin kopyasıdır.
func Snapshot() map[string]any {
	mu.Lock()
	defer mu.Unlock()
	c := make(map[string]int64, len(counters))
	for k, v := range counters {
		c[k] = v
	}
	v := make(map[string]string, len(values))
	for k, val := range values {
		v[k] = val
	}
	l := make(map[string]Event, len(lastByK))
	for k, e := range lastByK {
		l[k] = e
	}
	recent := make([]Event, len(events))
	for i := range events {
		recent[len(events)-1-i] = events[i] // en yeni başta
	}
	return map[string]any{"counters": c, "values": v, "last": l, "recent": recent}
}

// Reset testler içindir.
func Reset() {
	mu.Lock()
	counters, values, lastByK, events = map[string]int64{}, map[string]string{}, map[string]Event{}, nil
	mu.Unlock()
}

// longNumber öğrenci numarası, TC kimlik no gibi uzun sayılardır.
var longNumber = regexp.MustCompile(`\d{6,}`)

// Scrub uzun sayıları maskeler; /status herkese açık olduğu için her metin buradan geçer.
func Scrub(s string) string { return longNumber.ReplaceAllString(s, "<no>") }

func truncate(s string) string {
	s = Scrub(s)
	if len(s) <= maxText {
		return s
	}
	cut := maxText
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}
