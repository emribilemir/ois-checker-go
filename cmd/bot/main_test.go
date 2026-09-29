package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"notbot/config"
	"notbot/internal/auth"
	"notbot/internal/diff"
	"notbot/internal/scraper"
)

func TestTryLoginStopsAfterFiveCaptchaFailures(t *testing.T) {
	attempts := 0
	login := func(*http.Client, *config.Config) (auth.LoginResult, error) {
		attempts++
		return auth.LoginResult{Reason: "invalid_captcha"}, nil
	}

	success, _ := tryLogin(&http.Client{}, &config.Config{}, login, func(time.Duration) {})
	if success {
		t.Fatal("expected login to fail")
	}
	if attempts != 5 {
		t.Fatalf("expected five login attempts, got %d", attempts)
	}
}

func TestTryLoginDoesNotRetryInvalidCredentials(t *testing.T) {
	attempts := 0
	login := func(*http.Client, *config.Config) (auth.LoginResult, error) {
		attempts++
		return auth.LoginResult{Reason: "invalid_credentials"}, nil
	}

	success, reason := tryLogin(&http.Client{}, &config.Config{}, login, func(time.Duration) {})
	if success {
		t.Fatal("expected login to fail")
	}
	if attempts != 1 {
		t.Fatalf("expected invalid credentials to stop immediately, got %d attempts", attempts)
	}
	if reason != "invalid_credentials" {
		t.Fatalf("expected invalid_credentials reason, got %q", reason)
	}
}

type mainRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn mainRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestRunAuthenticatedChecksCourseSelectionWhenGradesAreUnchanged(t *testing.T) {
	gradeHTML := `<html><body><table class="a4">
		<tr><th>Etki</th><th>101 - Test Dersi</th><th>Puan</th><th>Tarih</th></tr>
		<tr><td>%100</td><td>Final</td><td>80</td><td>01/08/2026</td></tr>
	</table></body></html>`
	stateFile := filepath.Join(t.TempDir(), "state.json")
	initialCourses := []scraper.Course{{
		Code: "101",
		Name: "Test Dersi",
		Components: []scraper.Component{{
			Weight: "%100", Name: "Final", Score: "80", Date: "01/08/2026",
		}},
	}}
	if _, _, err := diff.Check(initialCourses, stateFile); err != nil {
		t.Fatal(err)
	}

	rootRequests := 0
	client := &http.Client{Transport: mainRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := gradeHTML
		if req.URL.Path == "/" {
			rootRequests++
			body = `<html><body><nav>Öğrenci menüsü</nav></body></html>`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": {"text/html; charset=utf-8"},
			},
			Body:    io.NopCloser(strings.NewReader(body)),
			Request: req,
		}, nil
	})}
	cfg := &config.Config{
		UniversityURL: "https://ois.example",
		UserAgent:     "test-agent",
		StateFile:     stateFile,
	}

	previousActive := isDersSecmeActive
	previousNotified := dersSecmeNotified
	isDersSecmeActive = true
	dersSecmeNotified = false
	t.Cleanup(func() {
		isDersSecmeActive = previousActive
		dersSecmeNotified = previousNotified
	})

	courses, success := runAuthenticated(client, cfg)
	if !success || len(courses) != 1 {
		t.Fatalf("expected a successful grade cycle, success=%v courses=%v", success, courses)
	}
	if rootRequests != 1 {
		t.Fatalf("expected one course-selection request for unchanged grades, got %d", rootRequests)
	}
}

func TestUnavailableGradesMessageIncludesLastFailure(t *testing.T) {
	recordCheckFailure("not sayfasında beklenen tablo bulunamadı")

	message := gradesUnavailableMessage()
	if !strings.Contains(message, "not sayfasında beklenen tablo bulunamadı") {
		t.Fatalf("expected the latest failure in the user-facing status, got %q", message)
	}
}

