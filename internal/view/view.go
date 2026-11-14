// Package view botun bütün Telegram mesajlarını üretir.
//
// Başlıklar <h3>/<h4> değil, gövde fontunda kalın satırdır (<p><b>): Telegram
// başlık etiketlerini ayrı bir fontla çiziyor (Emir, 2026-11-13: beğenilmedi).
//
// Her mesajın iki hali vardır: Rich, Telegram'ın zengin mesaj HTML'i (başlık,
// tablo, açılır bölüm); Plain, zengin mesajı kabul etmeyen durumlar için sade
// Telegram HTML'i (kalın başlık + alıntı kartları). Rich içinde satır sonu <br>,
// Plain içinde "\n" kullanılır; Plain'de <p>, <p><b>, <table>, <br> geçmez.
package view

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	"notbot/internal/dersecme"
	"notbot/internal/diff"
	"notbot/internal/exams"
	"notbot/internal/notify"
	"notbot/internal/schedule"
	"notbot/internal/scraper"
)

// Location mesajlarda gösterilen saatlerin dilimidir; main Europe/Istanbul yapar.
var Location = time.Local

var (
	weekdays      = [...]string{"Pazar", "Pazartesi", "Salı", "Çarşamba", "Perşembe", "Cuma", "Cumartesi"}
	shortWeekdays = [...]string{"Paz", "Pzt", "Sal", "Çar", "Per", "Cum", "Cmt"}
	months        = [...]string{"", "Ocak", "Şubat", "Mart", "Nisan", "Mayıs", "Haziran", "Temmuz", "Ağustos", "Eylül", "Ekim", "Kasım", "Aralık"}
	shortMonths   = [...]string{"", "Oca", "Şub", "Mar", "Nis", "May", "Haz", "Tem", "Ağu", "Eyl", "Eki", "Kas", "Ara"}
)

// escaper yalnız Telegram'ın belgelediği karakterleri kaçırır (&lt; &gt; &amp; &quot;).
var escaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")

func esc(s string) string { return escaper.Replace(s) }

// ---- düğmeler ----

// MainMenu ana menü düğmeleridir.
func MainMenu() notify.Keyboard {
	return notify.Keyboard{
		{{Text: "📖 Notlar", Data: "cmd_grades"}, {Text: "📅 Program", Data: "cmd_schedule"}},
		{{Text: "📝 Sınavlar", Data: "cmd_exams"}, {Text: "⚙️ Ayarlar", Data: "cmd_settings"}},
	}
}

func backKeyboard(refresh string) notify.Keyboard {
	menu := notify.Button{Text: "◀︎ Menü", Data: "cmd_menu"}
	if refresh == "" {
		return notify.Keyboard{{menu}}
	}
	return notify.Keyboard{{{Text: "🔄 Yenile", Data: refresh}, menu}}
}

// ---- menü ve ayarlar ----

// Status menüde ve ayarlarda gösterilen bot durumudur.
type Status struct {
	Paused          bool
	CourseSelection bool
	Reminders       bool
	Interval        time.Duration
	LastCheck       time.Time
	LastError       string
	Now             time.Time
	AwakeNote       string // botun ne zamana kadar uyanık kalacağı
}

func (s Status) items() []struct {
	on    bool
	label string
} {
	return []struct {
		on    bool
		label string
	}{
		{!s.Paused, fmt.Sprintf("Not taraması (%s)", everyText(s.Interval))},
		{s.CourseSelection, "Ders seçme takibi"},
		{s.Reminders, "Dersten 15 dk önce uyarı"},
	}
}

func (s Status) lastCheckText() string {
	if s.LastCheck.IsZero() {
		return "Henüz kontrol yapılmadı"
	}
	text := "Son kontrol " + clock(s.LastCheck) + " (" + ago(s.LastCheck, s.Now) + ")"
	if s.LastError != "" {
		text += " başarısız"
	}
	return text
}

