// Package exams OİS'teki kişisel sınav takvimini bulur, okur ve Telegram için biçimler.
//
// Sayfa /ogrenciler/belge/sinavprogrami adresinden okunur; okunamazsa yalnız OİS
// menüsünde görünen sınav bağlantıları denenir. Tablo düzeni sabit kabul edilmez,
// sütun başlıklarından (Tarih, Saat, Ders, Derslik ...) tanınır. Okunamazsa hata
// mesajı yalnız denenen adresleri, menüdeki sınav bağlantılarının adını ve tablo
// başlıklarını içerir; satır içeriği (kişisel veri) içermez.
package exams

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"golang.org/x/net/html"

	"notbot/config"
)

// Exam takvimdeki tek bir sınavdır.
type Exam struct {
	Code  string
	Name  string
	Kind  string
	Date  time.Time // yalnız gün; saat Start alanında
	Start string
	End   string
	Place string
}

// calendarPath Atlas OİS'teki sınav programı sayfasıdır. OİS bilinmeyen
// adresleri /auth/logout'a yönlendirdiği için adres tahmini yapılmaz; bu sayfa
// okunamazsa yalnız menüde gerçekten görünen bağlantılar denenir.
const calendarPath = "/ogrenciler/belge/sinavprogrami"

var linkSignals = []string{
	"sinav program", "sinavprogram", "sinav takvim", "sinavtakvim",
	"sinav yer", "sinavyer", "sinavlarim", "exam schedule", "exam program",
}

var popupPattern = regexp.MustCompile(`['"](/[^'"\s]+)['"]`)

// Fetch sınav takvimini OİS'ten okur. Oturumun açık olması beklenir.
func Fetch(client *http.Client, cfg *config.Config) ([]Exam, error) {
	base, err := url.Parse(strings.TrimRight(cfg.UniversityURL, "/") + "/")
	if err != nil || base.Host == "" {
		return nil, fmt.Errorf("geçersiz OİS adresi")
	}

	known := base.ResolveReference(&url.URL{Path: calendarPath}).String()
	exams, note, err := readPage(client, cfg, known)
	if err != nil || note == "" {
		return exams, err
	}
	report := []string{note}

	home, homeURL, err := get(client, cfg, base.String())
	if errors.Is(err, ErrSessionExpired) {
		return nil, err
	}
	if err != nil {
		report = append(report, "ana sayfa: "+err.Error())
	} else {
		links, labels := discoverLinks(home, homeURL, base)
		if len(labels) > 0 {
			report = append(report, "Menüdeki sınav bağlantıları: "+strings.Join(labels, " | "))
		} else {
			report = append(report, "Menüde sınav takvimi bağlantısı bulunamadı")
		}
		for _, link := range links {
			if link == known {
				continue
			}
			exams, note, err := readPage(client, cfg, link)
			if err != nil || note == "" {
				return exams, err
			}
			report = append(report, note)
		}
	}
	return nil, fmt.Errorf("sınav takvimi okunamadı\n- %s", strings.Join(report, "\n- "))
}

// readPage bir sayfayı okur. Takvim tanınırsa note boştur; tanınmazsa note
// tanı satırıdır. err yalnız oturum kapandığında döner.
func readPage(client *http.Client, cfg *config.Config, target string) ([]Exam, string, error) {
	body, finalURL, err := get(client, cfg, target)
	if errors.Is(err, ErrSessionExpired) {
		return nil, "", err
	}
	if err != nil {
		return nil, fmt.Sprintf("%s: %v", pathOf(target), err), nil
	}
	exams, headers, ok := Parse(body)
	if ok {
		return exams, "", nil
	}
	if len(headers) > 0 {
		return nil, fmt.Sprintf("%s: tablo tanınmadı, başlıklar: %s", pathOf(finalURL.String()), strings.Join(headers, " / ")), nil
	}
	return nil, fmt.Sprintf("%s: sayfada tablo yok (%d bayt)", pathOf(finalURL.String()), len(body)), nil
}

// ErrSessionExpired, OİS giriş sayfasına düşüldüğünde döner.
var ErrSessionExpired = errors.New("OİS oturumu sona erdi")

