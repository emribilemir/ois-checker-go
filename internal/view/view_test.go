package view

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"notbot/internal/dersecme"
	"notbot/internal/diff"
	"notbot/internal/exams"
	"notbot/internal/notify"
	"notbot/internal/schedule"
	"notbot/internal/scraper"
)

var (
	istanbul = time.FixedZone("Istanbul", 3*3600)
	now      = time.Date(2026, 11, 13, 14, 5, 0, 0, istanbul) // Cuma
	tagRE    = regexp.MustCompile(`</?([a-z0-9-]+)[^>]*>`)
	// Telegram sendMessage HTML'inin kabul ettiği etiketler.
	plainTags = map[string]bool{"b": true, "i": true, "u": true, "s": true, "a": true, "code": true, "pre": true, "blockquote": true, "tg-spoiler": true}
	// Zengin mesajda kullandığımız etiketler (hepsi Bot API rich HTML listesinde).
	richTags = map[string]bool{"p": true, "br": true, "b": true, "i": true, "s": true, "a": true,
		"table": true, "tr": true, "th": true, "td": true, "details": true, "summary": true, "ul": true, "li": true, "input": true,
		"footer": true, "blockquote": true}
	voidTags = map[string]bool{"br": true, "input": true}
)

func init() { Location = istanbul }

// checkMessage verifies both variants only use supported tags and are balanced.
func checkMessage(t *testing.T, name string, m notify.Message) {
	t.Helper()
	if m.Plain == "" {
		t.Fatalf("%s: plain fallback is empty", name)
	}
	for variant, body := range map[string]string{"plain": m.Plain, "rich": m.Rich} {
		allowed := plainTags
		if variant == "rich" {
			allowed = richTags
		}
		var stack []string
		for _, match := range tagRE.FindAllStringSubmatch(body, -1) {
			tag := match[1]
			if !allowed[tag] {
				t.Fatalf("%s %s uses unsupported <%s>: %s", name, variant, tag, body)
			}
			switch {
			case voidTags[tag]:
			case strings.HasPrefix(match[0], "</"):
				if len(stack) == 0 || stack[len(stack)-1] != tag {
					t.Fatalf("%s %s has unbalanced </%s>: %s", name, variant, tag, body)
				}
				stack = stack[:len(stack)-1]
			default:
				stack = append(stack, tag)
			}
		}
		if len(stack) != 0 {
			t.Fatalf("%s %s leaves <%v> open: %s", name, variant, stack, body)
		}
	}
	if strings.Contains(m.Plain, "<br") {
		t.Fatalf("%s plain uses <br>, which sendMessage rejects", name)
	}
}

func contains(t *testing.T, name, body string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Fatalf("%s missing %q:\n%s", name, want, body)
		}
	}
}

func TestMenuAndSettingsShowState(t *testing.T) {
	s := Status{Paused: false, CourseSelection: true, Reminders: false, Interval: 5 * time.Minute, LastCheck: now.Add(-2 * time.Minute), Now: now}
	menu := Menu(s)
	checkMessage(t, "menu", menu)
	contains(t, "menu rich", menu.Rich, `<input type="checkbox" checked>Not taraması (5 dk'da bir)`, `<input type="checkbox">Dersten 15 dk önce uyarı`, "Son kontrol 14:03 (2 dk önce)")
	contains(t, "menu plain", menu.Plain, "✅ Ders seçme takibi", "▫️ Dersten 15 dk önce uyarı")
	if len(menu.Keyboard) != 2 || len(menu.Keyboard[0]) != 2 {
		t.Fatalf("main menu must be a 2x2 grid: %#v", menu.Keyboard)
	}

	settings := Settings(Status{Paused: true, Reminders: true, Interval: time.Minute, LastError: "ocr_fail <x>", LastCheck: now, Now: now})
	checkMessage(t, "settings", settings)
	contains(t, "settings", settings.Rich, "ocr_fail &lt;x&gt;", "başarısız")
	var data []string
	for _, row := range settings.Keyboard {
		for _, b := range row {
			data = append(data, b.Data)
		}
	}
	contains(t, "settings keyboard", strings.Join(data, ","), "cmd_resume", "cmd_ders_secme_on", "cmd_reminders_off", "cmd_stats", "cmd_restart", "cmd_menu")
}