func statusMessage(title string, s Status, keyboard notify.Keyboard) notify.Message {
	var rich, plain strings.Builder
	rich.WriteString("<p><b>" + title + "</b></p><ul>")
	plain.WriteString("<b>" + title + "</b>\n")
	for _, item := range s.items() {
		checked, mark := "", "▫️"
		if item.on {
			checked, mark = " checked", "✅"
		}
		rich.WriteString(`<li><input type="checkbox"` + checked + `>` + esc(item.label) + "</li>")
		plain.WriteString(mark + " " + esc(item.label) + "\n")
	}
	rich.WriteString("</ul>")
	if s.LastError != "" {
		rich.WriteString("<blockquote>⚠️ " + esc(s.LastError) + "</blockquote>")
		plain.WriteString("<blockquote>⚠️ " + esc(s.LastError) + "</blockquote>\n")
	}
	if s.AwakeNote != "" {
		rich.WriteString("<p>" + esc(s.AwakeNote) + "</p>")
		plain.WriteString(esc(s.AwakeNote) + "\n")
	}
	rich.WriteString("<footer>" + esc(s.lastCheckText()) + "</footer>")
	plain.WriteString("<i>" + esc(s.lastCheckText()) + "</i>")
	return notify.Message{Rich: rich.String(), Plain: plain.String(), Keyboard: keyboard}
}

// Menu ana menüdür.
func Menu(s Status) notify.Message {
	return statusMessage("🎓 OİS Bot", s, MainMenu())
}

// Started bot her açıldığında sessizce gönderilir.
func Started(s Status) notify.Message {
	m := statusMessage("🤖 Bot başladı", s, MainMenu())
	m.Silent = true
	return m
}

// Settings takip ve uyarı ayarlarıdır; düğmeler durumu tersine çevirir.
func Settings(s Status) notify.Message {
	pause := notify.Button{Text: "⏸ Not taramasını durdur", Data: "cmd_pause"}
	if s.Paused {
		pause = notify.Button{Text: "▶️ Not taramasını başlat", Data: "cmd_resume"}
	}
	selection := notify.Button{Text: "📋 Ders seçme takibini aç", Data: "cmd_ders_secme_on"}
	if s.CourseSelection {
		selection = notify.Button{Text: "📋 Ders seçme takibini kapat", Data: "cmd_ders_secme_off"}
	}
	reminders := notify.Button{Text: "🔔 Ders uyarılarını aç", Data: "cmd_reminders_on"}
	if s.Reminders {
		reminders = notify.Button{Text: "🔕 Ders uyarılarını kapat", Data: "cmd_reminders_off"}
	}
	return statusMessage("⚙️ Ayarlar", s, notify.Keyboard{
		{pause}, {selection}, {reminders},
		{{Text: "📊 Sistem", Data: "cmd_stats"}, {Text: "🔄 Yeniden başlat", Data: "cmd_restart"}},
		{{Text: "◀︎ Menü", Data: "cmd_menu"}},
	})
}

// Stats çalışma zamanı bilgisidir.
func Stats(interval time.Duration, checks int64, startedAt, now time.Time) notify.Message {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	rows := [][2]string{
		{"Kontrol aralığı", everyText(interval)},
		{"Toplam kontrol", fmt.Sprintf("%d", checks)},
		{"Çalışma süresi", durationText(now.Sub(startedAt))},
		{"Bellek (Alloc)", fmt.Sprintf("%.1f MB", float64(m.Alloc)/1024/1024)},
		{"Ayrılan (Sys)", fmt.Sprintf("%.1f MB", float64(m.Sys)/1024/1024)},
		{"Goroutine", fmt.Sprintf("%d", runtime.NumGoroutine())},
		{"GC döngüsü", fmt.Sprintf("%d", m.NumGC)},
	}
	var rich, plain strings.Builder
	rich.WriteString("<p><b>📊 Sistem Durumu</b></p><table compact striped>")
	plain.WriteString("<b>📊 Sistem Durumu</b>\n<blockquote>")
	for i, row := range rows {
		rich.WriteString("<tr><td>" + row[0] + "</td><td><b>" + row[1] + "</b></td></tr>")
		if i > 0 {
			plain.WriteString("\n")
		}
		plain.WriteString(row[0] + ": <b>" + row[1] + "</b>")
	}
	rich.WriteString("</table>")
	plain.WriteString("</blockquote>")
	return notify.Message{Rich: rich.String(), Plain: plain.String(), Keyboard: notify.Keyboard{{{Text: "◀︎ Ayarlar", Data: "cmd_settings"}}}}
}