func get(client *http.Client, cfg *config.Config, target string) ([]byte, *url.URL, error) {
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("istek: %w", err)
	}
	req.Header.Set("User-Agent", cfg.UserAgent)
	req.Header.Set("Referer", strings.TrimRight(cfg.UniversityURL, "/")+"/")
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("istek: %w", err)
	}
	defer resp.Body.Close()
	finalURL := resp.Request.URL
	if p := strings.ToLower(finalURL.Path); strings.Contains(p, "login") || strings.Contains(p, "/auth") {
		return nil, nil, ErrSessionExpired
	}
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, nil, fmt.Errorf("okuma: %w", err)
	}
	if isLoginPage(body) {
		return nil, nil, ErrSessionExpired
	}
	return body, finalURL, nil
}

// discoverLinks menüde sınav takvimine giden, aynı sunucudaki bağlantıları bulur.
// Sınav sonuçları (notlar) sayfası hariç tutulur.
func discoverLinks(body []byte, pageURL, base *url.URL) (links, labels []string) {
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, nil
	}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			text := cleanText(nodeText(n))
			href := strings.TrimSpace(attr(n, "href"))
			onclick := attr(n, "onclick")
			searchable := fold(text + " " + href + " " + onclick)
			if isExamSignal(searchable) && !strings.Contains(searchable, "sonuc") {
				if text != "" {
					labels = appendUnique(labels, text)
				}
				for _, raw := range linkTargets(href, onclick) {
					parsed, err := url.Parse(raw)
					if err != nil {
						continue
					}
					resolved := pageURL.ResolveReference(parsed)
					if resolved.Host == base.Host && (resolved.Scheme == "http" || resolved.Scheme == "https") {
						resolved.Fragment = ""
						links = appendUnique(links, resolved.String())
					}
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return links, labels
}

func linkTargets(href, onclick string) []string {
	var targets []string
	lower := strings.ToLower(href)
	if href != "" && href != "#" && !strings.HasPrefix(lower, "javascript:") {
		targets = append(targets, href)
	}
	for _, source := range []string{href, onclick} {
		for _, match := range popupPattern.FindAllStringSubmatch(source, -1) {
			targets = append(targets, match[1])
		}
	}
	return targets
}

func isExamSignal(text string) bool {
	for _, signal := range linkSignals {
		if strings.Contains(text, signal) {
			return true
		}
	}
	return false
}

type columns struct{ date, clock, end, course, code, kind, place int }

// Parse sınav takvimi tablosunu okur. ok=false ise sayfada tanınan bir sınav
// tablosu yoktur; headers tanı için görülen tablo başlıklarıdır.
func Parse(body []byte) (exams []Exam, headers []string, ok bool) {
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, nil, false
	}
	var tables []*html.Node
	findElements(doc, "table", &tables)
	for _, table := range tables {
		rows := tableRows(table)
		for headerIndex, row := range rows {
			cells := directCells(row)
			labels := make([]string, len(cells))
			for i, cell := range cells {
				labels[i] = cleanText(nodeText(cell))
			}
			cols, recognised := mapColumns(labels)
			if !recognised {
				if headerIndex == 0 && len(labels) > 1 && hasHeaderCells(row) {
					headers = appendUnique(headers, strings.Join(nonEmpty(labels), ", "))
				}
				continue
			}
			found := parseRows(rows[headerIndex+1:], cols)
			exams = append(exams, found...)
			ok = true
			break
		}
	}
	sort.SliceStable(exams, func(i, j int) bool {
		if !exams[i].Date.Equal(exams[j].Date) {
			return exams[i].Date.Before(exams[j].Date)
		}
		return exams[i].Start < exams[j].Start
	})
	return exams, headers, ok
}

