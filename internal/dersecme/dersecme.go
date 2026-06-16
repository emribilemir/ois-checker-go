package dersecme

import (
	"fmt"
	"io"
	"log"
	"net/http"
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

// Check OIS ana sayfasındaki sidebar menüsünü kontrol eder.
// Ders seçme ile ilgili bir ifade bulunursa (found=true, matchedKeyword) döner.
func Check(client *http.Client, cfg *config.Config) (found bool, matchedKeyword string, err error) {
	// OIS ana sayfasını çek
	targetURL := cfg.UniversityURL + "/"
	req, _ := http.NewRequest("GET", targetURL, nil)
	req.Header.Set("User-Agent", cfg.UserAgent)
	req.Header.Set("Referer", cfg.UniversityURL+"/")

	resp, err := client.Do(req)
	if err != nil {
		return false, "", fmt.Errorf("ders seçme sayfası GET: %w", err)
	}
	defer resp.Body.Close()

	// Session expire tespiti
	if strings.Contains(resp.Request.URL.Path, "login") || strings.Contains(resp.Request.URL.Path, "auth") {
		return false, "", fmt.Errorf("session_expired")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, "", fmt.Errorf("body okuma: %w", err)
	}

	log.Printf("[dersecme] Sayfa çekildi: %d byte, URL=%s", len(body), resp.Request.URL.String())

	return searchKeywords(body)
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

	for _, kw := range activeKeywords {
		normalizedKeyword := normalizeText(kw)
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

	if phrase, ok := firstInactivePhrase(allText); ok {
		log.Printf("[dersecme] ⏸ Ders seçme kapalı görünüyor: %q", phrase)
		return false, "", nil
	}

	log.Println("[dersecme] ❌ Ders seçme ifadesi bulunamadı")
	return false, "", nil
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
