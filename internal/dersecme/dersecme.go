package dersecme

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/html"

	"notbot/config"
)

// Aranacak aktif ders seçme sinyalleri.
var activeKeywords = []string{
	"ders seçme",
	"ders secme",
	"derssecme",
	"ders seç",
	"ders sec",
	"derssec",
	"ders kayıt",
	"ders kayit",
	"derskayit",
	"ders seçimi",
	"ders secimi",
	"derssecimi",
	"ders kaydı",
	"ders kaydi",
	"ders alma",
	"ders ekle",
	"ders bırak",
	"ders birak",
	"ekle bırak",
	"ekle birak",
	"kayıt yenileme",
	"kayit yenileme",
	"course registration",
	"course selection",
	"add drop",
	"add/drop",
}

// Bu ifadeler varsa ders seçme menüsü/uyarısı görünse bile süreç açık değildir.
var inactivePhrases = []string{
	"ders seçme işlemleri kapalı",
	"ders seçimleri sona ermiştir",
	"ders secimleri sona ermistir",
	"ders seçimi sona ermiştir",
	"ders secimi sona ermistir",
	"ders seçme sona ermiştir",
	"ders secme sona ermistir",
	"ders seçme işlemi için danışmanınızla iletişime geçiniz",
	"ders secme islemi icin danismaninizla iletisime geciniz",
	"ders seçme işlemi sona ermiştir",
	"ders secme islemi sona ermistir",
	"ders kayıtları sona ermiştir",
	"ders kayitlari sona ermistir",
	"course registration has ended",
	"course selection has ended",
}

type SelectedCourse struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

type Status struct {
	Open            bool
	Signal          string
	SelectedCourses []SelectedCourse
}

// Check OIS ana sayfasındaki sidebar menüsünü kontrol eder.
// Ders seçme ile ilgili bir ifade bulunursa (found=true, matchedKeyword) döner.
func Check(client *http.Client, cfg *config.Config) (found bool, matchedKeyword string, err error) {
	status, err := Inspect(client, cfg)
	return status.Open, status.Signal, err
}

// Inspect returns the complete observable course-selection state.
func Inspect(client *http.Client, cfg *config.Config) (Status, error) {
	// OIS ana sayfasını çek
	targetURL := cfg.UniversityURL + "/"
	req, _ := http.NewRequest("GET", targetURL, nil)
	req.Header.Set("User-Agent", cfg.UserAgent)
	req.Header.Set("Referer", cfg.UniversityURL+"/")

	resp, err := client.Do(req)
	if err != nil {
		return Status{}, fmt.Errorf("ders seçme sayfası GET: %w", err)
	}
	defer resp.Body.Close()

	// Session expire tespiti
	if strings.Contains(resp.Request.URL.Path, "login") || strings.Contains(resp.Request.URL.Path, "auth") {
		return Status{}, fmt.Errorf("session_expired")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Status{}, fmt.Errorf("body okuma: %w", err)
	}
	if isLoginDocument(body) {
		return Status{}, fmt.Errorf("session_expired")
	}

	log.Printf("[dersecme] Ana sayfa çekildi: %d byte, URL=%s", len(body), resp.Request.URL.String())

	links, err := findCourseSelectionLinks(body, resp.Request.URL)
	if err != nil {
		return Status{}, err
	}
	if len(links) > 0 {
		return inspectCourseSelectionPage(client, cfg, links[0])
	}

	found, signal, err := searchKeywords(body)
	return Status{Open: found, Signal: signal}, err
}

