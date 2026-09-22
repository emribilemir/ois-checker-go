package schedule

import (
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"notbot/config"
)

const samplePage = `<html><body><table><tr><td>Kimlik bilgisi</td></tr></table>
<table border="1"><tr>
<td class="sutun_baslik">PAZARTESİ</td><td class="sutun_baslik">SALI</td><td class="sutun_baslik">ÇARŞAMBA</td>
<td class="sutun_baslik">PERŞEMBE</td><td class="sutun_baslik">CUMA</td><td class="sutun_baslik">CUMARTESİ</td><td class="sutun_baslik">PAZAR</td>
</tr><tr>
<td><table><tr><td class="bilgi_satir">1410221030<br>Veri Tabanı Sistemleri<br>Vadi Kampüs ONLİNE Online<br>09:00 - 12:45</td></tr><tr><td class="bilgi_satir">1410311021<br>İş Sağlığı ve Güvenliği III<br>Vadi Kampüs ONLINE Online<br>13:00 - 13:45</td></tr></table></td>
<td><table><tr><td class="bilgi_satir">1410311013<br>Bilgisayar Organizasyonu ve Mimarisi<br>Vadi Kampüs B3-09 3. Kat Tolkien 04<br>12:00 - 14:45</td></tr></table></td>
<td></td><td></td><td></td><td></td><td></td>
</tr></table><table><tr><td class="belge_satir">1410221030</td><td class="belge_satir">Veri Tabanı Sistemleri</td></tr></table></body></html>`

func TestParseReadsTimetableAndOnlineLocations(t *testing.T) {
	entries, err := Parse([]byte(samplePage))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected three scheduled meetings, got %d: %#v", len(entries), entries)
	}
	if entries[0].Day != time.Monday || entries[0].Start != "09:00" || entries[0].End != "12:45" || !entries[0].Online || entries[0].Name != "Veri Tabanı Sistemleri" {
		t.Fatalf("wrong first meeting: %#v", entries[0])
	}
	if entries[2].Day != time.Tuesday || entries[2].Online || !strings.Contains(entries[2].Location, "B3-09") {
		t.Fatalf("wrong classroom meeting: %#v", entries[2])
	}
}

func TestParseRejectsLoginPage(t *testing.T) {
	if _, err := Parse([]byte(`<html><form action="/login"></form></html>`)); err == nil {
		t.Fatal("expected a missing timetable to be reported as an error")
	}
}

func TestParseRecognizesTurkishOnlineLabelWithoutEnglishSuffix(t *testing.T) {
	body := strings.Replace(samplePage, "Vadi Kampüs ONLİNE Online", "Vadi Kampüs ONLİNE", 1)
	entries, err := Parse([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if !entries[0].Online {
		t.Fatalf("expected Turkish online label to be recognized: %#v", entries[0])
	}
}

func TestParseUsesDayHeadersForReminderDays(t *testing.T) {
	body := strings.Replace(samplePage, `>PAZARTESİ</td><td class="sutun_baslik">SALI<`, `>SALI</td><td class="sutun_baslik">PAZARTESİ<`, 1)
	entries, err := Parse([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if entries[0].Day != time.Monday || entries[0].Name != "Bilgisayar Organizasyonu ve Mimarisi" {
		t.Fatalf("reminder day did not follow the header: %#v", entries)
	}
}

func TestFormatWeekKeepsMobileReadableAndEscapesNames(t *testing.T) {
	entries := []Entry{{Day: time.Monday, Start: "09:00", End: "10:00", Name: "A&B <Lab>", Code: "123", Location: "Online", Online: true}, {Day: time.Tuesday, Start: "12:00", End: "13:00", Name: "Matematik", Location: "B3-09"}}
	message := FormatWeek(entries)
	for _, want := range []string{"<b>Pazartesi</b>", "09:00–10:00", "🌐", "A&amp;B &lt;Lab&gt;", "<b>Salı</b>", "B3-09", "<b>Cuma</b>\n— Ders yok"} {
		if !strings.Contains(message, want) {
			t.Fatalf("formatted schedule missing %q: %s", want, message)
		}
	}
	if strings.Contains(message, "<table") || strings.Contains(message, "<Lab>") {
		t.Fatalf("message is not Telegram-safe mobile text: %s", message)
	}
}

func TestDueRemindersOnlyWithinLeadWindowAndOncePerMeeting(t *testing.T) {
	location := time.FixedZone("Istanbul", 3*3600)
	now := time.Date(2026, 9, 21, 8, 46, 0, 0, location)
	entries := []Entry{{Day: time.Monday, Start: "09:00", End: "12:45", Code: "101", Name: "Online", Online: true}, {Day: time.Monday, Start: "13:00", End: "14:00", Code: "102", Name: "Later"}}
	due := Due(entries, now, 15*time.Minute, nil)
	if len(due) != 1 || due[0].Entry.Code != "101" || due[0].Key == "" {
		t.Fatalf("expected only imminent online class, got %#v", due)
	}
	if again := Due(entries, now, 15*time.Minute, map[string]bool{due[0].Key: true}); len(again) != 0 {
		t.Fatalf("expected sent meeting to be suppressed, got %#v", again)
	}
	if early := Due(entries, now.Add(-2*time.Minute), 15*time.Minute, nil); len(early) != 0 {
		t.Fatalf("expected no early reminder, got %#v", early)
	}
	if late := Due(entries, now.Add(15*time.Minute), 15*time.Minute, nil); len(late) != 0 {
		t.Fatalf("expected no reminder after start, got %#v", late)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestFetchUsesAuthenticatedProgramPage(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/ogrenciler/belge/ogrdersprogrami" || req.Header.Get("User-Agent") != "test-agent" {
			t.Errorf("unexpected program request: %s with user-agent %q", req.URL, req.Header.Get("User-Agent"))
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(samplePage)), Request: req}, nil
	})}
	entries, err := Fetch(client, &config.Config{UniversityURL: "https://ois.example", UserAgent: "test-agent"})
	if err != nil || len(entries) != 3 {
		t.Fatalf("expected live timetable, got %#v, %v", entries, err)
	}
}

func TestFetchRejectsRedirectedLoginPage(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Path = "/login"
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`<html>Login</html>`)), Request: req}, nil
	})}
	if _, err := Fetch(client, &config.Config{UniversityURL: "https://ois.example"}); err == nil {
		t.Fatal("expected expired session to be rejected")
	}
}

func TestReminderStatePersistsUserChoiceAndSentMeetings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json.schedule")
	if err := SaveState(path, State{Enabled: true, Sent: map[string]bool{"2026-09-21:09:00:101:Online": true}}); err != nil {
		t.Fatal(err)
	}
	state, err := LoadState(path)
	if err != nil || !state.Enabled || !state.Sent["2026-09-21:09:00:101:Online"] {
		t.Fatalf("state did not survive restart: %#v, %v", state, err)
	}
}