func mapColumns(labels []string) (columns, bool) {
	cols := columns{-1, -1, -1, -1, -1, -1, -1}
	for i, label := range labels {
		f := fold(label)
		switch {
		case f == "":
		case strings.Contains(f, "aciklanma"):
			// Not sayfasının "Açıklanma Tarihi" sütunu sınav tarihi değildir.
		case strings.Contains(f, "tarih") && cols.date < 0:
			cols.date = i
			if strings.Contains(f, "saat") && cols.clock < 0 {
				cols.clock = i
			}
		case strings.Contains(f, "bitis") && cols.end < 0:
			cols.end = i
		case strings.Contains(f, "saat") && cols.clock < 0:
			cols.clock = i
		case (strings.Contains(f, "derslik") || strings.Contains(f, "salon") || strings.Contains(f, "sinif") ||
			strings.Contains(f, "yer") || strings.Contains(f, "mekan") || strings.Contains(f, "bina")) && cols.place < 0:
			cols.place = i
		case strings.Contains(f, "kod") && cols.code < 0:
			cols.code = i
		case (strings.Contains(f, "tur") || strings.Contains(f, "tip") || f == "sinav" || strings.Contains(f, "sinav adi")) && cols.kind < 0:
			cols.kind = i
		case strings.Contains(f, "ders") && cols.course < 0:
			cols.course = i
		}
	}
	return cols, cols.date >= 0 && (cols.course >= 0 || cols.code >= 0)
}

var (
	datePattern  = regexp.MustCompile(`(\d{1,2})[./-](\d{1,2})[./-](\d{4})|(\d{4})-(\d{2})-(\d{2})`)
	clockPattern = regexp.MustCompile(`\b([01]?\d|2[0-3])[:.]([0-5]\d)\b`)
	codePattern  = regexp.MustCompile(`^([A-ZÇĞİÖŞÜ]{2,}\s?\d{2,}[A-Z]?|\d{5,})\s*[-–]\s*(.+)$`)
)

func parseRows(rows []*html.Node, cols columns) []Exam {
	var exams []Exam
	for _, row := range rows {
		cells := directCells(row)
		value := func(i int) string {
			if i < 0 || i >= len(cells) {
				return ""
			}
			return cleanText(nodeText(cells[i]))
		}
		dateText := value(cols.date)
		date, ok := parseDate(dateText)
		if !ok {
			continue
		}
		exam := Exam{Date: date, Name: value(cols.course), Code: value(cols.code), Kind: value(cols.kind), Place: value(cols.place)}
		if exam.Code == "" {
			if match := codePattern.FindStringSubmatch(exam.Name); match != nil {
				exam.Code, exam.Name = strings.TrimSpace(match[1]), strings.TrimSpace(match[2])
			}
		}
		if exam.Name == "" && exam.Code == "" {
			continue
		}
		clockText := value(cols.clock)
		if cols.clock == cols.date || clockText == "" {
			clockText = strings.TrimSpace(datePattern.ReplaceAllString(dateText, ""))
		}
		clocks := clockPattern.FindAllStringSubmatch(clockText, 2)
		if len(clocks) > 0 {
			exam.Start = clocks[0][1] + ":" + clocks[0][2]
			exam.Start = padClock(exam.Start)
		}
		if len(clocks) > 1 {
			exam.End = padClock(clocks[1][1] + ":" + clocks[1][2])
		} else if end := clockPattern.FindStringSubmatch(value(cols.end)); end != nil {
			exam.End = padClock(end[1] + ":" + end[2])
		}
		exams = append(exams, exam)
	}
	return exams
}

func padClock(clock string) string {
	if len(clock) == 4 {
		return "0" + clock
	}
	return clock
}

