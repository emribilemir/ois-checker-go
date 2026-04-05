package auth

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/html"

	"notbot/config"
	"notbot/internal/captcha"
)

type LoginResult struct {
	Success bool
	Reason  string
}

func Login(client *http.Client, cfg *config.Config) (LoginResult, error) {
	// 1. Login sayfasını çek — hidden token'ları al
	loginPageURL := cfg.UniversityURL + "/auth/login"
	req, _ := http.NewRequest("GET", loginPageURL, nil)
	req.Header.Set("User-Agent", cfg.UserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return LoginResult{}, fmt.Errorf("login sayfası GET: %w", err)
	}
	defer resp.Body.Close()

	// Redirect sonrası gerçek URL'i al (örn: /auth/login → /auth/login/ln/tr)
	actualLoginURL := resp.Request.URL.String()
	baseURL := resp.Request.URL.Scheme + "://" + resp.Request.URL.Host
	log.Printf("[login] GET %s → status=%d, actual=%s", loginPageURL, resp.StatusCode, actualLoginURL)

	body, _ := io.ReadAll(resp.Body)

	tokens := parseHiddenFields(body)
	log.Printf("[login] hidden fields: %v", tokens)

	// 2. CAPTCHA görselini indir — mutlaka scheme+host'tan oluştur
	captchaURL := baseURL + "/auth/captcha"

	imgReq, _ := http.NewRequest("GET", captchaURL, nil)
	imgReq.Header.Set("User-Agent", cfg.UserAgent)
	imgReq.Header.Set("Referer", actualLoginURL)
	imgReq.Header.Set("Accept", "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8")
	imgResp, err := client.Do(imgReq)
	if err != nil {
		return LoginResult{}, fmt.Errorf("captcha indir: %w", err)
	}
	defer imgResp.Body.Close()
	imgBytes, _ := io.ReadAll(imgResp.Body)
	contentType := imgResp.Header.Get("Content-Type")
	log.Printf("[login] captcha: %d bytes, type=%s, url=%s", len(imgBytes), contentType, imgResp.Request.URL.String())
	if !strings.Contains(contentType, "image") {
		preview := string(imgBytes)
		if len(preview) > 500 {
			preview = preview[:500]
		}
		log.Printf("[login] captcha HTML döndü: %s", preview)
		return LoginResult{Reason: "captcha_not_image"}, nil
	}

	// 3. OCR
	captchaText, err := captcha.Solve(imgBytes)
	if err != nil {
		log.Printf("[login] OCR hata: %v", err)
		return LoginResult{Reason: "ocr_fail"}, nil
	}
	log.Printf("[login] OCR sonuç: %q (len=%d)", captchaText, len(captchaText))
	if len(captchaText) < 4 {
		return LoginResult{Reason: "ocr_fail_short"}, nil
	}

	// 4. Login POST — gerçek login URL'ine gönder
	formData := url.Values{}
	formData.Set("kullanici_adi", cfg.Username)
	formData.Set("kullanici_sifre", cfg.Password)
	formData.Set("captcha", captchaText)
	for k, v := range tokens {
		formData.Set(k, v)
	}

	postReq, _ := http.NewRequest("POST", actualLoginURL,
		strings.NewReader(formData.Encode()))
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReq.Header.Set("User-Agent", cfg.UserAgent)
	postReq.Header.Set("Referer", actualLoginURL)

	postResp, err := client.Do(postReq)
	if err != nil {
		return LoginResult{}, fmt.Errorf("login POST: %w", err)
	}
	defer postResp.Body.Close()

	finalURL := postResp.Request.URL.String()
	postBody, _ := io.ReadAll(postResp.Body)
	log.Printf("[login] POST → status=%d, final=%s", postResp.StatusCode, finalURL)

	// Hata mesajı arama (alert, error, hata gibi anahtar kelimeler)
	bodyStr := string(postBody)
	if strings.Contains(bodyStr, "alert") || strings.Contains(bodyStr, "hata") || strings.Contains(bodyStr, "error") {
		// Alert div'inin içeriğini bul
		for _, marker := range []string{"alert-danger", "alert-warning", "hata"} {
			if idx := strings.Index(bodyStr, marker); idx > -1 {
				end := idx + 300
				if end > len(bodyStr) {
					end = len(bodyStr)
				}
				log.Printf("[login] server mesajı: ...%s...", bodyStr[idx:end])
				break
			}
		}
	}

	// Başarı tespiti: login sayfasından ayrıldıysa başarılı
	if !strings.Contains(postResp.Request.URL.Path, "login") {
		return LoginResult{Success: true}, nil
	}
	return LoginResult{Reason: "wrong_credentials_or_captcha"}, nil
}

// parseHiddenFields sayfadaki tüm hidden input'ları map olarak döner.
func parseHiddenFields(body []byte) map[string]string {
	fields := map[string]string{}
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return fields
	}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "input" {
			attrs := map[string]string{}
			for _, a := range n.Attr {
				attrs[a.Key] = a.Val
			}
			if attrs["type"] == "hidden" && attrs["name"] != "" {
				fields[attrs["name"]] = attrs["value"]
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return fields
}

// parseCaptchaURL artık kullanılmıyor — CAPTCHA path'i sabit: /auth/captcha
