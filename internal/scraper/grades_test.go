package scraper

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"notbot/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestFetchGradesRejectsSuccessfulPageWithoutGradeTable(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": {"text/html; charset=utf-8"},
			},
			Body:    io.NopCloser(strings.NewReader(`<html><body><h1>Bakım çalışması</h1></body></html>`)),
			Request: req,
		}, nil
	})}

	courses, err := FetchGrades(client, &config.Config{
		UniversityURL: "https://ois.example",
		UserAgent:     "test-agent",
	})

	if err == nil {
		t.Fatalf("expected an error for a page without a grade table, got courses=%v", courses)
	}
}

func TestParseGradesSplitsCourseGradeSummaryAndNormalizesWeight(t *testing.T) {
	html := []byte(`
		<html>
			<body>
				<table class="a4"><tr><td>ignored</td></tr></table>
				<table class="a4">
					<tr>
						<th>Etki Oranı</th>
						<th>1410121006 - Siber Güvenliğe Giriş<h3>A-Başarı Puanı: 86.00</h3></th>
						<th>Puan</th>
						<th>Açıklanma Tarihi</th>
					</tr>
					<tr>
						<td>(%20)</td>
						<td>Ödev</td>
						<td>85.00</td>
						<td>10/06/2026</td>
					</tr>
				</table>
			</body>
		</html>
	`)

	courses := parseGrades(html)
	if len(courses) != 1 {
		t.Fatalf("expected 1 course, got %d", len(courses))
	}

	course := courses[0]
	if course.Name != "Siber Güvenliğe Giriş" {
		t.Fatalf("unexpected course name: %q", course.Name)
	}
	if course.LetterGrade != "A-" {
		t.Fatalf("unexpected letter grade: %q", course.LetterGrade)
	}
	if course.SuccessScore != "86.00" {
		t.Fatalf("unexpected success score: %q", course.SuccessScore)
	}
	if course.DisplayName() != "Siber Güvenliğe Giriş (A-, Başarı Puanı: 86.00)" {
		t.Fatalf("unexpected display name: %q", course.DisplayName())
	}
	if got := course.Components[0].Weight; got != "%20" {
		t.Fatalf("unexpected normalized weight: %q", got)
	}
}

func TestParseGradesSplitsSingleLetterGradeWithoutDash(t *testing.T) {
	code, name, letterGrade, successScore := parseCourseText("1410121007 - Sayısal Yöntemler CBaşarı Puanı: 66.00")

	if code != "1410121007" {
		t.Fatalf("unexpected code: %q", code)
	}
	if name != "Sayısal Yöntemler" {
		t.Fatalf("unexpected name: %q", name)
	}
	if letterGrade != "C" {
		t.Fatalf("unexpected letter grade: %q", letterGrade)
	}
	if successScore != "66.00" {
		t.Fatalf("unexpected success score: %q", successScore)
	}
}

func TestFetchGradesFallsBackFromEmptySummerToSpringTerm(t *testing.T) {
	requestCount := 0
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requestCount++
		var body string
		switch requestCount {
		case 1:
			if req.Method != http.MethodGet {
				t.Fatalf("first request method = %s, want GET", req.Method)
			}
			body = `
				<form method="post" action="/ogrenciler/belge/ogrsinavsonuc/ogrenci_no/123">
					<input type="hidden" name="token" value="safe-token">
					<select name="sezon">
						<option value="2024-2025">2024-2025</option>
						<option value="2025-2026" selected>2025-2026</option>
					</select>
					<select name="donem">
						<option value="1">Güz</option>
						<option value="2">Bahar</option>
						<option value="3" selected>Yaz</option>
					</select>
				</form>
				<table class="a4"></table>`
		case 2:
			if req.Method != http.MethodPost {
				t.Fatalf("fallback request method = %s, want POST", req.Method)
			}
			if req.URL.Path != "/ogrenciler/belge/ogrsinavsonuc/ogrenci_no/123" {
				t.Fatalf("fallback path = %q", req.URL.Path)
			}
			posted, err := url.ParseQuery(readRequestBody(t, req))
			if err != nil {
				t.Fatalf("parse fallback form: %v", err)
			}
			want := url.Values{"sezon": {"2025-2026"}, "donem": {"2"}, "token": {"safe-token"}}
			if posted.Encode() != want.Encode() {
				t.Fatalf("fallback form = %q, want %q", posted.Encode(), want.Encode())
			}
			body = `
				<table class="a4">
					<tr><th>Etki Oranı</th><th>1410121006 - Siber Güvenlik</th><th>Puan</th><th>Açıklanma Tarihi</th></tr>
					<tr><td>%40</td><td>Final</td><td>85</td><td>18/08/2026</td></tr>
				</table>`
		default:
			t.Fatalf("unexpected request %d: %s %s", requestCount, req.Method, req.URL)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"text/html; charset=utf-8"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})}

	courses, err := FetchGrades(client, &config.Config{
		UniversityURL: "https://ois.example",
		UserAgent:     "test-agent",
	})
	if err != nil {
		t.Fatalf("FetchGrades returned error: %v", err)
	}
	if requestCount != 2 {
		t.Fatalf("request count = %d, want 2", requestCount)
	}
	if len(courses) != 1 || courses[0].Name != "Siber Güvenlik" {
		t.Fatalf("unexpected courses: %#v", courses)
	}
}

