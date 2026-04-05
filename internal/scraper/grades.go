package scraper

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"golang.org/x/net/html"

	"notbot/config"
)

// Component bir sınav bileşenidir (Ara Sınav, Final vb.)
type Component struct {
	Name   string `json:"name"`
	Weight string `json:"weight"`
	Score  string `json:"score"`
	Date   string `json:"date"`
}

// Course bir ders ve ona ait sınav bileşenlerini tutar.
type Course struct {
	Code       string      `json:"code"`
	Name       string      `json:"name"`
	Components []Component `json:"components"`
}

// FetchGrades Atlas OIS'ten sınav sonuçlarını çeker.
func FetchGrades(client *http.Client, cfg *config.Config) ([]Course, error) {
	gradesURL := cfg.UniversityURL + "/ogrenciler/belge/ogrsinavsonuc"
	req, _ := http.NewRequest("GET", gradesURL, nil)
	req.Header.Set("User-Agent", cfg.UserAgent)
	req.Header.Set("Referer", cfg.UniversityURL+"/")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sinav sonuc GET: %w", err)
	}
	defer resp.Body.Close()

	// Session expire tespiti
	if strings.Contains(resp.Request.URL.Path, "login") || strings.Contains(resp.Request.URL.Path, "auth") {
		return nil, fmt.Errorf("session_expired")
	}

	body, _ := io.ReadAll(resp.Body)
	return parseGrades(body), nil
}

// parseGrades Atlas OIS sınav sonuçları sayfasını parse eder.
//
// Sayfa yapısı:
//   <table class="a4"> (son tablo — notlar)
//     <tr>
//       <th class="belge_satir">Etki Oranı</th>
//       <th class="belge_satir">CODE - NAME <h3> </h3></th>
//       <th class="belge_satir">Puan</th>
//       <th class="belge_satir">Açıklanma Tarihi</th>
//     </tr>
//     <!-- notlar açıklandığında aşağıdaki satırlar eklenir -->
//     <tr>
//       <td>%40</td>
//       <td>Ara Sınav</td>
//       <td>85.0</td>
//       <td>15/03/2026</td>
//     </tr>
//   </table>
func parseGrades(body []byte) []Course {
	doc, _ := html.Parse(strings.NewReader(string(body)))

	// Sayfadaki tüm <table class="a4"> tablolarını bul
	var tables []*html.Node
	findTables(doc, &tables)

	if len(tables) == 0 {
		return nil
	}

	// Son a4 tablosu not tablosudur
	gradeTable := tables[len(tables)-1]

	// Tüm <tr>'leri çek
	var trs []*html.Node
	findTRs(gradeTable, &trs)

	var courses []Course
	var current *Course

	for _, tr := range trs {
		ths := getChildrenByTag(tr, "th")
		tds := getChildrenByTag(tr, "td")

		if len(ths) >= 2 {
			// Bu bir ders başlık satırı
			// İkinci <th> ders kodunu ve adını içerir: "1410121006 - Matematik II"
			courseText := textContent(ths[1])
			code, name := parseCourseText(courseText)

			if code != "" || name != "" {
				courses = append(courses, Course{Code: code, Name: name})
				current = &courses[len(courses)-1]
			}
		} else if len(tds) >= 4 && current != nil {
			// Bu bir not satırı (sınav bileşeni)
			comp := Component{
				Weight: strings.TrimSpace(textContent(tds[0])),
				Name:   strings.TrimSpace(textContent(tds[1])),
				Score:  strings.TrimSpace(textContent(tds[2])),
				Date:   strings.TrimSpace(textContent(tds[3])),
			}
			if comp.Name != "" || comp.Score != "" {
				current.Components = append(current.Components, comp)
			}
		}
	}

	return courses
}

// parseCourseText "1410121006 - Matematik II" formatındaki metni parse eder.
func parseCourseText(text string) (code, name string) {
	text = strings.TrimSpace(text)
	idx := strings.Index(text, " - ")
	if idx > 0 {
		return strings.TrimSpace(text[:idx]), strings.TrimSpace(text[idx+3:])
	}
	return "", text
}

// findTables <table class="a4"> elementlerini bulur.
func findTables(n *html.Node, tables *[]*html.Node) {
	if n.Type == html.ElementNode && n.Data == "table" {
		for _, a := range n.Attr {
			if a.Key == "class" && strings.Contains(a.Val, "a4") {
				*tables = append(*tables, n)
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		findTables(c, tables)
	}
}

// findTRs bir node altındaki tüm <tr> elementlerini bulur.
func findTRs(n *html.Node, trs *[]*html.Node) {
	if n.Type == html.ElementNode && n.Data == "tr" {
		*trs = append(*trs, n)
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		findTRs(c, trs)
	}
}

// getChildrenByTag direkt çocuklar arasında belirtilen tag'i bulur.
func getChildrenByTag(n *html.Node, tag string) []*html.Node {
	var result []*html.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == tag {
			result = append(result, c)
		}
	}
	return result
}

// textContent bir node'un tüm metin içeriğini döner.
func textContent(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(strings.TrimSpace(n.Data))
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}