func TestRunAuthenticatedChecksCourseSelectionWhenGradePageIsInvalid(t *testing.T) {
	rootRequests := 0
	client := &http.Client{Transport: mainRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `<html><body><h1>Geçici not sayfası hatası</h1></body></html>`
		if req.URL.Path == "/" {
			rootRequests++
			body = `<html><body><nav>Öğrenci menüsü</nav></body></html>`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": {"text/html; charset=utf-8"},
			},
			Body:    io.NopCloser(strings.NewReader(body)),
			Request: req,
		}, nil
	})}
	cfg := &config.Config{
		UniversityURL: "https://ois.example",
		UserAgent:     "test-agent",
		StateFile:     filepath.Join(t.TempDir(), "state.json"),
	}

	previousActive := isDersSecmeActive
	previousNotified := dersSecmeNotified
	isDersSecmeActive = true
	dersSecmeNotified = false
	t.Cleanup(func() {
		isDersSecmeActive = previousActive
		dersSecmeNotified = previousNotified
	})

	_, success := runAuthenticated(client, cfg)
	if success {
		t.Fatal("expected the invalid grade page to fail the grade cycle")
	}
	if rootRequests != 1 {
		t.Fatalf("expected course selection to be checked despite the grade error, got %d requests", rootRequests)
	}
}

func TestRunAuthenticatedKeepsCourseSelectionMonitoringWhileGradesArePaused(t *testing.T) {
	rootRequests := 0
	gradeRequests := 0
	client := &http.Client{Transport: mainRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `<html><body><nav>Öğrenci menüsü</nav></body></html>`
		if req.URL.Path == "/" {
			rootRequests++
		} else if req.URL.Path == "/ogrenciler/belge/ogrsinavsonuc" {
			gradeRequests++
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"text/html; charset=utf-8"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})}

	previousPaused := isPaused
	previousActive := isDersSecmeActive
	isPaused = true
	isDersSecmeActive = true
	t.Cleanup(func() {
		isPaused = previousPaused
		isDersSecmeActive = previousActive
	})

	courses, success := runAuthenticated(client, &config.Config{
		UniversityURL: "https://ois.example",
		UserAgent:     "test-agent",
		StateFile:     filepath.Join(t.TempDir(), "state.json"),
	})
	if !success || courses != nil {
		t.Fatalf("expected a successful course-only cycle, success=%v courses=%v", success, courses)
	}
	if rootRequests != 1 {
		t.Fatalf("expected course-selection monitoring while paused, got %d root requests", rootRequests)
	}
	if gradeRequests != 0 {
		t.Fatalf("expected paused grade checks to make no grade requests, got %d", gradeRequests)
	}
}

func TestRunDersSecmeCheckKeepsMonitoringAfterClassClosedNotice(t *testing.T) {
	rootRequests := 0
	client := &http.Client{Transport: mainRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		rootRequests++
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": {"text/html; charset=utf-8"},
			},
			Body: io.NopCloser(strings.NewReader(
				`<main><h2>Ders Seçme</h2><div>Sizin sınıfınız için ders seçme işlemleri kapalı</div></main>`,
			)),
			Request: req,
		}, nil
	})}
	cfg := &config.Config{
		UniversityURL: "https://ois.example",
		UserAgent:     "test-agent",
	}

	previousActive := isDersSecmeActive
	previousNotified := dersSecmeNotified
	isDersSecmeActive = true
	dersSecmeNotified = false
	t.Cleanup(func() {
		isDersSecmeActive = previousActive
		dersSecmeNotified = previousNotified
	})

	runDersSecmeCheck(client, cfg)
	runDersSecmeCheck(client, cfg)

	if rootRequests != 2 {
		t.Fatalf("expected monitoring to keep checking after a closed notice, got %d requests", rootRequests)
	}
	if !isDersSecmeActive {
		t.Fatal("expected course-selection monitoring to remain enabled")
	}
	if dersSecmeNotified {
		t.Fatal("expected no active-course-selection notification state")
	}
}

