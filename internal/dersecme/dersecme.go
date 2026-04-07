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

// Aranacak anahtar kelimeler (küçük harf)
var keywords = []string{
	"ders seçme",
	"ders secme",
	"ders kayıt",
	"ders kayit",
	"ders seçimi",
	"ders secimi",
	"ders kaydı",
	"ders kaydi",
	"course registration",
	"course selection",
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

	// Sayfadaki tüm text içeriğini topla
	allText := strings.ToLower(extractAllText(doc))

	// Anahtar kelimeleri ara
	for _, kw := range keywords {
		if strings.Contains(allText, kw) {
			log.Printf("[dersecme] ✅ Eşleşme bulundu: %q", kw)
			return true, kw, nil
		}
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
				if a.Key == "href" {
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