// Restarting yeniden başlatma onayıdır.
func Restarting() notify.Message {
	return notify.Message{
		Rich:  "<p><b>🔄 Yeniden başlatılıyor</b></p><p>Bot birkaç saniye içinde geri gelir.</p>",
		Plain: "<b>🔄 Yeniden başlatılıyor</b>\n<i>Bot birkaç saniye içinde geri gelir.</i>",
	}
}

// Loading OİS'ten veri beklenirken düğmenin bulunduğu mesajda gösterilir.
func Loading(title string) notify.Message {
	return notify.Message{
		Rich:  "<p><b>" + title + "</b></p><p>⏳ OİS'ten alınıyor…</p>",
		Plain: "<b>" + title + "</b>\n<i>⏳ OİS'ten alınıyor…</i>",
	}
}

// Failure bir isteğin başarısız olduğunu söyler; uzun hata açılır alıntıdadır.
func Failure(title, reason, retry string) notify.Message {
	lines := strings.Split(strings.TrimSpace(reason), "\n")
	for i := range lines {
		lines[i] = esc(lines[i])
	}
	return notify.Message{
		Rich:     "<p><b>⚠️ " + title + "</b></p><blockquote expandable>" + strings.Join(lines, "<br>") + "</blockquote><footer>Biraz sonra tekrar deneyebilirsin.</footer>",
		Plain:    "<b>⚠️ " + title + "</b>\n<blockquote expandable>" + strings.Join(lines, "\n") + "</blockquote>\n<i>Biraz sonra tekrar deneyebilirsin.</i>",
		Keyboard: backKeyboard(retry),
	}
}

// Notice kısa, düğmesiz bir bilgi mesajıdır.
func Notice(text string) notify.Message {
	return notify.Message{Rich: "<p>" + esc(text) + "</p>", Plain: esc(text)}
}

// ---- notlar ----

func courseSummary(c scraper.Course) string {
	var parts []string
	if c.LetterGrade != "" {
		parts = append(parts, c.LetterGrade)
	}
	if c.SuccessScore != "" {
		parts = append(parts, "Başarı puanı "+c.SuccessScore)
	}
	return strings.Join(parts, " · ")
}

// Grades not listesidir. subtitle boş olabilir.
func Grades(courses []scraper.Course, subtitle string, now time.Time) notify.Message {
	var rich, plain strings.Builder
	rich.WriteString("<p><b>📖 Notlar</b></p>")
	plain.WriteString("<b>📖 Notlar</b>")
	if subtitle != "" {
		rich.WriteString("<p>" + esc(subtitle) + "</p>")
		plain.WriteString("\n<i>" + esc(subtitle) + "</i>")
	}
	plain.WriteString("\n")
	for _, c := range courses {
		summary := courseSummary(c)
		rich.WriteString("<p><b>" + esc(c.Name) + "</b></p>")
		plain.WriteString("<blockquote><b>" + esc(c.Name) + "</b>")
		if summary != "" {
			rich.WriteString("<p>" + esc(summary) + "</p>")
			plain.WriteString(" · " + esc(summary))
		}
		if len(c.Components) > 0 {
			rich.WriteString("<table compact striped><tr><th>Bileşen</th><th>Etki</th><th>Puan</th></tr>")
			for _, comp := range c.Components {
				weight := scraper.NormalizeWeight(comp.Weight)
				rich.WriteString("<tr><td>" + esc(comp.Name) + "</td><td>" + esc(dash(weight)) + "</td><td><b>" + esc(comp.Score) + "</b></td></tr>")
				plain.WriteString("\n" + esc(comp.Name))
				if weight != "" {
					plain.WriteString(" <i>(" + esc(weight) + ")</i>")
				}
				plain.WriteString("  <b>" + esc(comp.Score) + "</b>")
			}
			rich.WriteString("</table>")
		}
		plain.WriteString("</blockquote>")
	}
	rich.WriteString("<footer>OİS · " + esc(stamp(now)) + "</footer>")
	return notify.Message{Rich: rich.String(), Plain: strings.TrimRight(plain.String(), "\n"), Keyboard: backKeyboard("")}
}