func inspectCourseSelectionPage(client *http.Client, cfg *config.Config, targetURL string) (Status, error) {
	req, _ := http.NewRequest("GET", targetURL, nil)
	req.Header.Set("User-Agent", cfg.UserAgent)
	req.Header.Set("Referer", cfg.UniversityURL+"/")

	resp, err := client.Do(req)
	if err != nil {
		return Status{}, fmt.Errorf("ders seçme hedef sayfası GET: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Status{}, fmt.Errorf("ders seçme hedef sayfası GET: status=%d", resp.StatusCode)
	}
	if strings.Contains(resp.Request.URL.Path, "login") || strings.Contains(resp.Request.URL.Path, "auth") {
		return Status{}, fmt.Errorf("session_expired")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Status{}, fmt.Errorf("ders seçme hedef body okuma: %w", err)
	}
	if isLoginDocument(body) {
		return Status{}, fmt.Errorf("session_expired")
	}
	log.Printf("[dersecme] Hedef sayfa çekildi: %d byte, URL=%s", len(body), resp.Request.URL.String())
	found, signal, err := searchKeywords(body)
	return Status{
		Open:            found,
		Signal:          signal,
		SelectedCourses: parseSelectedCourses(body),
	}, err
}

func parseSelectedCourses(body []byte) []SelectedCourse {
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil
	}

	table := selectedCoursesTable(doc)
	if table == nil {
		return nil
	}

	var rows []*html.Node
	collectElements(table, "tr", &rows)
	courses := make([]SelectedCourse, 0)
	for _, row := range rows {
		cells := directElementChildren(row, "td")
		if len(cells) < 2 {
			continue
		}
		code := strings.TrimSpace(nodeText(cells[0]))
		name := strings.TrimSpace(nodeText(cells[1]))
		if code == "" || name == "" {
			continue
		}
		courses = append(courses, SelectedCourse{Code: code, Name: name})
	}
	return courses
}

func selectedCoursesTable(n *html.Node) *html.Node {
	if n.Type == html.ElementNode && n.Data == "table" {
		var headers []*html.Node
		collectElements(n, "th", &headers)
		for _, header := range headers {
			if strings.Contains(normalizeText(nodeText(header)), "sectiginiz dersler") {
				return n
			}
		}
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if table := selectedCoursesTable(child); table != nil {
			return table
		}
	}
	return nil
}

func collectElements(n *html.Node, tag string, result *[]*html.Node) {
	if n.Type == html.ElementNode && n.Data == tag {
		*result = append(*result, n)
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		collectElements(child, tag, result)
	}
}

func directElementChildren(n *html.Node, tag string) []*html.Node {
	var result []*html.Node
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode && child.Data == tag {
			result = append(result, child)
		}
	}
	return result
}

func nodeText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current.Type == html.TextNode {
			b.WriteString(current.Data)
			b.WriteByte(' ')
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return strings.Join(strings.Fields(b.String()), " ")
}

// isLoginDocument catches Atlas OIS login HTML served with a successful status
// at the originally requested URL. This happens when another browser invalidates
// the current session, so checking only the final response URL is insufficient.
func isLoginDocument(body []byte) bool {
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return false
	}

	var hasLoginAction, hasUsername, hasPassword, hasCaptcha bool
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "form":
				hasLoginAction = hasLoginAction || strings.Contains(strings.ToLower(attrValue(n, "action")), "/auth/login")
			case "input":
				switch strings.ToLower(attrValue(n, "name")) {
				case "kullanici_adi":
					hasUsername = true
				case "kullanici_sifre":
					hasPassword = true
				case "captcha":
					hasCaptcha = true
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)

	if hasLoginAction && hasUsername && hasPassword && hasCaptcha {
		return true
	}
	return strings.Contains(normalizeText(extractAllText(doc)), "oturumunuz farkli bir ekranda acildi")
}

func attrValue(n *html.Node, key string) string {
	for _, attr := range n.Attr {
		if strings.EqualFold(attr.Key, key) {
			return attr.Val
		}
	}
	return ""
}

