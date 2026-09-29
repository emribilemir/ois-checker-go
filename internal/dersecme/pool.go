package dersecme

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"

	"notbot/config"
)

var poolPathPattern = regexp.MustCompile(`^/ogrenciler/derssecme/popderssecme/havuz_id/[0-9]+/ogrenci_slot_id/[0-9]+/?$`)

type PoolCourse struct {
	Code  string `json:"code"`
	Name  string `json:"name"`
	Quota int    `json:"quota"`
}

// FetchPool reads an elective pool. It never invokes the course-enrolment endpoint.
func FetchPool(client *http.Client, cfg *config.Config, poolPath string) ([]PoolCourse, error) {
	base, err := url.Parse(cfg.UniversityURL)
	if err != nil || base.Scheme != "https" || base.Host == "" {
		return nil, fmt.Errorf("geçersiz OİS adresi")
	}
	ref, err := url.Parse(poolPath)
	if err != nil || ref.IsAbs() || ref.Host != "" || ref.RawQuery != "" || ref.Fragment != "" || !poolPathPattern.MatchString(ref.Path) {
		return nil, fmt.Errorf("geçersiz seçmeli havuz yolu")
	}
	target := base.ResolveReference(ref)
	req, err := http.NewRequest(http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", cfg.UserAgent)
	req.Header.Set("Referer", strings.TrimRight(cfg.UniversityURL, "/")+"/")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("seçmeli havuz isteği: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Request.URL.Host != base.Host || resp.Request.URL.Path != ref.Path {
		return nil, fmt.Errorf("seçmeli havuz yanıtı: HTTP %d, URL %s", resp.StatusCode, resp.Request.URL)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("seçmeli havuz okuma: %w", err)
	}
	if isLoginDocument(body) {
		return nil, fmt.Errorf("session_expired")
	}
	return parsePool(body)
}

func parsePool(body []byte) ([]PoolCourse, error) {
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("seçmeli havuz HTML: %w", err)
	}
	var tables []*html.Node
	collectElements(doc, "table", &tables)
	for _, table := range tables {
		var headers []*html.Node
		collectElements(table, "th", &headers)
		hasCode, hasName, hasQuota := false, false, false
		for _, header := range headers {
			switch normalizeText(nodeText(header)) {
			case "ders kodu":
				hasCode = true
			case "ders adi":
				hasName = true
			case "kalan kota":
				hasQuota = true
			}
		}
		if !hasCode || !hasName || !hasQuota {
			continue
		}
		courses := make([]PoolCourse, 0)
		var rows []*html.Node
		collectElements(table, "tr", &rows)
		for _, row := range rows {
			cells := directElementChildren(row, "td")
			if len(cells) < 5 {
				continue
			}
			code, name := nodeText(cells[0]), nodeText(cells[1])
			quota, err := strconv.Atoi(strings.TrimSpace(nodeText(cells[4])))
			if code == "" || name == "" || err != nil || quota < 0 {
				continue
			}
			courses = append(courses, PoolCourse{Code: code, Name: name, Quota: quota})
		}
		return courses, nil
	}
	return nil, fmt.Errorf("seçmeli havuz tablosu bulunamadı")
}

// NewlyAvailable reports only new courses with open seats and courses that reopened.
func NewlyAvailable(previous, current []PoolCourse) []PoolCourse {
	previousByCode := make(map[string]PoolCourse, len(previous))
	for _, course := range previous {
		previousByCode[course.Code] = course
	}
	var available []PoolCourse
	for _, course := range current {
		old, exists := previousByCode[course.Code]
		if course.Quota > 0 && (!exists || old.Quota <= 0 || old.Name != course.Name) {
			available = append(available, course)
		}
	}
	return available
}