func parseDate(text string) (time.Time, bool) {
	match := datePattern.FindStringSubmatch(text)
	if match == nil {
		return time.Time{}, false
	}
	var day, month, year string
	if match[1] != "" {
		day, month, year = match[1], match[2], match[3]
	} else {
		year, month, day = match[4], match[5], match[6]
	}
	parsed, err := time.Parse("2-1-2006", day+"-"+month+"-"+year)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

var (
	weekdays = [...]string{"Pazar", "Pazartesi", "Salı", "Çarşamba", "Perşembe", "Cuma", "Cumartesi"}
	months   = [...]string{"", "Ocak", "Şubat", "Mart", "Nisan", "Mayıs", "Haziran", "Temmuz", "Ağustos", "Eylül", "Ekim", "Kasım", "Aralık"}
)

// Format bugünden itibaren olan sınavları Telegram HTML mesajı olarak yazar.
func Format(exams []Exam, now time.Time) string {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	var upcoming []Exam
	past := 0
	for _, exam := range exams {
		if exam.Date.Before(today) {
			past++
			continue
		}
		upcoming = append(upcoming, exam)
	}

	var out strings.Builder
	out.WriteString("📝 <b>Sınav Takvimim</b>\n")
	if len(upcoming) == 0 {
		if len(exams) == 0 {
			out.WriteString("\nOİS'teki sınav takviminde henüz sınav yok.")
		} else {
			out.WriteString("\nOİS'teki takvimde bugünden sonra sınav yok.")
		}
	}
	var lastDate time.Time
	for _, exam := range upcoming {
		if !exam.Date.Equal(lastDate) {
			lastDate = exam.Date
			out.WriteString(fmt.Sprintf("\n<b>%d %s %s</b> · %s\n", exam.Date.Day(), months[exam.Date.Month()], weekdays[exam.Date.Weekday()], countdown(exam.Date, today)))
		}
		clock := "Saat belirtilmemiş"
		if exam.Start != "" {
			clock = exam.Start
			if exam.End != "" {
				clock += "–" + exam.End
			}
		}
		name := exam.Name
		if name == "" {
			name = exam.Code
		}
		out.WriteString(fmt.Sprintf("🕒 %s  <b>%s</b>", clock, html.EscapeString(name)))
		if exam.Kind != "" {
			out.WriteString(" (" + html.EscapeString(exam.Kind) + ")")
		}
		out.WriteString("\n")
		if exam.Place != "" {
			out.WriteString("   📍 " + html.EscapeString(exam.Place) + "\n")
		}
	}
	if past > 0 {
		out.WriteString(fmt.Sprintf("\n<i>Geçmiş %d sınav gösterilmedi.</i>", past))
	}
	return strings.TrimRight(out.String(), "\n")
}

func countdown(date, today time.Time) string {
	days := int(date.Sub(today).Hours() / 24)
	switch days {
	case 0:
		return "bugün"
	case 1:
		return "yarın"
	default:
		return fmt.Sprintf("%d gün sonra", days)
	}
}

func isLoginPage(body []byte) bool {
	lower := strings.ToLower(string(body))
	return strings.Contains(lower, "/auth/login") && strings.Contains(lower, "kullanici_sifre")
}

func pathOf(raw string) string {
	if parsed, err := url.Parse(raw); err == nil && parsed.Path != "" {
		return parsed.Path
	}
	return raw
}

func appendUnique(list []string, value string) []string {
	for _, existing := range list {
		if existing == value {
			return list
		}
	}
	return append(list, value)
}

func nonEmpty(values []string) []string {
	var out []string
	for _, v := range values {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

func hasHeaderCells(row *html.Node) bool {
	for child := row.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode && child.Data == "th" {
			return true
		}
	}
	return false
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}

// fold Türkçe harfleri ASCII küçük harfe indirir: "Sınav Programı" -> "sinav programi".
func fold(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Mn, r) {
			return -1
		}
		switch r {
		case 'Ç', 'ç':
			return 'c'
		case 'Ğ', 'ğ':
			return 'g'
		case 'İ', 'ı', 'I':
			return 'i'
		case 'Ö', 'ö':
			return 'o'
		case 'Ş', 'ş':
			return 's'
		case 'Ü', 'ü':
			return 'u'
		}
		return unicode.ToLower(r)
	}, value)
}

func cleanText(value string) string { return strings.Join(strings.Fields(value), " ") }

func findElements(n *html.Node, tag string, out *[]*html.Node) {
	if n.Type == html.ElementNode && n.Data == tag {
		*out = append(*out, n)
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		findElements(child, tag, out)
	}
}

func tableRows(table *html.Node) []*html.Node {
	var rows []*html.Node
	for child := table.FirstChild; child != nil; child = child.NextSibling {
		switch child.Data {
		case "tr":
			rows = append(rows, child)
		case "tbody", "thead", "tfoot":
			for row := child.FirstChild; row != nil; row = row.NextSibling {
				if row.Data == "tr" {
					rows = append(rows, row)
				}
			}
		}
	}
	return rows
}

func directCells(row *html.Node) []*html.Node {
	var cells []*html.Node
	for child := row.FirstChild; child != nil; child = child.NextSibling {
		if child.Data == "td" || child.Data == "th" {
			cells = append(cells, child)
		}
	}
	return cells
}

func nodeText(n *html.Node) string {
	var out strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			out.WriteString(node.Data)
			out.WriteString(" ")
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return out.String()
}