func TestFetchGradesSkipsCourseOnlyPeriodWithoutPublishedScores(t *testing.T) {
	requestCount := 0
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requestCount++
		var body string
		switch requestCount {
		case 1:
			body = `
				<form method="post" action="/ogrenciler/belge/ogrsinavsonuc/ogrenci_no/123">
					<select name="sezon">
						<option value="2025-2026" selected>2025-2026</option>
					</select>
					<select name="donem">
						<option value="1">Güz</option>
						<option value="2">Bahar</option>
						<option value="3" selected>Yaz</option>
					</select>
				</form>
				<table class="a4">
					<tr><th>Etki Oranı</th><th>YAZ101 - Yaz Dersi</th><th>Puan</th><th>Açıklanma Tarihi</th></tr>
				</table>`
		case 2:
			body = `
				<table class="a4">
					<tr><th>Etki Oranı</th><th>BAH101 - Bahar Dersi</th><th>Puan</th><th>Açıklanma Tarihi</th></tr>
					<tr><td>%40</td><td>Final</td><td>85</td><td>18/08/2026</td></tr>
				</table>`
		default:
			t.Fatalf("unexpected request %d: %s %s", requestCount, req.Method, req.URL)
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"text/html; charset=utf-8"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})}

	courses, err := FetchGrades(client, &config.Config{
		UniversityURL: "https://ois.example",
		UserAgent:     "test-agent",
	})
	if err != nil {
		t.Fatalf("FetchGrades returned error: %v", err)
	}
	if requestCount != 2 {
		t.Fatalf("request count = %d, want 2", requestCount)
	}
	if len(courses) != 1 || courses[0].Code != "BAH101" || courses[0].Components[0].Score != "85" {
		t.Fatalf("unexpected courses: %#v", courses)
	}
}

func TestFetchGradesReturnsOnlyCoursesAndComponentsWithPublishedGrades(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `
			<table class="a4">
				<tr><th>Etki Oranı</th><th>BOS101 - Notsuz Ders</th><th>Puan</th><th>Açıklanma Tarihi</th></tr>
				<tr><td>%40</td><td>Vize</td><td>-</td><td></td></tr>
				<tr><th>Etki Oranı</th><th>DOL101 - Notlu Ders</th><th>Puan</th><th>Açıklanma Tarihi</th></tr>
				<tr><td>%30</td><td>Ödev</td><td></td><td></td></tr>
				<tr><td>%70</td><td>Final</td><td>85</td><td>18/08/2026</td></tr>
			</table>`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"text/html; charset=utf-8"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})}

	courses, err := FetchGrades(client, &config.Config{
		UniversityURL: "https://ois.example",
		UserAgent:     "test-agent",
	})
	if err != nil {
		t.Fatalf("FetchGrades returned error: %v", err)
	}
	if len(courses) != 1 || courses[0].Code != "DOL101" {
		t.Fatalf("unexpected courses: %#v", courses)
	}
	if len(courses[0].Components) != 1 || courses[0].Components[0].Name != "Final" || courses[0].Components[0].Score != "85" {
		t.Fatalf("unexpected components: %#v", courses[0].Components)
	}
}

func readRequestBody(t *testing.T, req *http.Request) string {
	t.Helper()
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}
	return string(body)
}
