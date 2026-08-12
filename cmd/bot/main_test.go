package main

import (
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"notbot/config"
	"notbot/internal/diff"
	"notbot/internal/scraper"
)

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
