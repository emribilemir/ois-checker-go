package schedule

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"notbot/config"
)

func Fetch(client *http.Client, cfg *config.Config) ([]Entry, error) {
	target := strings.TrimRight(cfg.UniversityURL, "/") + "/ogrenciler/belge/ogrdersprogrami"
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("ders programı isteği: %w", err)
	}
	req.Header.Set("User-Agent", cfg.UserAgent)
	req.Header.Set("Referer", strings.TrimRight(cfg.UniversityURL, "/")+"/")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ders programı isteği: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ders programı HTTP %d", resp.StatusCode)
	}
	if strings.Contains(resp.Request.URL.Path, "login") || strings.Contains(resp.Request.URL.Path, "auth") {
		return nil, fmt.Errorf("OİS oturumu sona erdi")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("ders programı okunamadı: %w", err)
	}
	return Parse(body)
}
