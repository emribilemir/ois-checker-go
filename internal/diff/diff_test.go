package diff

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"notbot/internal/scraper"
)

func TestCheckDoesNotReportDuplicateComponentAsChanged(t *testing.T) {
	courses := []scraper.Course{{
		Code: "SEC101",
		Name: "Siber Güvenliğe Giriş",
		Components: []scraper.Component{
			{Name: "Ödev", Weight: "%20", Score: "85.00"},
			{Name: "Ödev", Weight: "%20", Score: "95.00"},
		},
	}}

	stateFile := filepath.Join(t.TempDir(), "state.json")
	if err := saveState(stateFile, State{Hash: "stale-hash", Courses: courses}); err != nil {
		t.Fatal(err)
	}

	changed, changes, err := Check(courses, stateFile)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatalf("expected no change, got %#v", changes)
	}
}

func TestCheckIgnoresReorderedDuplicateComponentsWithSameScores(t *testing.T) {
	previous := []scraper.Course{{
		Code: "SEC101",
		Name: "Siber Güvenliğe Giriş",
		Components: []scraper.Component{
			{Name: "Ödev", Weight: "%20", Score: "85.00"},
			{Name: "Ödev", Weight: "%20", Score: "95.00"},
		},
	}}
	current := []scraper.Course{{
		Code: "SEC101",
		Name: "Siber Güvenliğe Giriş",
		Components: []scraper.Component{
			{Name: "Ödev", Weight: "%20", Score: "95.00"},
			{Name: "Ödev", Weight: "%20", Score: "85.00"},
		},
	}}

	stateFile := filepath.Join(t.TempDir(), "state.json")
	if err := saveState(stateFile, State{Hash: "stale-hash", Courses: previous}); err != nil {
		t.Fatal(err)
	}

	changed, changes, err := Check(current, stateFile)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatalf("expected no change, got %#v", changes)
	}
}

func TestCheckReportsOnlyNewDuplicateOccurrence(t *testing.T) {
	previous := []scraper.Course{{
		Code: "SEC101",
		Name: "Siber Güvenliğe Giriş",
		Components: []scraper.Component{
			{Name: "Ödev", Weight: "(%20)", Score: "85.00"},
		},
	}}
	current := []scraper.Course{{
		Code: "SEC101",
		Name: "Siber Güvenliğe Giriş",
		Components: []scraper.Component{
			{Name: "Ödev", Weight: "%20", Score: "85.00"},
			{Name: "Ödev", Weight: "%20", Score: "95.00"},
		},
	}}

	stateFile := filepath.Join(t.TempDir(), "state.json")
	if err := saveState(stateFile, State{Hash: "stale-hash", Courses: previous}); err != nil {
		t.Fatal(err)
	}

	changed, changes, err := Check(current, stateFile)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected a change")
	}
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %#v", changes)
	}
	if changes[0].Type != "new_score" || changes[0].NewScore != "95.00" {
		t.Fatalf("unexpected change: %#v", changes[0])
	}
}

func TestFormatMessageDoesNotDoubleWrapWeight(t *testing.T) {
	message := FormatMessage([]Change{{
		Type:       "new_score",
		CourseCode: "SEC101",
		CourseName: "Siber Güvenliğe Giriş (A-, Başarı Puanı: 86.00)",
		Component:  "Ödev",
		Weight:     "(%20)",
		NewScore:   "85.00",
	}})

	if strings.Contains(message, "((%20))") {
		t.Fatalf("message double-wrapped weight: %s", message)
	}
	if !strings.Contains(message, "Ödev (%20):") {
		t.Fatalf("message did not include normalized weight: %s", message)
	}
}

func TestCheckSavesNormalizedCurrentState(t *testing.T) {
	previous := []scraper.Course{{
		Code:       "MATH101",
		Name:       "Matematik II",
		Components: []scraper.Component{{Name: "Quiz", Weight: "(%15)", Score: "38.00"}},
	}}
	current := []scraper.Course{{
		Code:       "MATH101",
		Name:       "Matematik II",
		Components: []scraper.Component{{Name: "Quiz", Weight: "%15", Score: "38.00"}},
	}}

	stateFile := filepath.Join(t.TempDir(), "state.json")
	if err := saveState(stateFile, State{Hash: "stale-hash", Courses: previous}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Check(current, stateFile); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "(%15)") {
		t.Fatalf("state was not updated with normalized weight: %s", string(data))
	}
}