func findCourseSelectionLinks(body []byte, baseURL *url.URL) ([]string, error) {
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("ders seçme linkleri parse: %w", err)
	}

	seen := make(map[string]bool)
	var links []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			href := ""
			for _, attr := range n.Attr {
				if strings.EqualFold(attr.Key, "href") {
					href = strings.TrimSpace(attr.Val)
					break
				}
			}
			searchable := normalizeText(extractAllText(n) + " " + href)
			if href != "" && isCourseSelectionSignal(searchable) {
				if parsed, parseErr := url.Parse(href); parseErr == nil {
					resolved := baseURL.ResolveReference(parsed).String()
					if !seen[resolved] {
						seen[resolved] = true
						links = append(links, resolved)
					}
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return links, nil
}

func isCourseSelectionSignal(text string) bool {
	for _, keyword := range activeKeywords {
		if strings.Contains(text, normalizeText(keyword)) {
			return true
		}
	}
	return false
}

// searchKeywords HTML body içinde anahtar kelimeleri arar.
// Hem <nav> elementleri hem de <a> linkleri dahil tüm text content kontrol edilir.
func searchKeywords(body []byte) (found bool, matchedKeyword string, err error) {
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return false, "", fmt.Errorf("html parse: %w", err)
	}

	// Sayfadaki tüm aranabilir içeriği topla
	allText := normalizeText(extractAllText(doc))

	// Kalıcı menü linkleri "Ders Seçme" yazsa bile açık bir kapanış duyurusu
	// varsa süreç aktif değildir. Kapanış sinyali sayfa genelinde önceliklidir.
	if phrase, ok := firstInactivePhrase(allText); ok {
		log.Printf("[dersecme] ⏸ Ders seçme kapalı görünüyor: %q", phrase)
		return false, "", nil
	}

	// Atlas'ın menüsündeki "Ders Seçme" bağlantısı dönem dışında da kalıcıdır.
	// Açık sayfayı, ekteki gerçek sayfada bulunan işlem kontrolleri ve kayıt
	// endpoint'i gibi kullanıcıya ders ekleme/silme yetkisi veren sinyallerle ayır.
	if hasCourseSelectionControls(allText) {
		log.Println("[dersecme] ✅ Aktif ders seçme işlem kontrolleri bulundu")
		return true, "ders seçme işlem kontrolleri", nil
	}

	for _, kw := range activeKeywords {
		normalizedKeyword := normalizeText(kw)
		if isPersistentMenuKeyword(normalizedKeyword) {
			continue
		}
		for _, idx := range findAllIndexes(allText, normalizedKeyword) {
			snippet := surroundingText(allText, idx, len(normalizedKeyword), 180)
			if containsInactivePhrase(snippet) {
				log.Printf("[dersecme] ⏸ Kapalı süreç ifadesi atlandı: %q", kw)
				continue
			}
			log.Printf("[dersecme] ✅ Aktif ders seçme sinyali bulundu: %q", kw)
			return true, kw, nil
		}
	}

	log.Println("[dersecme] ❌ Ders seçme ifadesi bulunamadı")
	return false, "", nil
}

func hasCourseSelectionControls(text string) bool {
	for _, signal := range []string{
		"/ogrenciler/derssecme/ogrderskaydet",
		"dersial(",
		"dersisil(",
		"danismanagonder(",
	} {
		if strings.Contains(text, signal) {
			return true
		}
	}
	return false
}

func isPersistentMenuKeyword(keyword string) bool {
	switch keyword {
	case "ders secme", "derssecme", "ders sec":
		return true
	default:
		return false
	}
}

// extractAllText bir HTML node ağacındaki tüm metin içeriğini birleştirir.
func extractAllText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
			b.WriteString(" ")
		}
		// href attribute'larını da kontrol et (URL path'lerde "ders" geçebilir)
		if n.Type == html.ElementNode {
			for _, a := range n.Attr {
				if searchableAttribute(a.Key) {
					b.WriteString(a.Val)
					b.WriteString(" ")
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

func searchableAttribute(key string) bool {
	switch strings.ToLower(key) {
	case "href", "title", "alt", "aria-label", "data-original-title", "onclick", "value":
		return true
	default:
		return false
	}
}

func normalizeText(text string) string {
	text = strings.ToLower(text)
	replacer := strings.NewReplacer(
		"ç", "c",
		"ğ", "g",
		"ı", "i",
		"i̇", "i",
		"ö", "o",
		"ş", "s",
		"ü", "u",
	)
	text = replacer.Replace(text)
	return strings.Join(strings.Fields(text), " ")
}

func findAllIndexes(text, needle string) []int {
	var indexes []int
	offset := 0
	for {
		idx := strings.Index(text[offset:], needle)
		if idx < 0 {
			return indexes
		}
		indexes = append(indexes, offset+idx)
		offset += idx + len(needle)
	}
}

func surroundingText(text string, idx, length, radius int) string {
	start := idx - radius
	if start < 0 {
		start = 0
	}
	end := idx + length + radius
	if end > len(text) {
		end = len(text)
	}
	return text[start:end]
}

func containsInactivePhrase(text string) bool {
	_, ok := firstInactivePhrase(text)
	return ok
}

func firstInactivePhrase(text string) (string, bool) {
	for _, phrase := range inactivePhrases {
		normalizedPhrase := normalizeText(phrase)
		if strings.Contains(text, normalizedPhrase) {
			return phrase, true
		}
	}
	if strings.Contains(text, "sona ermistir") && strings.Contains(text, "ders sec") {
		return "ders seçme sona ermiştir", true
	}
	if strings.Contains(text, "sona erdi") && strings.Contains(text, "ders sec") {
		return "ders seçme sona erdi", true
	}
	return "", false
}