func TestRunDersSecmeCheckRetriesNotificationAfterTelegramFailure(t *testing.T) {
	oisClient := &http.Client{Transport: mainRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `<html><body><a href="/ogrenciler/derssecme/ogrindex">Ders Seçme</a></body></html>`
		if req.URL.Path == "/ogrenciler/derssecme/ogrindex" {
			body = `<html><body><h1>Ders Seçme</h1><script>function dersiAl(){}; var url="/ogrenciler/derssecme/ogrderskaydet";</script></body></html>`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"text/html; charset=utf-8"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})}

	telegramAttempts := 0
	previousTransport := http.DefaultTransport
	http.DefaultTransport = mainRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		telegramAttempts++
		status := http.StatusInternalServerError
		if telegramAttempts > 1 {
			status = http.StatusOK
		}
		return &http.Response{
			StatusCode: status,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    req,
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = previousTransport })

	previousActive := isDersSecmeActive
	previousNotified := dersSecmeNotified
	isDersSecmeActive = true
	dersSecmeNotified = false
	t.Cleanup(func() {
		isDersSecmeActive = previousActive
		dersSecmeNotified = previousNotified
	})

	cfg := &config.Config{
		UniversityURL:  "https://ois.example",
		UserAgent:      "test-agent",
		TelegramToken:  "token",
		TelegramChatID: "123",
	}
	runDersSecmeCheck(oisClient, cfg)
	if dersSecmeNotified {
		t.Fatal("expected a failed Telegram delivery to remain unnotified")
	}
	runDersSecmeCheck(oisClient, cfg)
	if telegramAttempts != 2 {
		t.Fatalf("expected the next check to retry Telegram delivery, got %d attempts", telegramAttempts)
	}
	if !dersSecmeNotified {
		t.Fatal("expected the successful retry to mark the alert as delivered")
	}
}

func TestRunDersSecmeCheckNotifiesWhenOISAddsACourseToTheSelectedList(t *testing.T) {
	selectedRows := `<tr><td>SEC101</td><td>Kolay Seçmeli</td><td>3</td><td>5</td><td><input value="Dersi Sil" onclick="dersiSil(1,2,0)"></td></tr>`
	oisClient := &http.Client{Transport: mainRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `<html><body><a href="/ogrenciler/derssecme/ogrindex">Ders Seçme</a></body></html>`
		if req.URL.Path == "/ogrenciler/derssecme/ogrindex" {
			body = `<html><body><script>function dersiAl(){}; var url="/ogrenciler/derssecme/ogrderskaydet";</script>` +
				`<table><tr><th>Seçtiğiniz Dersler</th></tr>` + selectedRows + `</table></body></html>`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"text/html; charset=utf-8"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})}

	var telegramMessages []string
	previousTransport := http.DefaultTransport
	http.DefaultTransport = mainRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if err := req.ParseForm(); err != nil {
			t.Fatalf("parse Telegram form: %v", err)
		}
		telegramMessages = append(telegramMessages, req.Form.Get("text"))
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    req,
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = previousTransport })

	previousActive := isDersSecmeActive
	previousNotified := dersSecmeNotified
	isDersSecmeActive = true
	dersSecmeNotified = false
	t.Cleanup(func() {
		isDersSecmeActive = previousActive
		dersSecmeNotified = previousNotified
	})

	cfg := &config.Config{
		UniversityURL:  "https://ois.example",
		UserAgent:      "test-agent",
		TelegramToken:  "token",
		TelegramChatID: "123",
		StateFile:      filepath.Join(t.TempDir(), "state.json"),
	}
	runDersSecmeCheck(oisClient, cfg)
	if len(telegramMessages) != 1 || !strings.Contains(telegramMessages[0], "SEC101") || !strings.Contains(telegramMessages[0], "Kolay Seçmeli") {
		t.Fatalf("expected the opening alert to include the already selected courses, got %#v", telegramMessages)
	}

	selectedRows += `<tr><td>AUTO202</td><td>Sistem Tarafından Eklenen Ders</td><td>3</td><td>5</td><td><input value="Dersi Sil" onclick="dersiSil(3,4,0)"></td></tr>`
	runDersSecmeCheck(oisClient, cfg)

	if len(telegramMessages) != 2 {
		t.Fatalf("expected an opening alert and a selected-course change alert, got %d messages: %#v", len(telegramMessages), telegramMessages)
	}
	changeMessage := telegramMessages[1]
	if !strings.Contains(changeMessage, "AUTO202") || !strings.Contains(changeMessage, "Sistem Tarafından Eklenen Ders") {
		t.Fatalf("expected the newly selected course in the alert, got %q", changeMessage)
	}
	if !strings.Contains(changeMessage, "ayırt edilemiyor") {
		t.Fatalf("expected an honest source-attribution warning, got %q", changeMessage)
	}
}