// GradesUnavailable not henüz alınamadığında gösterilir.
func GradesUnavailable(reason string, at time.Time) notify.Message {
	if reason == "" {
		return notify.Message{
			Rich:     "<p><b>⏳ Notlar hazırlanıyor</b></p><p>Henüz başarılı bir not kontrolü tamamlanmadı. Bot otomatik olarak yeniden deniyor.</p>",
			Plain:    "<b>⏳ Notlar hazırlanıyor</b>\n<i>Henüz başarılı bir not kontrolü tamamlanmadı. Bot otomatik olarak yeniden deniyor.</i>",
			Keyboard: backKeyboard("cmd_grades"),
		}
	}
	return notify.Message{
		Rich:     "<p><b>⚠️ Notlar henüz alınamadı</b></p><blockquote>" + esc(reason) + "</blockquote><footer>Son deneme " + esc(stamp(at)) + " · Bot otomatik olarak yeniden deniyor.</footer>",
		Plain:    "<b>⚠️ Notlar henüz alınamadı</b>\n<blockquote>" + esc(reason) + "</blockquote>\n<i>Son deneme " + esc(stamp(at)) + " · Bot otomatik olarak yeniden deniyor.</i>",
		Keyboard: backKeyboard("cmd_grades"),
	}
}

// GradeChanges yeni ya da değişen notların bildirimidir (sesli).
func GradeChanges(changes []diff.Change) notify.Message {
	type group struct {
		name    string
		changes []diff.Change
	}
	var groups []*group
	byCode := map[string]*group{}
	newScores, updated := 0, 0
	for _, ch := range changes {
		g, ok := byCode[ch.CourseCode]
		if !ok {
			g = &group{name: ch.CourseName}
			byCode[ch.CourseCode] = g
			groups = append(groups, g)
		}
		g.changes = append(g.changes, ch)
		switch ch.Type {
		case "new_score":
			newScores++
		case "score_change":
			updated++
		}
	}
	title := "🆕 Yeni ders eklendi"
	switch {
	case newScores == 1 && updated == 0:
		title = "🎉 Yeni not"
	case newScores > 0:
		title = "🎉 Yeni notlar"
	case updated > 0:
		title = "✏️ Not güncellendi"
	}

	var rich, plain strings.Builder
	rich.WriteString("<p><b>" + title + "</b></p>")
	plain.WriteString("<b>" + title + "</b>\n")
	for _, g := range groups {
		rich.WriteString("<p><b>" + esc(g.name) + "</b></p>")
		plain.WriteString("<blockquote><b>" + esc(g.name) + "</b>")
		var rows strings.Builder
		for _, ch := range g.changes {
			weight := scraper.NormalizeWeight(ch.Weight)
			var richScore, plainScore string
			switch ch.Type {
			case "new_score":
				richScore = "<mark><b>" + esc(ch.NewScore) + "</b></mark>"
				plainScore = "<b>" + esc(ch.NewScore) + "</b>"
			case "score_change":
				richScore = "<s>" + esc(ch.OldScore) + "</s> → <mark><b>" + esc(ch.NewScore) + "</b></mark>"
				plainScore = "<s>" + esc(ch.OldScore) + "</s> → <b>" + esc(ch.NewScore) + "</b>"
			default:
				rich.WriteString("<p>🆕 Ders listeye eklendi</p>")
				plain.WriteString("\n🆕 Ders listeye eklendi")
				continue
			}
			rows.WriteString("<tr><td>" + esc(ch.Component) + "</td><td>" + esc(dash(weight)) + "</td><td>" + richScore + "</td></tr>")
			plain.WriteString("\n" + esc(ch.Component))
			if weight != "" {
				plain.WriteString(" <i>(" + esc(weight) + ")</i>")
			}
			plain.WriteString("  " + plainScore)
		}
		if rows.Len() > 0 {
			rich.WriteString("<table compact><tr><th>Bileşen</th><th>Etki</th><th>Puan</th></tr>" + rows.String() + "</table>")
		}
		plain.WriteString("</blockquote>")
	}
	return notify.Message{Rich: rich.String(), Plain: strings.TrimRight(plain.String(), "\n"), Keyboard: notify.Keyboard{{{Text: "📖 Tüm notlar", Data: "cmd_grades_new"}}}}
}

// ---- ders programı ----