func TestGradesListAndWeights(t *testing.T) {
	m := Grades([]scraper.Course{
		{Code: "BLM305", Name: "Veritabanı <Yönetimi>", LetterGrade: "AA", SuccessScore: "92", Components: []scraper.Component{{Name: "Ödev", Weight: "(%20)", Score: "85"}, {Name: "Final", Score: "95"}}},
		{Code: "BLM301", Name: "Staj"},
	}, "OİS'e giriş yapıldı", now)
	checkMessage(t, "grades", m)
	contains(t, "grades rich", m.Rich, "<p><b>Veritabanı &lt;Yönetimi&gt;</b></p>", "AA · Başarı puanı 92", "<td>Ödev</td><td>%20</td><td><b>85</b></td>", "<td>Final</td><td>—</td>")
	contains(t, "grades plain", m.Plain, "Ödev <i>(%20)</i>  <b>85</b>", "Final  <b>95</b>")
	if strings.Contains(m.Rich+m.Plain, "((%20))") || strings.Contains(m.Rich+m.Plain, "(%20)%") {
		t.Fatalf("weight double-wrapped: %s", m.Plain)
	}
}

func TestGradeChangeAlert(t *testing.T) {
	m := GradeChanges([]diff.Change{
		{Type: "new_score", CourseCode: "SEC101", CourseName: "Siber & Güvenlik", Component: "Ödev", Weight: "(%20)", NewScore: "85.00"},
		{Type: "score_change", CourseCode: "SEC101", CourseName: "Siber & Güvenlik", Component: "Vize", Weight: "%40", OldScore: "70", NewScore: "75"},
	})
	checkMessage(t, "grade change", m)
	contains(t, "grade change rich", m.Rich, "🎉 Yeni notlar", "Siber &amp; Güvenlik", "<b>85.00</b> 🆕", "<s>70</s> → <b>75</b>")
	contains(t, "grade change plain", m.Plain, "Ödev <i>(%20)</i>  <b>85.00</b>", "<s>70</s> → <b>75</b>")
	if m.Silent || len(m.Keyboard) != 1 || m.Keyboard[0][0].Data != "cmd_grades_new" {
		t.Fatalf("grade alert must ring and offer the full list as a new message: %#v", m)
	}
	if one := GradeChanges([]diff.Change{{Type: "new_score", CourseCode: "A", CourseName: "A", Component: "Final", NewScore: "90"}}); !strings.Contains(one.Rich, "🎉 Yeni not<") {
		t.Fatalf("single new score title: %s", one.Rich)
	}
}