func TestRunDersSecmeCheckAlertsOnNewElectiveWithoutPostingToOIS(t *testing.T) {
	const poolPath = "/ogrenciler/derssecme/popderssecme/havuz_id/319/ogrenci_slot_id/555925"
	poolRows := `<tr><td>SEC101</td><td>Siber Güvenlik</td><td>3</td><td>5</td><td>2</td><td><input value="Dersi Al"></td></tr>` +
		`<tr><td>SEC999</td><td>Zaten Seçili</td><td>3</td><td>5</td><td>8</td><td><input value="Dersi Al"></td></tr>`
	oisClient := &http.Client{Transport: mainRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet {
			t.Fatalf("bot attempted mutation: %s %s", req.Method, req.URL)
		}
		body := `<a href="/ogrenciler/derssecme/ogrindex">Ders Seçme</a>`
		switch req.URL.Path {
		case "/ogrenciler/derssecme/ogrindex":
			body = `<script>function dersiAl(){}; var url="/ogrenciler/derssecme/ogrderskaydet";</script>` +
				`<table><tr><th>Seçtiğiniz Dersler</th></tr><tr><td>SEC999</td><td>Zaten Seçili</td></tr></table>`
		case poolPath:
			body = `<table><tr><th>Ders Kodu</th><th>Ders Adı</th><th>Kredi</th><th>AKTS</th><th>Kalan Kota</th><th></th></tr>` + poolRows + `</table>`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	var messages []string
	previousTransport := http.DefaultTransport
	http.DefaultTransport = mainRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if err := req.ParseForm(); err != nil {
			t.Fatal(err)
		}
		messages = append(messages, req.Form.Get("text"))
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"ok":true}`)), Request: req}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = previousTransport })
	previousActive, previousNotified := isDersSecmeActive, dersSecmeNotified
	isDersSecmeActive, dersSecmeNotified = true, false
	t.Cleanup(func() { isDersSecmeActive, dersSecmeNotified = previousActive, previousNotified })
	cfg := &config.Config{UniversityURL: "https://ois.example", UserAgent: "test-agent", TelegramToken: "token", TelegramChatID: "123", StateFile: filepath.Join(t.TempDir(), "state.json"), ElectivePoolPaths: []string{poolPath}}
	runDersSecmeCheck(oisClient, cfg)
	if len(messages) != 2 || !strings.Contains(messages[1], "SEC101") || strings.Contains(messages[1], "SEC999") || !strings.Contains(messages[1], "online bilgisi") {
		t.Fatalf("expected initial elective alert with honest online status, got %#v", messages)
	}
	runDersSecmeCheck(oisClient, cfg)
	if len(messages) != 2 {
		t.Fatalf("unchanged pool produced duplicate notification: %#v", messages)
	}
	poolRows += `<tr><td>SEC102</td><td>Yeni Seçmeli A&amp;B &lt;X&gt;</td><td>3</td><td>5</td><td>4</td><td><input value="Dersi Al"></td></tr>`
	runDersSecmeCheck(oisClient, cfg)
	if len(messages) != 3 || !strings.Contains(messages[2], "SEC102") || !strings.Contains(messages[2], "A&amp;B &lt;X&gt;") || strings.Contains(messages[2], "SEC101") {
		t.Fatalf("expected only newly available course, got %#v", messages)
	}
}

func TestHealthHandlerRejectsAStalledPollingLoop(t *testing.T) {
	statusMu.Lock()
	previousCheckAt := lastCheckAt
	lastCheckAt = time.Now().Add(-11 * time.Minute)
	statusMu.Unlock()
	t.Cleanup(func() {
		statusMu.Lock()
		lastCheckAt = previousCheckAt
		statusMu.Unlock()
	})

	recorder := httptest.NewRecorder()
	healthHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected a stalled poller to report 503, got %d with body %q", recorder.Code, recorder.Body.String())
	}
}

func TestHealthHandlerAcceptsARecentlyCompletedPollingLoop(t *testing.T) {
	statusMu.Lock()
	previousCheckAt := lastCheckAt
	lastCheckAt = time.Now().Add(-time.Minute)
	statusMu.Unlock()
	t.Cleanup(func() {
		statusMu.Lock()
		lastCheckAt = previousCheckAt
		statusMu.Unlock()
	})

	recorder := httptest.NewRecorder()
	healthHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected a live poller to report 200, got %d with body %q", recorder.Code, recorder.Body.String())
	}
}
