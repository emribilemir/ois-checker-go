// Package uptime UptimeRobot izlemesini botun uyanık kalma planına uydurur.
//
// UptimeRobot botun adresine 5 dakikada bir istek atarak Render durdurduğunda
// onu geri kaldırır. Ama her istek botu uyandırdığı için takvim dışında da bot
// hiç uyumaz. Bot bu yüzden uyanık kalmayacağı dönemde izlemeyi duraklatır,
// uyanık kalacağı dönemde yeniden açar (UptimeRobot v2 API, ana API anahtarı).
package uptime

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Durumlar UptimeRobot v2 API'sindeki izleme durumlarıdır.
const (
	StatusPaused = 0
)

// Client tek bir izlemeyi yönetir. APIKey boşsa hiçbir şey yapmaz.
type Client struct {
	APIKey     string
	MonitorURL string // izlemenin kontrol ettiği adres, ör. https://ois-checker-go.onrender.com/
	Base       string // test için; boşsa https://api.uptimerobot.com/v2
	HTTP       *http.Client

	mu        sync.Mutex
	monitorID int64
}

// Enabled istemcinin yapılandırılmış olduğunu söyler.
func (c *Client) Enabled() bool { return c != nil && c.APIKey != "" && c.MonitorURL != "" }

type monitor struct {
	ID     int64  `json:"id"`
	URL    string `json:"url"`
	Status int    `json:"status"`
}

type apiResponse struct {
	Stat     string    `json:"stat"`
	Monitors []monitor `json:"monitors"`
	Error    *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func (c *Client) post(method string, values url.Values) (apiResponse, error) {
	base := c.Base
	if base == "" {
		base = "https://api.uptimerobot.com/v2"
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	values.Set("api_key", c.APIKey)
	values.Set("format", "json")
	resp, err := client.PostForm(base+"/"+method, values)
	if err != nil {
		// Hata metni istek adresini içerir ama anahtar gövdede; yine de yalnız yöntemi yaz.
		return apiResponse{}, fmt.Errorf("uptimerobot %s: bağlantı hatası", method)
	}
	defer resp.Body.Close()
	var out apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return apiResponse{}, fmt.Errorf("uptimerobot %s: HTTP %d, cevap okunamadı", method, resp.StatusCode)
	}
	if out.Stat != "ok" {
		msg := "bilinmeyen hata"
		if out.Error != nil {
			msg = strings.TrimSpace(out.Error.Type + " " + out.Error.Message)
		}
		return apiResponse{}, fmt.Errorf("uptimerobot %s: %s", method, msg)
	}
	return out, nil
}

func sameURL(a, b string) bool {
	norm := func(s string) string { return strings.TrimRight(strings.ToLower(strings.TrimSpace(s)), "/") }
	return norm(a) == norm(b)
}

// Ensure izlemeyi active ise açık, değilse duraklatılmış hale getirir.
// changed, bir değişiklik yapıldığını söyler.
func (c *Client) Ensure(active bool) (changed bool, err error) {
	if !c.Enabled() {
		return false, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	res, err := c.post("getMonitors", url.Values{})
	if err != nil {
		return false, err
	}
	var target *monitor
	for i := range res.Monitors {
		if sameURL(res.Monitors[i].URL, c.MonitorURL) {
			target = &res.Monitors[i]
			break
		}
	}
	if target == nil {
		return false, fmt.Errorf("uptimerobot: %s adresini izleyen monitör bulunamadı", c.MonitorURL)
	}
	c.monitorID = target.ID
	paused := target.Status == StatusPaused
	if active != paused {
		return false, nil // zaten istenen durumda
	}
	status := "1" // devam et
	if !active {
		status = "0" // duraklat
	}
	if _, err := c.post("editMonitor", url.Values{"id": {fmt.Sprint(target.ID)}, "status": {status}}); err != nil {
		return false, err
	}
	return true, nil
}
