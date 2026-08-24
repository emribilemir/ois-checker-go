package scraper

import (
	"io"
	"net/http"
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

func TestSummarizeGradePageReportsStructureWithoutPageValues(t *testing.T) {
	body := []byte(`
		<html>
			<head><title>Emir Bilici - Sınav Sonuçları</title></head>
			<body>
				<form method="post" action="/ogrenciler/belge/ogrsinavsonuc">
					<select name="yil"><option value="2025">2025-2026</option><option value="2026" selected>2026-2027</option></select>
					<select name="donem"><option value="1">Güz</option><option value="2" selected>Bahar</option></select>
				</form>
				<table class="responsive results">
					<tr><th>Etki Oranı</th><th>240001 - Siber Güvenlik</th><th>Puan</th><th>Açıklanma Tarihi</th></tr>
					<tr><td>%40</td><td>Final</td><td>85</td><td>18/08/2026</td></tr>
				</table>
			</body>
		</html>
	`)

	got := summarizeGradePage(body)
	want := `tables=1 table[0]={class="responsive results",rows=2,max_th=4,max_td=4} forms=1 form[0]={method="post",action="/ogrenciler/belge/ogrsinavsonuc"} selects=2 select[0]={name="yil",options="2025,2026",selected="2026"} select[1]={name="donem",options="1,2",selected="2"} markers="Etki Oranı,Puan,Açıklanma Tarihi"`
	if got != want {
		t.Fatalf("unexpected summary:\nwant: %s\n got: %s", want, got)
	}

	for _, sensitive := range []string{"Emir Bilici", "240001", "Siber Güvenlik", "85", "18/08/2026"} {
		if strings.Contains(got, sensitive) {
			t.Fatalf("summary leaked page value %q: %s", sensitive, got)
		}
	}
}
