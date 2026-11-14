package main

import (
	"strings"
	"testing"
	"time"

	"notbot/internal/view"
)

func TestStayAwakeFollowsTheCalendar(t *testing.T) {
	istanbul := time.FixedZone("Istanbul", 3*3600)
	previous := view.Location
	view.Location = istanbul
	t.Cleanup(func() { view.Location = previous; courseSelectionOpen.Store(false); remindersActive.Store(false) })
	courseSelectionOpen.Store(false)
	remindersActive.Store(false)

	cases := []struct {
		at    time.Time
		awake bool
		want  string
	}{
		{time.Date(2026, 11, 14, 12, 0, 0, 0, istanbul), true, "15 Aralık'a kadar"},
		{time.Date(2026, 12, 20, 12, 0, 0, 0, istanbul), false, "28 Aralık'ta uyanır"},
		{time.Date(2027, 1, 20, 12, 0, 0, 0, istanbul), true, "8 Şubat'a kadar"},
		{time.Date(2027, 3, 1, 12, 0, 0, 0, istanbul), false, "sıradaki dönem yok"},
	}
	for _, c := range cases {
		awake, reason := stayAwake(c.at)
		if awake != c.awake || !strings.Contains(reason, c.want) {
			t.Fatalf("%s: awake=%v reason=%q, want awake=%v containing %q", c.at.Format("2006-01-02"), awake, reason, c.awake, c.want)
		}
	}

	courseSelectionOpen.Store(true)
	if awake, reason := stayAwake(time.Date(2026, 12, 20, 12, 0, 0, 0, istanbul)); !awake || !strings.Contains(reason, "Ders seçme açık") {
		t.Fatalf("open course selection must keep the bot awake: %v %q", awake, reason)
	}
	courseSelectionOpen.Store(false)
	remindersActive.Store(true)
	if awake, _ := stayAwake(time.Date(2026, 12, 20, 12, 0, 0, 0, istanbul)); !awake {
		t.Fatal("class reminders must keep the bot awake")
	}
}