// Week haftalık ders programıdır. Bugünden itibaren dersi olan ilk gün açık gelir.
func Week(entries []schedule.Entry, now time.Time) notify.Message {
	now = now.In(Location)
	order := []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday, time.Sunday}
	byDay := map[time.Weekday][]schedule.Entry{}
	for _, e := range entries {
		byDay[e.Day] = append(byDay[e.Day], e)
	}
	if len(entries) == 0 {
		return notify.Message{
			Rich:     "<p><b>📅 Ders Programı</b></p><p>Bu dönemde programda ders görünmüyor.</p>",
			Plain:    "<b>📅 Ders Programı</b>\n<i>Bu dönemde programda ders görünmüyor.</i>",
			Keyboard: backKeyboard("cmd_schedule"),
		}
	}
	// Açık gelen gün: bugün dersi varsa ve son dersi bitmediyse bugün, yoksa
	// sıradaki dersli gün. Mesaj basıldığı anın durumunu gösterir; sonra
	// kendiliğinden değişmez, 🔄 Yenile ile güncellenir.
	clockNow := now.Format("15:04")
	openDay := time.Weekday(-1)
	for i := 0; i < 7; i++ {
		day := time.Weekday((int(now.Weekday()) + i) % 7)
		if len(byDay[day]) == 0 || (i == 0 && lastEnd(byDay[day]) <= clockNow) {
			continue
		}
		openDay = day
		break
	}
	if openDay < 0 && len(byDay[now.Weekday()]) > 0 {
		// Haftada tek dersli gün bugün ve dersler bitti: yine de bugünü aç.
		openDay = now.Weekday()
	}
	tomorrow := time.Weekday((int(now.Weekday()) + 1) % 7)

	var rich, plain strings.Builder
	var empty []string
	rich.WriteString("<p><b>📅 Ders Programı</b></p>")
	plain.WriteString("<b>📅 Ders Programı</b>\n")
	for _, day := range order {
		list := byDay[day]
		if len(list) == 0 {
			empty = append(empty, weekdays[day])
			continue
		}
		label := weekdays[day]
		switch day {
		case now.Weekday():
			label = "Bugün · " + label
		case tomorrow:
			label = "Yarın · " + label
		}
		open := ""
		if day == openDay {
			open = " open"
		}
		rich.WriteString(fmt.Sprintf("<details%s><summary>%s · %d ders</summary><table compact striped>", open, esc(label), len(list)))
		plain.WriteString("\n<b>" + esc(label) + "</b>\n<blockquote>")
		for i, e := range list {
			place := e.Location
			if e.Online {
				place = "🌐 Online"
			}
			course := "<b>" + esc(e.Name) + "</b>"
			if e.Instructor != "" {
				course += " · <i>" + esc(e.Instructor) + "</i>"
			}
			rich.WriteString("<tr><td>" + esc(e.Start) + "–" + esc(e.End) + "</td><td>" + course + "</td><td>" + esc(dash(place)) + "</td></tr>")
			if i > 0 {
				plain.WriteString("\n")
			}
			plain.WriteString("<b>" + esc(e.Start) + "–" + esc(e.End) + "</b>  " + esc(e.Name) + "\n<i>" + esc(joinNonEmpty(" · ", place, e.Instructor)) + "</i>")
		}
		rich.WriteString("</table></details>")
		plain.WriteString("</blockquote>")
	}
	if len(empty) > 0 {
		rich.WriteString("<footer>Ders yok: " + esc(strings.Join(empty, ", ")) + "</footer>")
		plain.WriteString("\n<i>Ders yok: " + esc(strings.Join(empty, ", ")) + "</i>")
	}
	return notify.Message{Rich: rich.String(), Plain: plain.String(), Keyboard: backKeyboard("cmd_schedule")}
}

// lastEnd bir günün en geç biten dersinin saatidir ("15:04" biçimi, sözlük sırası saat sırasıdır).
func lastEnd(entries []schedule.Entry) string {
	last := ""
	for _, e := range entries {
		if e.End > last {
			last = e.End
		}
	}
	return last
}

