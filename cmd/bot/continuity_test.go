package main

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"notbot/config"
	"notbot/internal/diff"
	"notbot/internal/scraper"
	"notbot/internal/telemetry"
)

var sampleCourses = []scraper.Course{
	{Code: "BM311", Name: "Bilgisayar Mimarisi", Components: []scraper.Component{{Name: "Ara Sınavlar", Weight: "%30", Score: "35.00"}}},
	{Code: "EKO101", Name: "Ekonomik Tercihler", Components: []scraper.Component{{Name: "Ara Sınavlar", Weight: "%40", Score: "65.00"}}},
}

func resetContinuity(t *testing.T) {
	t.Helper()
	continuityMu.Lock()
	knownMarks, lastAlive, lastAliveSaved = nil, time.Time{}, time.Time{}
	continuityMu.Unlock()
	previous := settingsStore
	settingsStore = nil
	t.Cleanup(func() {
		settingsStore = previous
		continuityMu.Lock()
		knownMarks, lastAlive, lastAliveSaved = nil, time.Time{}, time.Time{}
		continuityMu.Unlock()
	})
}

func TestMarksHideNamesAndScores(t *testing.T) {
	marks := marksOf(sampleCourses)
	joined := strings.Join(marks, " ")
	if len(marks) != 2 || strings.Contains(joined, "35.00") || strings.Contains(joined, "Mimari") || strings.Contains(joined, "BM311") || len(marks[0]) != 6 {
		t.Fatalf("unexpected marks %v", marks)
	}
}

func TestMissedChangesFindsGradesPublishedWhileDown(t *testing.T) {
	if got := missedChanges(sampleCourses, nil); got != nil {
		t.Fatal("without stored marks nothing may be reported (first run of the feature)")
	}
	stored := marksOf(sampleCourses[:1])
	now := append([]scraper.Course(nil), sampleCourses...)
	now[0].LetterGrade = "CC"
	got := missedChanges(now, stored)
	var names []string
	for _, c := range got {
		names = append(names, c.CourseCode+"/"+c.Component+"="+c.NewScore)
	}
	if strings.Join(names, ",") != "BM311/Harf notu=CC,EKO101/Ara Sınavlar=65.00" {
		t.Fatalf("unexpected missed changes %v", names)
	}
}

func TestFreshDiskAlertsGradesMissedWhileDown(t *testing.T) {
	resetContinuity(t)
	continuityMu.Lock()
	knownMarks = marksOf(sampleCourses[:1]) // önceki çalışmada yalnız Mimari notu vardı
	continuityMu.Unlock()
	calls := captureTelegramCalls(t)

	grades := `<table class="a4">
		<tr><th>Etki</th><th>BM311 - Bilgisayar Mimarisi</th><th>Puan</th><th>Tarih</th></tr><tr><td>%30</td><td>Ara Sınavlar</td><td>35.00</td><td>14/11/2026</td></tr>
		<tr><th>Etki</th><th>EKO101 - Ekonomik Tercihler</th><th>Puan</th><th>Tarih</th></tr><tr><td>%40</td><td>Ara Sınavlar</td><td>65.00</td><td>16/11/2026</td></tr>
	</table>`
	logins, courses, ok := runWithFakes(t, true, grades)
	if !ok || len(courses) != 2 || logins != 0 {
		t.Fatalf("cycle failed: ok=%v courses=%d logins=%d", ok, len(courses), logins)
	}
	var alert string
	for _, c := range *calls {
		if c.method == "sendRichMessage" && strings.Contains(c.body, "Yeni not") {
			alert = c.body
		}
	}
	if !strings.Contains(alert, "Ekonomik Tercihler") || strings.Contains(alert, "Bilgisayar Mimarisi") {
		t.Fatalf("expected an alert for the grade missed while down only, got %q", alert)
	}
	if got := storedMarks(); len(got) != 2 {
		t.Fatalf("marks not updated after the cycle: %v", got)
	}
}

func TestExistingStateFileDoesNotRealertMissedGrades(t *testing.T) {
	resetContinuity(t)
	continuityMu.Lock()
	knownMarks = marksOf(sampleCourses[:1])
	continuityMu.Unlock()
	stateFile := filepath.Join(t.TempDir(), "state.json")
	if _, _, err := diff.Check(sampleCourses, stateFile); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stateFile); err != nil {
		t.Fatal(err)
	}
	calls := captureTelegramCalls(t)
	client := gradeClient(cycleGradeTwoCourses)
	cfg := &config.Config{UniversityURL: "https://ois.example", UserAgent: "x", StateFile: stateFile}
	if _, ok, _ := runCycle(client, cfg); !ok {
		t.Fatal("cycle failed")
	}
	for _, c := range *calls {
		if strings.Contains(c.body, "Yeni not") {
			t.Fatalf("a known state must not re-alert: %s", c.body)
		}
	}
}

func TestHeartbeatSavesAtMostEveryTenMinutes(t *testing.T) {
	resetContinuity(t)
	start := time.Date(2026, 11, 16, 21, 0, 0, 0, time.UTC)
	heartbeat(start)
	heartbeat(start.Add(5 * time.Minute))
	continuityMu.Lock()
	first := lastAlive
	continuityMu.Unlock()
	if !first.Equal(start) {
		t.Fatalf("second beat within 10 min must not move lastAlive: %v", first)
	}
	heartbeat(start.Add(11 * time.Minute))
	continuityMu.Lock()
	defer continuityMu.Unlock()
	if !lastAlive.Equal(start.Add(11 * time.Minute)) {
		t.Fatalf("beat after 10 min must update: %v", lastAlive)
	}
}

func TestRecoverPanicKeepsTheProcessAliveAndReportsTheSite(t *testing.T) {
	telemetry.Reset()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer recoverPanic("test")
		var m map[string]int
		m["x"]++ // nil map: panic
	}()
	<-done
	snap := telemetry.Snapshot()
	if snap["counters"].(map[string]int64)["panic"] != 1 {
		t.Fatal("panic not counted")
	}
	text := snap["last"].(map[string]telemetry.Event)["panic"].Text
	if !strings.Contains(text, "test: assignment to entry in nil map") || !strings.Contains(text, "continuity_test.go:") {
		t.Fatalf("unexpected panic note %q", text)
	}
}

const cycleGradeTwoCourses = `<table class="a4">
	<tr><th>Etki</th><th>BM311 - Bilgisayar Mimarisi</th><th>Puan</th><th>Tarih</th></tr><tr><td>%30</td><td>Ara Sınavlar</td><td>35.00</td><td>14/11/2026</td></tr>
	<tr><th>Etki</th><th>EKO101 - Ekonomik Tercihler</th><th>Puan</th><th>Tarih</th></tr><tr><td>%40</td><td>Ara Sınavlar</td><td>65.00</td><td>16/11/2026</td></tr>
</table>`

func gradeClient(body string) *http.Client {
	return &http.Client{Transport: mainRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
}
