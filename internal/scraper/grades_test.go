package scraper

import "testing"

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
