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
func fetchExamCalendar(client *http.Client, cfg *config.Config) ([]exams.Exam, error) {
	oisMu.Lock()
	defer oisMu.Unlock()
	list, err := exams.Fetch(client, cfg)
	if errors.Is(err, exams.ErrSessionExpired) {
		loggedIn, reason := tryLogin(client, cfg, auth.Login, time.Sleep)
		if !loggedIn {
			return nil, fmt.Errorf("OİS girişi başarısız: %s (sınav takvimi hatası: %w)", reason, err)
		}
		list, err = exams.Fetch(client, cfg)
		if errors.Is(err, exams.ErrSessionExpired) {
			return nil, fmt.Errorf("giriş başarılı olduğu halde sınav programı sayfası OİS oturumunu kapattı; sayfa şu an öğrenciye kapalı olabilir")
		}
	}
	if err != nil {
		return nil, err
	}
	return list, nil
}
