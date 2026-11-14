package main

import (
	"strings"
	"testing"
	"time"

	"notbot/config"
	"notbot/internal/notify"
	"notbot/internal/settings"
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

func withIstanbul(t *testing.T) *time.Location {
	t.Helper()
	istanbul := time.FixedZone("Istanbul", 3*3600)
	previous := view.Location
	view.Location = istanbul
	t.Cleanup(func() {
		view.Location = previous
		setChosenAwakeUntil(time.Time{})
		courseSelectionOpen.Store(false)
		remindersActive.Store(false)
	})
	setChosenAwakeUntil(time.Time{})
	courseSelectionOpen.Store(false)
	remindersActive.Store(false)
	return istanbul
}

func ymd(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func TestChosenEndExtendsAndShortensTheCalendar(t *testing.T) {
	ist := withIstanbul(t)
	noon := func(m time.Month, d int, y int) time.Time { return time.Date(y, m, d, 12, 0, 0, 0, ist) }

	// Uzatma: takvim 15 Aralık'ta bitiyor, seçim 22 Aralık.
	setChosenAwakeUntil(ymd(2026, 12, 22))
	if p := planAwake(noon(12, 20, 2026)); !p.Awake || !p.Chosen || !strings.Contains(p.Reason, "22 Aralık'a kadar") {
		t.Fatalf("extension not applied: %+v", p)
	}
	if p := planAwake(noon(12, 23, 2026)); p.Awake {
		t.Fatalf("must sleep after the chosen end: %+v", p)
	}
	if p := planAwake(noon(12, 28, 2026)); !p.Awake || p.Chosen {
		t.Fatalf("a calendar window starting after the choice must wake the bot: %+v", p)
	}

	// Kısaltma: takvim dönemi sürerken "bugün bitsin".
	setChosenAwakeUntil(ymd(2026, 11, 14))
	if p := planAwake(noon(11, 14, 2026)); !p.Awake {
		t.Fatal("the chosen last day is still awake")
	}
	if p := planAwake(noon(11, 15, 2026)); p.Awake || !strings.Contains(p.Reason, "28 Aralık'ta uyanır") {
		t.Fatalf("shortened window must sleep until the next one: %+v", p)
	}

	// Takvime dönüş.
	setChosenAwakeUntil(time.Time{})
	if p := planAwake(noon(11, 15, 2026)); !p.Awake || !strings.Contains(p.Reason, "15 Aralık'a kadar") {
		t.Fatalf("reset must restore the calendar: %+v", p)
	}
}

func TestExtendAwakeStartsFromTheCurrentEnd(t *testing.T) {
	ist := withIstanbul(t)
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, ist)
	if got := extendAwake(now, 7); !got.Equal(ymd(2026, 12, 22)) {
		t.Fatalf("15 Aralık + 1 hafta = 22 Aralık, got %s", got.Format("2006-01-02"))
	}
	asleep := time.Date(2026, 12, 20, 12, 0, 0, 0, ist)
	if got := extendAwake(asleep, 14); !got.Equal(ymd(2027, 1, 3)) {
		t.Fatalf("asleep: today + 14, got %s", got.Format("2006-01-02"))
	}
	if got := extendAwake(now, 1000); !got.Equal(ymd(2027, 3, 14)) {
		t.Fatalf("must be capped at %d days, got %s", maxAwakeAhead, got.Format("2006-01-02"))
	}
}

func TestParseAwakeDate(t *testing.T) {
	ist := withIstanbul(t)
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, ist)
	for in, want := range map[string]time.Time{
		"20.12": ymd(2026, 12, 20), "20/12/2026": ymd(2026, 12, 20), "2026-12-20": ymd(2026, 12, 20),
		"10.01": ymd(2027, 1, 10), // geçmişte kalan yılsız tarih gelecek yıl
	} {
		got, err := parseAwakeDate(in, now)
		if err != nil || !got.Equal(want) {
			t.Fatalf("%q -> %v %v, want %s", in, got, err, want.Format("2006-01-02"))
		}
	}
	for _, bad := range []string{"31.02", "yarın", "20.13"} {
		if _, err := parseAwakeDate(bad, now); err == nil {
			t.Fatalf("%q must be rejected", bad)
		}
	}
}

func TestAwakeButtonsAndCommandPersistTheChoice(t *testing.T) {
	withIstanbul(t)
	calls := captureTelegramCalls(t)
	b := &bot{cfg: &config.Config{TelegramToken: "token", TelegramChatID: "123"}, store: &settings.Store{Token: "token", ChatID: "123"}}

	b.handle(notify.Update{Command: "cmd_awake_7", ChatID: "123", CallbackID: "cb", MessageID: 9})
	if chosenAwakeUntil().IsZero() {
		t.Fatal("+1 hafta did not set a date")
	}
	var edited, saved bool
	for _, c := range *calls {
		edited = edited || (c.method == "editMessageText" && strings.Contains(c.body, "Uyanık kalma") && strings.Contains(c.body, "cmd_awake_reset"))
		saved = saved || (c.method == "sendMessage" && strings.Contains(c.body, "Uyanık kalma sonu"))
	}
	if !edited || !saved {
		t.Fatalf("expected an in-place redraw and a saved setting, got %#v", *calls)
	}

	*calls = nil
	b.handle(notify.Update{Command: "/uyanik", Text: "/uyanik takvim", ChatID: "123"})
	if !chosenAwakeUntil().IsZero() {
		t.Fatal("/uyanik takvim must clear the choice")
	}
	*calls = nil
	b.handle(notify.Update{Command: "/uyanik", Text: "/uyanik 31.02", ChatID: "123"})
	if len(*calls) == 0 || !strings.Contains((*calls)[0].body, "böyle bir gün yok") {
		t.Fatalf("bad date must be explained: %#v", *calls)
	}
}
