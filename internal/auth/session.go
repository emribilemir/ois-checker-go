package auth

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"notbot/config"
)

// SessionActive, istemcinin çerezlerinde hâlâ geçerli bir OİS oturumu olup
// olmadığını tek bir GET ile kontrol eder. Oturum yoksa reason nedenini söyler.
// Emin olunamayan her durumda false döner; çağıran o zaman normal giriş yapar.
func SessionActive(client *http.Client, cfg *config.Config) (active bool, reason string) {
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(cfg.UniversityURL, "/")+"/", nil)
	if err != nil {
		return false, "istek oluşturulamadı"
	}
	req.Header.Set("User-Agent", cfg.UserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Sprintf("ana sayfa açılamadı: %v", err)
	}
	defer resp.Body.Close()
	if path := strings.ToLower(resp.Request.URL.Path); strings.Contains(path, "login") || strings.Contains(path, "/auth") {
		return false, "giriş sayfasına yönlendirildi"
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Sprintf("ana sayfa HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return false, "ana sayfa okunamadı"
	}
	if IsLoginPage(body) {
		return false, "giriş formu döndü"
	}
	if !strings.Contains(string(body), "/ogrenciler/") {
		return false, "öğrenci menüsü görünmedi"
	}
	return true, ""
}

// IsLoginPage, OİS'in oturum kapalıyken istenen adreste 200 ile döndürdüğü
// giriş sayfasını ve "oturumunuz farklı bir ekranda açıldı" uyarısını tanır.
func IsLoginPage(body []byte) bool {
	lower := strings.ToLower(string(body))
	if strings.Contains(lower, `name="kullanici_sifre"`) || strings.Contains(lower, `name='kullanici_sifre'`) {
		return true
	}
	return strings.Contains(lower, "farklı bir ekranda") || strings.Contains(lower, "farkli bir ekranda")
}