func TestWeekOpensTodayAndEscapes(t *testing.T) {
	m := Week([]schedule.Entry{
		{Day: time.Monday, Start: "09:00", End: "10:00", Name: "A&B <Lab>", Location: "Online", Online: true, Instructor: "Dr. A & B"},
		{Day: time.Friday, Start: "15:00", End: "16:00", Name: "Matematik", Location: "B3-09"},
	}, now)
	checkMessage(t, "week", m)
	contains(t, "week rich", m.Rich, "<details><summary>Pazartesi · 1 ders</summary>", "<details open><summary>Bugün · Cuma · 1 ders</summary>", "A&amp;B &lt;Lab&gt;", "🌐 Online", "Dr. A &amp; B", "Ders yok: Salı, Çarşamba, Perşembe, Cumartesi, Pazar")
	contains(t, "week plain", m.Plain, "<b>09:00–10:00</b>  A&amp;B &lt;Lab&gt;", "<i>🌐 Online · Dr. A &amp; B</i>", "<b>Bugün · Cuma</b>")

	// Cuma 18:00: bugünün tek dersi 13:00'te bitti, sıradaki dersli gün Pazartesi açılır.
	evening := Week([]schedule.Entry{
		{Day: time.Monday, Start: "09:00", End: "10:00", Name: "X"},
		{Day: time.Friday, Start: "12:00", End: "13:00", Name: "Y"},
	}, time.Date(2026, 11, 13, 18, 0, 0, 0, istanbul))
	contains(t, "week evening", evening.Rich, "<details open><summary>Pazartesi · 1 ders</summary>", "<details><summary>Bugün · Cuma · 1 ders</summary>")
	// Pazar akşamı: Pazartesi yarındır.
	sunday := Week([]schedule.Entry{{Day: time.Monday, Start: "09:00", End: "10:00", Name: "X"}}, time.Date(2026, 11, 15, 20, 0, 0, 0, istanbul))
	contains(t, "week sunday", sunday.Rich, "<details open><summary>Yarın · Pazartesi · 1 ders</summary>")
	// Haftada tek dersli gün bugün ve bitti: yine bugün açılır.
	only := Week([]schedule.Entry{{Day: time.Friday, Start: "09:00", End: "10:00", Name: "X"}}, time.Date(2026, 11, 13, 18, 0, 0, 0, istanbul))
	contains(t, "week only today", only.Rich, "<details open><summary>Bugün · Cuma")

	saturday := Week([]schedule.Entry{{Day: time.Monday, Start: "09:00", End: "10:00", Name: "X"}}, now.Add(24*time.Hour))
	contains(t, "week from saturday", saturday.Rich, "<details open><summary>Pazartesi")
	if empty := Week(nil, now); !strings.Contains(empty.Plain, "ders görünmüyor") {
		t.Fatalf("empty week: %s", empty.Plain)
	}
}

func TestReminder(t *testing.T) {
	m := Reminder(schedule.Entry{Name: "Veri Tabanı", Start: "09:00", End: "12:45", Online: true, Instructor: "Dr. A & B"}, 15*time.Minute)
	checkMessage(t, "reminder", m)
	contains(t, "reminder", m.Rich+m.Plain, "⏰ 15 dk sonra ders", "Veri Tabanı", "09:00–12:45 · 🌐 Online", "👤 Dr. A &amp; B")
}

func TestExamCalendar(t *testing.T) {
	day := func(d int) time.Time { return time.Date(2026, 11, d, 0, 0, 0, 0, time.UTC) }
	m := Exams([]exams.Exam{
		{Name: "Eski", Date: day(10), Start: "10:00"},
		{Name: "Robotik", Kind: "Vize", Date: day(14), Start: "10:00", End: "11:30", Place: "A<1>", Campus: "Kağıthane", Instructor: "Dr. X", Note: "Kitap kapalı"},
		{Name: "Ağlar", Date: day(14), Start: "13:00"},
		{Code: "BLM305", Date: day(20)},
	}, now)
	checkMessage(t, "exams", m)
	contains(t, "exams rich", m.Rich, "3 sınav · ilki yarın",
		`<tr><th colspan="3">14 Kasım Cumartesi · yarın</th></tr><tr><td>10:00–11:30</td><td><b>Robotik</b> · <i>Vize</i></td><td>A&lt;1&gt; · Kağıthane</td></tr><tr><td>13:00</td>`,
		`<tr><th colspan="3">20 Kasım Cuma · 7 gün sonra</th></tr><tr><td>—</td><td><b>BLM305</b></td><td>—</td></tr>`,
		"<li><b>Robotik</b>: Dr. X · Kitap kapalı</li>", "<summary>Geçmiş sınavlar (1)</summary>", "<li>10 Kas Sal · Eski</li>")
	contains(t, "exams plain", m.Plain, "<b>14 Kasım Cumartesi</b> · yarın", "<b>10:00–11:30</b>  <b>Robotik</b>", "<i>Vize · A&lt;1&gt;, Kağıthane</i>", "<b>13:00</b>  <b>Ağlar</b>", "Geçmiş 1 sınav gösterilmedi")
	contains(t, "exams plain without time", m.Plain, "<blockquote><b>BLM305</b>")
	if strings.Count(m.Plain, "14 Kasım") != 1 || strings.Count(m.Rich, "14 Kasım") != 1 {
		t.Fatalf("day header repeated: %s", m.Plain)
	}
	empty := Exams(nil, now)
	checkMessage(t, "empty exams", empty)
	contains(t, "empty exams", empty.Plain, "henüz sınav yok")
}

