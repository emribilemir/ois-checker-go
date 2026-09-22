package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"notbot/internal/schedule"
)

func TestScheduleServiceSendsOneReminderOnlyWhenEnabled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schedule.json")
	location := time.FixedZone("Istanbul", 3*3600)
	now := time.Date(2026, 9, 21, 8, 46, 0, 0, location)
	fetches := 0
	var messages []string
	service, err := newScheduleService(path,
		func() ([]schedule.Entry, error) {
			fetches++
			return []schedule.Entry{{Day: time.Monday, Code: "101", Name: "Veri Tabanı", Start: "09:00", End: "12:45", Location: "Online", Online: true}}, nil
		},
		func(message string) error { messages = append(messages, message); return nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Check(now); err != nil || len(messages) != 0 || fetches != 0 {
		t.Fatalf("disabled reminders should do nothing; messages=%v fetches=%d err=%v", messages, fetches, err)
	}
	if err := service.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	if err := service.Check(now); err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || !strings.Contains(messages[0], "Veri Tabanı") || !strings.Contains(messages[0], "09:00") || !strings.Contains(messages[0], "🌐") {
		t.Fatalf("expected one useful class reminder, got %v", messages)
	}
	if err := service.Check(now.Add(time.Minute)); err != nil || len(messages) != 1 {
		t.Fatalf("duplicate reminder sent: %v, %v", messages, err)
	}
	if err := service.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	if service.Enabled() {
		t.Fatal("expected reminders to be disabled")
	}
	state, err := schedule.LoadState(path)
	if err != nil || state.Enabled {
		t.Fatalf("disabled choice not persisted: %#v, %v", state, err)
	}
}

func TestScheduleServiceRetriesReminderAfterSendFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schedule.json")
	location := time.FixedZone("Istanbul", 3*3600)
	now := time.Date(2026, 9, 21, 8, 46, 0, 0, location)
	attempts := 0
	service, err := newScheduleService(path,
		func() ([]schedule.Entry, error) {
			return []schedule.Entry{{Day: time.Monday, Code: "101", Name: "Online", Start: "09:00", End: "10:00", Online: true}}, nil
		},
		func(string) error {
			attempts++
			if attempts == 1 {
				return errors.New("telegram unavailable")
			}
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	if err := service.Check(now); err == nil {
		t.Fatal("expected first send failure")
	}
	if err := service.Check(now.Add(time.Minute)); err != nil || attempts != 2 {
		t.Fatalf("expected failed reminder to retry, attempts=%d err=%v", attempts, err)
	}
}

func TestDisablingRemindersTakesEffectWhileProgramFetchIsPending(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schedule.json")
	started := make(chan struct{})
	release := make(chan struct{})
	sent := make(chan struct{}, 1)
	service, err := newScheduleService(path,
		func() ([]schedule.Entry, error) {
			close(started)
			<-release
			return []schedule.Entry{{Day: time.Monday, Code: "101", Name: "Online", Start: "09:00", End: "10:00", Online: true}}, nil
		},
		func(string) error { sent <- struct{}{}; return nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 21, 8, 46, 0, 0, time.FixedZone("Istanbul", 3*3600))
	done := make(chan error, 1)
	go func() { done <- service.Check(now) }()
	<-started
	disabled := make(chan error, 1)
	go func() { disabled <- service.SetEnabled(false) }()
	select {
	case err := <-disabled:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(200 * time.Millisecond):
		close(release)
		<-done
		t.Fatal("turning reminders off waited for the OIS request")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	select {
	case <-sent:
		t.Fatal("sent a reminder after the user turned alerts off")
	default:
	}
}