// Reminder dersten önce gelen uyarıdır (sesli).
func Reminder(e schedule.Entry, lead time.Duration) notify.Message {
	title := fmt.Sprintf("⏰ %d dk sonra ders", int(lead.Minutes()))
	place := e.Location
	if e.Online {
		place = "🌐 Online"
	}
	when := esc(e.Start) + "–" + esc(e.End)
	if place != "" {
		when += " · " + esc(place)
	}
	rich := "<p><b>" + title + "</b></p><p><b>" + esc(e.Name) + "</b><br>" + when
	plain := "<b>" + title + "</b>\n<blockquote><b>" + esc(e.Name) + "</b>\n" + when
	if e.Instructor != "" {
		rich += "<br><i>👤 " + esc(e.Instructor) + "</i>"
		plain += "\n<i>👤 " + esc(e.Instructor) + "</i>"
	}
	return notify.Message{Rich: rich + "</p>", Plain: plain + "</blockquote>"}
}

// ---- sınav takvimi ----

// Exams sınav takvimidir: bugünden sonraki sınavlar tabloda, geçmişler açılır bölümde.
func Exams(list []exams.Exam, now time.Time) notify.Message {
	now = now.In(Location)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	var upcoming, past []exams.Exam
	for _, e := range list {
		if e.Date.Before(today) {
			past = append(past, e)
		} else {
			upcoming = append(upcoming, e)
		}
	}

	var rich, plain strings.Builder
	rich.WriteString("<p><b>📝 Sınav Takvimi</b></p>")
	plain.WriteString("<b>📝 Sınav Takvimi</b>\n")
	switch {
	case len(upcoming) > 0:
		sub := fmt.Sprintf("%d sınav · ilki %s", len(upcoming), countdown(upcoming[0].Date, today, false))
		rich.WriteString("<p>" + esc(sub) + "</p>")
		plain.WriteString("<i>" + esc(sub) + "</i>\n")
	case len(list) == 0:
		rich.WriteString("<p>OİS'teki sınav takviminde henüz sınav yok.</p>")
		plain.WriteString("<i>OİS'teki sınav takviminde henüz sınav yok.</i>\n")
	default:
		rich.WriteString("<p>OİS'teki takvimde bugünden sonra sınav yok.</p>")
		plain.WriteString("<i>OİS'teki takvimde bugünden sonra sınav yok.</i>\n")
	}

	if len(upcoming) > 0 {
		rich.WriteString("<table compact>")
		var details []string
		var lastDate time.Time
		for _, e := range upcoming {
			name := examName(e)
			course := "<b>" + esc(name) + "</b>"
			if e.Kind != "" {
				course += " · <i>" + esc(e.Kind) + "</i>"
			}
			if !e.Date.Equal(lastDate) {
				rich.WriteString(`<tr><th colspan="3">` + esc(longDate(e.Date)) + " · " + esc(countdown(e.Date, today, false)) + "</th></tr>")
			}
			rich.WriteString("<tr><td>" + esc(dash(examClock(e))) + "</td><td>" + course + "</td><td>" + esc(dash(joinNonEmpty(" · ", e.Place, e.Campus))) + "</td></tr>")
			if extra := joinNonEmpty(" · ", e.Instructor, e.Note); extra != "" {
				details = append(details, "<li><b>"+esc(name)+"</b>: "+esc(extra)+"</li>")
			}

			if !e.Date.Equal(lastDate) {
				if !lastDate.IsZero() {
					plain.WriteString("</blockquote>")
				}
				lastDate = e.Date
				plain.WriteString("\n<b>" + esc(longDate(e.Date)) + "</b> · " + esc(countdown(e.Date, today, true)) + "\n<blockquote>")
			} else {
				plain.WriteString("\n")
			}
			if clock := examClock(e); clock != "" {
				plain.WriteString("<b>" + esc(clock) + "</b>  ")
			}
			plain.WriteString("<b>" + esc(name) + "</b>")
			if info := joinNonEmpty(" · ", e.Kind, joinNonEmpty(", ", e.Place, e.Campus)); info != "" {
				plain.WriteString("\n<i>" + esc(info) + "</i>")
			}
			if e.Instructor != "" {
				plain.WriteString("\n<i>👤 " + esc(e.Instructor) + "</i>")
			}
			if e.Note != "" {
				plain.WriteString("\nℹ️ " + esc(e.Note))
			}
		}
		rich.WriteString("</table>")
		plain.WriteString("</blockquote>")
		if len(details) > 0 {
			rich.WriteString("<details><summary>Hocalar ve açıklamalar</summary><ul>" + strings.Join(details, "") + "</ul></details>")
		}
	}
	if len(past) > 0 {
		rich.WriteString(fmt.Sprintf("<details><summary>Geçmiş sınavlar (%d)</summary><ul>", len(past)))
		for _, e := range past {
			rich.WriteString("<li>" + esc(shortDate(e.Date)) + " · " + esc(examName(e)) + "</li>")
		}
		rich.WriteString("</ul></details>")
		plain.WriteString(fmt.Sprintf("\n<i>Geçmiş %d sınav gösterilmedi.</i>", len(past)))
	}
	rich.WriteString("<footer>OİS · " + esc(stamp(now)) + "</footer>")
	return notify.Message{Rich: rich.String(), Plain: strings.TrimRight(plain.String(), "\n"), Keyboard: backKeyboard("cmd_exams")}
}

