package config

import "testing"

func TestLoadDefaultsCourseSelectionTrackingToActive(t *testing.T) {
	t.Setenv("UNIVERSITY_URL", "https://ois.example")
	t.Setenv("UNIVERSITY_USER", "user")
	t.Setenv("UNIVERSITY_PASS", "pass")
	t.Setenv("TELEGRAM_TOKEN", "token")
	t.Setenv("TELEGRAM_CHAT_ID", "123")
	t.Setenv("DERS_SECME_ACTIVE", "")

	cfg := Load()
	if !cfg.DersSecmeActive {
		t.Fatal("expected course-selection tracking to default to active")
	}
}