func TestCourseSelectionMessages(t *testing.T) {
	opened := CourseSelectionOpened("Ders Seçme", []dersecme.SelectedCourse{{Code: "SEC101", Name: "A & B"}}, "https://ois.example", now)
	checkMessage(t, "opened", opened)
	contains(t, "opened", opened.Rich, "🚨 Ders seçme açıldı", `<a href="https://ois.example">`, "SEC101 · A &amp; B", "Tespit: 13 Kasım 14:05")
	if opened.Silent {
		t.Fatal("course selection alert must ring")
	}

	changes := SelectionChanges([]dersecme.SelectionChange{{Type: "added", Course: dersecme.SelectedCourse{Code: "X1", Name: "Yeni"}}, {Type: "removed", Course: dersecme.SelectedCourse{Code: "X2", Name: "Eski"}}})
	checkMessage(t, "changes", changes)
	contains(t, "changes", changes.Plain, "➕ <b>Eklendi:</b> X1 · Yeni", "➖ <b>Silindi:</b> X2 · Eski", "ayırt edilemiyor")

	pool := PoolAvailable([]dersecme.PoolCourse{{Code: "SEC102", Name: "A&B <X>", Quota: 4}})
	checkMessage(t, "pool", pool)
	contains(t, "pool", pool.Rich, "<td>SEC102</td><td><b>A&amp;B &lt;X&gt;</b></td><td>4</td>", "online bilgisi")
}

func TestStatusAndErrorMessages(t *testing.T) {
	checkMessage(t, "stats", Stats(5*time.Minute, 42, now.Add(-26*time.Hour), now))
	contains(t, "stats", Stats(5*time.Minute, 42, now.Add(-26*time.Hour), now).Rich, "<b>42</b>", "1 gün 2 sa")
	checkMessage(t, "restart", Restarting())
	awakeView := Awake("Senin seçimin: 22 Aralık'a kadar uyanık", true)
	checkMessage(t, "awake", awakeView)
	contains(t, "awake", awakeView.Rich, "22 Aralık'a kadar", "/uyanik 20.12")
	checkMessage(t, "loading", Loading("📅 Ders Programı"))
	checkMessage(t, "notice", Notice("a < b"))

	failure := Failure("Sınav takvimi alınamadı", "sınav takvimi okunamadı\n- /yol: HTTP 404\n- <tablo>", "cmd_exams")
	checkMessage(t, "failure", failure)
	contains(t, "failure rich", failure.Rich, "<blockquote expandable>sınav takvimi okunamadı<br>- /yol: HTTP 404<br>- &lt;tablo&gt;</blockquote>")
	contains(t, "failure plain", failure.Plain, "okunamadı\n- /yol: HTTP 404")
	if failure.Keyboard[0][0].Data != "cmd_exams" {
		t.Fatalf("failure must offer a retry: %#v", failure.Keyboard)
	}

	unavailable := GradesUnavailable("ocr_fail", now)
	checkMessage(t, "unavailable", unavailable)
	contains(t, "unavailable", unavailable.Plain, "ocr_fail", "13 Kasım 14:05")
	checkMessage(t, "unavailable empty", GradesUnavailable("", time.Time{}))
}
