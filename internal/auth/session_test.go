package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"notbot/config"
)

func TestSessionActive(t *testing.T) {
	cases := []struct {
		name   string
		handle http.HandlerFunc
		active bool
	}{
		{"menu", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`<a href="/ogrenciler/belge/ogrsinavsonuc">Sınav Sonuçları</a>`))
		}, true},
		{"redirect", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/" {
				http.Redirect(w, r, "/auth/login", http.StatusFound)
				return
			}
			w.Write([]byte(`<form action="/auth/login"><input name="kullanici_sifre"></form>`))
		}, false},
		{"login form at same URL", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`<a href="/ogrenciler/x">x</a><form action="/auth/login"><input type="password" name="kullanici_sifre"></form>`))
		}, false},
		{"other screen", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`<a href="/ogrenciler/x">x</a> Oturumunuz farklı bir ekranda açıldı`))
		}, false},
		{"unknown page", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`<p>bakım</p>`)) }, false},
		{"server error", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "x", 500) }, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			server := httptest.NewServer(c.handle)
			defer server.Close()
			active, reason := SessionActive(server.Client(), &config.Config{UniversityURL: server.URL})
			if active != c.active || (!active && reason == "") {
				t.Fatalf("active=%v reason=%q, want active=%v", active, reason, c.active)
			}
		})
	}
}