func examName(e exams.Exam) string {
	if e.Name != "" {
		return e.Name
	}
	return e.Code
}

func examClock(e exams.Exam) string {
	if e.Start == "" {
		return ""
	}
	if e.End != "" {
		return e.Start + "–" + e.End
	}
	return e.Start
}

// ---- ders seçme ----

// CourseSelectionOpened ders seçme açıldığında gelir (sesli).
func CourseSelectionOpened(signal string, selected []dersecme.SelectedCourse, oisURL string, now time.Time) notify.Message {
	var rich, plain strings.Builder
	rich.WriteString("<p><b>🚨 Ders seçme açıldı</b></p><p>OİS'te “" + esc(signal) + "” görüldü. <a href=\"" + esc(oisURL) + "\">OİS'e git</a></p>")
	plain.WriteString("<b>🚨 Ders seçme açıldı</b>\nOİS'te “" + esc(signal) + "” görüldü. <a href=\"" + esc(oisURL) + "\">OİS'e git</a>\n")
	if len(selected) > 0 {
		rich.WriteString(fmt.Sprintf("<p><b>Şu an seçili %d ders</b></p><ul>", len(selected)))
		plain.WriteString(fmt.Sprintf("\n<b>Şu an seçili %d ders</b>\n<blockquote>", len(selected)))
		for i, c := range selected {
			rich.WriteString("<li>" + esc(c.Code) + " · " + esc(c.Name) + "</li>")
			if i > 0 {
				plain.WriteString("\n")
			}
			plain.WriteString(esc(c.Code) + " · " + esc(c.Name))
		}
		rich.WriteString("</ul>")
		plain.WriteString("</blockquote>\n")
	}
	rich.WriteString("<footer>Tespit: " + esc(stamp(now)) + "</footer>")
	plain.WriteString("<i>Tespit: " + esc(stamp(now)) + "</i>")
	return notify.Message{Rich: rich.String(), Plain: plain.String()}
}

const attributionNote = "Bu değişikliğin senin işlemin mi yoksa OİS'in otomatik ataması mı olduğu HTML'den ayırt edilemiyor."

// SelectionChanges seçili ders listesindeki ekleme / çıkarmalardır (sesli).
func SelectionChanges(changes []dersecme.SelectionChange) notify.Message {
	var rich, plain strings.Builder
	rich.WriteString("<p><b>🔔 Ders listen değişti</b></p><ul>")
	plain.WriteString("<b>🔔 Ders listen değişti</b>\n<blockquote>")
	for i, ch := range changes {
		label := "➕ <b>Eklendi:</b> "
		if ch.Type == "removed" {
			label = "➖ <b>Silindi:</b> "
		}
		line := label + esc(ch.Course.Code) + " · " + esc(ch.Course.Name)
		rich.WriteString("<li>" + line + "</li>")
		if i > 0 {
			plain.WriteString("\n")
		}
		plain.WriteString(line)
	}
	rich.WriteString("</ul><footer>ℹ️ " + attributionNote + "</footer>")
	plain.WriteString("</blockquote>\n<i>ℹ️ " + attributionNote + "</i>")
	return notify.Message{Rich: rich.String(), Plain: plain.String()}
}

const poolOnlineNote = "Bu havuzda online bilgisi gösterilmiyor; öğretim şeklini doğrulamak gerekiyor."

