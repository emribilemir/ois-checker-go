package main

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"log"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"

	"notbot/internal/diff"
	"notbot/internal/scraper"
	"notbot/internal/settings"
	"notbot/internal/telemetry"
	"notbot/internal/view"
)

// Render'ın ücretsiz diski her yeniden başlatmada silinir; not durumu dosyası da
// gider. Bot kapalıyken açıklanan bir not, açılıştaki ilk okumada "zaten vardı"
// sayılıp hiç bildirilmezdi. Bu yüzden bot tanıdığı notların kısa izlerini sabit
// ayar mesajında tutar ve açılışta onlarla karşılaştırır.

var (
	continuityMu   sync.Mutex
	knownMarks     []string  // sabit mesajdaki not izleri
	lastAlive      time.Time // sabit mesaja en son yazılan çalışma anı
	lastAliveSaved time.Time
	settingsStore  *settings.Store
)

const heartbeatEvery = 10 * time.Minute

// gradeItem tek bir yayınlanmış not öğesidir (bileşen puanı, harf notu ya da başarı puanı).
type gradeItem struct {
	course    scraper.Course
	component string
	weight    string
	score     string
}

func gradeItems(courses []scraper.Course) []gradeItem {
	var items []gradeItem
	for _, c := range courses {
		for _, comp := range c.Components {
			items = append(items, gradeItem{c, comp.Name, comp.Weight, comp.Score})
		}
		if c.LetterGrade != "" {
			items = append(items, gradeItem{course: c, component: "Harf notu", score: c.LetterGrade})
		}
		if c.SuccessScore != "" {
			items = append(items, gradeItem{course: c, component: "Başarı puanı", score: c.SuccessScore})
		}
	}
	return items
}

// markOf 6 haneli kısa izdir; adı ya da puanı geri vermez.
func markOf(it gradeItem) string {
	sum := sha1.Sum([]byte(it.course.Code + "\x1f" + it.component + "\x1f" + it.score))
	return hex.EncodeToString(sum[:3])
}

func marksOf(courses []scraper.Course) []string {
	seen := map[string]bool{}
	var marks []string
	for _, it := range gradeItems(courses) {
		if m := markOf(it); !seen[m] {
			seen[m] = true
			marks = append(marks, m)
		}
	}
	sort.Strings(marks)
	return marks
}

// missedChanges, sabit mesajdaki izlerde olmayan not öğelerini "yeni not" olarak döndürür.
// İz hiç yoksa (özelliğin ilk çalışması) hiçbir şeyi yeni saymaz.
func missedChanges(courses []scraper.Course, stored []string) []diff.Change {
	if len(stored) == 0 {
		return nil
	}
	known := map[string]bool{}
	for _, m := range stored {
		known[m] = true
	}
	var changes []diff.Change
	for _, it := range gradeItems(courses) {
		if known[markOf(it)] {
			continue
		}
		changes = append(changes, diff.Change{
			Type: "new_score", CourseCode: it.course.Code, CourseName: it.course.DisplayName(),
			Component: it.component, Weight: it.weight, NewScore: it.score,
		})
	}
	return changes
}

func storedMarks() []string {
	continuityMu.Lock()
	defer continuityMu.Unlock()
	return append([]string(nil), knownMarks...)
}

// rememberGrades, not izleri değiştiyse sabit mesaja yazar.
func rememberGrades(courses []scraper.Course) {
	marks := marksOf(courses)
	continuityMu.Lock()
	same := strings.Join(marks, " ") == strings.Join(knownMarks, " ")
	if !same {
		knownMarks = marks
	}
	continuityMu.Unlock()
	if !same {
		persistSettings()
	}
}

// heartbeat, en fazla heartbeatEvery'de bir son çalışma anını sabit mesaja yazar.
func heartbeat(now time.Time) {
	continuityMu.Lock()
	due := now.Sub(lastAliveSaved) >= heartbeatEvery
	if due {
		lastAlive, lastAliveSaved = now.UTC(), now
	}
	continuityMu.Unlock()
	if due {
		persistSettings()
	}
}

// noteRestart, açılışta önceki çalışmanın ne zaman sustuğunu /status'a yazar.
func noteRestart(previous, now time.Time) {
	if previous.IsZero() {
		telemetry.Note("restart", "önceki çalışma kaydı yok")
		return
	}
	gap := now.Sub(previous).Round(time.Minute)
	text := fmt.Sprintf("önceki çalışma %s'de sustu, %s kapalı kaldı", previous.In(view.Location).Format("02.01 15:04"), gap)
	telemetry.Note("restart", text)
	telemetry.Set("previous_downtime", gap.String())
	log.Printf("[açılış] %s", text)
}

func persistSettings() {
	if settingsStore == nil {
		return
	}
	if err := settingsStore.Save(currentSettings()); err != nil {
		log.Printf("ayarlar sabit mesaja yazılamadı: %v", err)
	}
}

// recoverPanic bir goroutine'deki hatanın süreci düşürmesini önler ve /status'a yazar.
// Doğrudan defer ile çağrılmalıdır.
func recoverPanic(where string) {
	r := recover()
	if r == nil {
		return
	}
	stack := string(debug.Stack())
	log.Printf("[panic] %s: %v\n%s", where, r, stack)
	telemetry.Inc("panic")
	telemetry.Note("panic", fmt.Sprintf("%s: %v @ %s", where, r, panicSite(stack)))
}

// panicSite yığındaki ilk proje satırını (dosya:satır) döndürür.
func panicSite(stack string) string {
	afterPanic := false
	for _, line := range strings.Split(stack, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "panic(") {
			afterPanic = true
			continue
		}
		if afterPanic && strings.Contains(line, ".go:") && !strings.Contains(line, "/runtime/") {
			if i := strings.LastIndex(line, "/"); i >= 0 {
				line = line[i+1:]
			}
			if i := strings.Index(line, " +0x"); i >= 0 {
				line = line[:i]
			}
			return line
		}
	}
	return "?"
}
