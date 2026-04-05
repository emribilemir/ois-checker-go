package session

import (
	"net/http"
	"net/http/cookiejar"
	"time"

	"golang.org/x/net/publicsuffix"
)

// New her çağrıda aynı cookie jar'ı koruyan bir client döner.
// Bu nesneyi uygulama boyunca TEK instance olarak kullanın.
func New(userAgent string) *http.Client {
	jar, _ := cookiejar.New(&cookiejar.Options{
		PublicSuffixList: publicsuffix.List,
	})
	return &http.Client{
		Jar:     jar,
		Timeout: 15 * time.Second,
		// Redirect'leri yakala — 302 → login sayfası = session expired
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 3 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
}