// PoolAvailable seçmeli havuzda yer açılan derslerdir (sesli).
func PoolAvailable(courses []dersecme.PoolCourse) notify.Message {
	var rich, plain strings.Builder
	rich.WriteString("<p><b>🔔 Seçmeli havuzda yer var</b></p><table compact striped><tr><th>Kod</th><th>Ders</th><th>Kalan</th></tr>")
	plain.WriteString("<b>🔔 Seçmeli havuzda yer var</b>\n<blockquote>")
	for i, c := range courses {
		rich.WriteString(fmt.Sprintf("<tr><td>%s</td><td><b>%s</b></td><td>%d</td></tr>", esc(c.Code), esc(c.Name), c.Quota))
		if i > 0 {
			plain.WriteString("\n")
		}
		plain.WriteString(fmt.Sprintf("%s · <b>%s</b> · kalan %d", esc(c.Code), esc(c.Name), c.Quota))
	}
	rich.WriteString("</table><footer>" + poolOnlineNote + "</footer>")
	plain.WriteString("</blockquote>\n<i>" + poolOnlineNote + "</i>")
	return notify.Message{Rich: rich.String(), Plain: plain.String()}
}

// ---- yardımcılar ----

func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func joinNonEmpty(sep string, values ...string) string {
	var out []string
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			out = append(out, v)
		}
	}
	return strings.Join(out, sep)
}

// datives ay adlarının "-e / -a" halidir: "15 Aralık'a", "10 Eylül'e".
var datives = [...]string{"", "Ocak'a", "Şubat'a", "Mart'a", "Nisan'a", "Mayıs'a", "Haziran'a", "Temmuz'a", "Ağustos'a", "Eylül'e", "Ekim'e", "Kasım'a", "Aralık'a"}

// UntilPhrase "15 Aralık'a kadar" biçimidir.
func UntilPhrase(d time.Time) string { return fmt.Sprintf("%d %s kadar", d.Day(), datives[d.Month()]) }

// locatives ay adlarının "-de / -da / -te / -ta" halidir: "28 Aralık'ta".
var locatives = [...]string{"", "Ocak'ta", "Şubat'ta", "Mart'ta", "Nisan'da", "Mayıs'ta", "Haziran'da", "Temmuz'da", "Ağustos'ta", "Eylül'de", "Ekim'de", "Kasım'da", "Aralık'ta"}

// OnDay "28 Aralık'ta" biçimidir.
func OnDay(d time.Time) string { return fmt.Sprintf("%d %s", d.Day(), locatives[d.Month()]) }

func clock(t time.Time) string { return t.In(Location).Format("15:04") }

// stamp "13 Kasım 14:05" biçimidir.
func stamp(t time.Time) string {
	t = t.In(Location)
	return fmt.Sprintf("%d %s %s", t.Day(), months[t.Month()], t.Format("15:04"))
}

func shortDate(d time.Time) string {
	return fmt.Sprintf("%d %s %s", d.Day(), shortMonths[d.Month()], shortWeekdays[d.Weekday()])
}

func longDate(d time.Time) string {
	return fmt.Sprintf("%d %s %s", d.Day(), months[d.Month()], weekdays[d.Weekday()])
}

// countdown bir sınav gününe kalan süredir; short tabloda kullanılır.
func countdown(date, today time.Time, short bool) string {
	days := int(date.Sub(today).Hours() / 24)
	switch {
	case days == 0:
		return "bugün"
	case days == 1:
		return "yarın"
	case short:
		return fmt.Sprintf("%d gün", days)
	default:
		return fmt.Sprintf("%d gün sonra", days)
	}
}

func ago(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "az önce"
	case d < time.Hour:
		return fmt.Sprintf("%d dk önce", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d sa önce", int(d.Hours()))
	default:
		return fmt.Sprintf("%d gün önce", int(d.Hours()/24))
	}
}

func everyText(interval time.Duration) string {
	if interval < time.Minute {
		return fmt.Sprintf("%d sn'de bir", int(interval.Seconds()))
	}
	return fmt.Sprintf("%d dk'da bir", int(interval.Minutes()))
}

func durationText(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%d dk", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d sa %d dk", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%d gün %d sa", int(d.Hours()/24), int(d.Hours())%24)
	}
}
