package main

import (
	"fmt"
	"html"
	"net/http"
	"strings"
	"sync"
	"time"

	"notbot/config"
	"notbot/internal/auth"
	"notbot/internal/schedule"
)

func remindersEnabled() bool { return remindersActive.Load() }

func fetchAuthenticatedSchedule(client *http.Client, cfg *config.Config) ([]schedule.Entry, error) {
	oisMu.Lock()
	defer oisMu.Unlock()
	entries, err := schedule.Fetch(client, cfg)
	if err == nil {
		return entries, nil
	}
	loggedIn, reason := tryLogin(client, cfg, auth.Login, time.Sleep)
	if !loggedIn {
		return nil, fmt.Errorf("OİS girişi başarısız: %s (program hatası: %w)", reason, err)
	}
	return schedule.Fetch(client, cfg)
}

type scheduleService struct {
	mu        sync.Mutex
	refreshMu sync.Mutex
	statePath string
	state     schedule.State
	entries   []schedule.Entry
	fetchedAt time.Time
	fetch     func() ([]schedule.Entry, error)
	send      func(string) error
}

func newScheduleService(statePath string, fetch func() ([]schedule.Entry, error), send func(string) error) (*scheduleService, error) {
	state, err := schedule.LoadState(statePath)
	if err != nil {
		return nil, err
	}
	return &scheduleService{statePath: statePath, state: state, fetch: fetch, send: send}, nil
}

func (s *scheduleService) Enabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.Enabled
}

func (s *scheduleService) SetEnabled(enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.state
	next.Enabled = enabled
	if err := schedule.SaveState(s.statePath, next); err != nil {
		return err
	}
	s.state = next
	return nil
}

func (s *scheduleService) Show() (string, error) {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	entries, err := s.fetch()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	s.entries = entries
	s.fetchedAt = time.Now()
	s.mu.Unlock()
	return schedule.FormatWeek(entries), nil
}

func (s *scheduleService) Check(now time.Time) error {
	s.mu.Lock()
	enabled := s.state.Enabled
	needsRefresh := s.fetchedAt.IsZero() || now.Sub(s.fetchedAt) >= 30*time.Minute
	s.mu.Unlock()
	if !enabled {
		return nil
	}
	if needsRefresh {
		s.refreshMu.Lock()
		s.mu.Lock()
		stillEnabled := s.state.Enabled
		needsRefresh = s.fetchedAt.IsZero() || now.Sub(s.fetchedAt) >= 30*time.Minute
		s.mu.Unlock()
		if stillEnabled && needsRefresh {
			entries, err := s.fetch()
			if err != nil {
				s.refreshMu.Unlock()
				return err
			}
			s.mu.Lock()
			if s.state.Enabled {
				s.entries = entries
				s.fetchedAt = now
			}
			s.mu.Unlock()
		}
		s.refreshMu.Unlock()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.state.Enabled {
		return nil
	}
	for key := range s.state.Sent {
		if !strings.HasPrefix(key, now.Format("2006-01-02")+":") {
			delete(s.state.Sent, key)
		}
	}
	for _, reminder := range schedule.Due(s.entries, now, 15*time.Minute, s.state.Sent) {
		message := reminderMessage(reminder.Entry)
		if err := s.send(message); err != nil {
			return err
		}
		s.state.Sent[reminder.Key] = true
		if err := schedule.SaveState(s.statePath, s.state); err != nil {
			return err
		}
	}
	return nil
}

func reminderMessage(entry schedule.Entry) string {
	place := entry.Location
	icon := "🏫"
	if entry.Online {
		icon = "🌐"
		place = "Online"
	}
	message := fmt.Sprintf("⏰ <b>Ders yakında başlıyor</b>\n\n%s <b>%s</b>\n🕒 %s–%s\n📍 %s", icon, html.EscapeString(entry.Name), entry.Start, entry.End, html.EscapeString(place))
	if entry.Instructor != "" {
		message += "\n👤 Hoca: " + html.EscapeString(entry.Instructor)
	}
	return message
}
