package main

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"notbot/config"
	"notbot/internal/auth"
	"notbot/internal/exams"
)

// fetchExamCalendar sınav takvimini okur; oturum kapanmışsa bir kez giriş yapıp yeniden dener.
func fetchExamCalendar(client *http.Client, cfg *config.Config, now time.Time) (string, error) {
	oisMu.Lock()
	defer oisMu.Unlock()
	list, err := exams.Fetch(client, cfg)
	if errors.Is(err, exams.ErrSessionExpired) {
		loggedIn, reason := tryLogin(client, cfg, auth.Login, time.Sleep)
		if !loggedIn {
			return "", fmt.Errorf("OİS girişi başarısız: %s (sınav takvimi hatası: %w)", reason, err)
		}
		list, err = exams.Fetch(client, cfg)
	}
	if err != nil {
		return "", err
	}
	return exams.Format(list, now), nil
}
