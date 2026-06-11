package diff

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"notbot/internal/scraper"
)

type State struct {
	Hash    string           `json:"hash"`
	Courses []scraper.Course `json:"courses"`
}

type Change struct {
	Type       string // "new_score", "score_change", "new_course"
	CourseCode string
	CourseName string
	Component  string
	Weight     string
	OldScore   string
	NewScore   string
	Date       string
}

// Check önceki state ile karşılaştırır.
// changed=true ise değişiklik listesi döner.
func Check(courses []scraper.Course, stateFile string) (changed bool, changes []Change, err error) {
	current := hashCourses(courses)

	prev, err := loadState(stateFile)
	if err != nil {
		// İlk çalışma — state yaz, bildirim gönderme
		return false, nil, saveState(stateFile, State{Hash: current, Courses: courses})
	}

	if prev.Hash == current {
		return false, nil, nil
	}

	// Önceki dersleri map'le
	prevMap := make(map[string]scraper.Course)
	for _, c := range prev.Courses {
		prevMap[c.Code] = c
	}

	for _, course := range courses {
		prevCourse, exists := prevMap[course.Code]

		if !exists {
			// Yeni ders eklendi
			if len(course.Components) > 0 {
				for _, comp := range course.Components {
					changes = append(changes, Change{
						Type:       "new_score",
						CourseCode: course.Code,
						CourseName: course.DisplayName(),
						Component:  comp.Name,
						Weight:     comp.Weight,
						NewScore:   comp.Score,
						Date:       comp.Date,
					})
				}
			} else {
				changes = append(changes, Change{
					Type:       "new_course",
					CourseCode: course.Code,
					CourseName: course.DisplayName(),
				})
			}
			continue
		}

		changes = append(changes, compareComponents(course, prevCourse.Components)...)
	}

	_ = saveState(stateFile, State{Hash: current, Courses: courses})

	if len(changes) == 0 {
		// Hash farklı ama anlamlı değişiklik yok (yapısal değişiklik vb.)
		return false, nil, nil
	}
	return true, changes, nil
}

func hashCourses(courses []scraper.Course) string {
	b, _ := json.Marshal(courses)
	return fmt.Sprintf("%x", sha256.Sum256(b))
}

type componentSlot struct {
	comp scraper.Component
	used bool
}

func compareComponents(course scraper.Course, prevComponents []scraper.Component) []Change {
	prevGroups := groupComponents(prevComponents)
	currentGroups := groupComponents(course.Components)

	var changes []Change
	for _, baseKey := range componentGroupOrder(course.Components) {
		prevSlots := prevGroups[baseKey]
		currentSlots := currentGroups[baseKey]
		currentUsed := make([]bool, len(currentSlots))

		// Önce aynı skorları eşleştir: OIS aynı isim/ağırlıktaki satırları farklı
		// sırada döndürürse sahte değişiklik üretmeyelim.
		for currentIndex, current := range currentSlots {
			for prevIndex := range prevSlots {
				if prevSlots[prevIndex].used {
					continue
				}
				if prevSlots[prevIndex].comp.Score == current.comp.Score {
					prevSlots[prevIndex].used = true
					currentUsed[currentIndex] = true
					break
				}
			}
		}

		for currentIndex, current := range currentSlots {
			if currentUsed[currentIndex] {
				continue
			}

			prevIndex := firstUnusedComponent(prevSlots)
			if prevIndex < 0 {
				changes = append(changes, Change{
					Type:       "new_score",
					CourseCode: course.Code,
					CourseName: course.DisplayName(),
					Component:  current.comp.Name,
					Weight:     current.comp.Weight,
					NewScore:   current.comp.Score,
					Date:       current.comp.Date,
				})
				continue
			}

			prev := prevSlots[prevIndex]
			prevSlots[prevIndex].used = true
			changes = append(changes, Change{
				Type:       "score_change",
				CourseCode: course.Code,
				CourseName: course.DisplayName(),
				Component:  current.comp.Name,
				Weight:     current.comp.Weight,
				OldScore:   prev.comp.Score,
				NewScore:   current.comp.Score,
				Date:       current.comp.Date,
			})
		}
	}
	return changes
}

func groupComponents(components []scraper.Component) map[string][]componentSlot {
	groups := make(map[string][]componentSlot)
	for _, comp := range components {
		key := componentBaseKey(comp)
		groups[key] = append(groups[key], componentSlot{comp: comp})
	}
	return groups
}

func componentGroupOrder(components []scraper.Component) []string {
	seen := make(map[string]bool)
	var order []string
	for _, comp := range components {
		key := componentBaseKey(comp)
		if seen[key] {
			continue
		}
		seen[key] = true
		order = append(order, key)
	}
	return order
}

func componentBaseKey(comp scraper.Component) string {
	return comp.Name + "|" + scraper.NormalizeWeight(comp.Weight)
}

func firstUnusedComponent(slots []componentSlot) int {
	for i, slot := range slots {
		if !slot.used {
			return i
		}
	}
	return -1
}

func loadState(path string) (State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return State{}, err
	}
	var s State
	return s, json.Unmarshal(data, &s)
}

func saveState(path string, s State) error {
	data, _ := json.MarshalIndent(s, "", "  ")
	return os.WriteFile(path, data, 0644)
}

// FormatMessage değişiklikleri ders bazında gruplayıp Telegram mesajı oluşturur.
func FormatMessage(changes []Change) string {
	// Ders bazında grupla
	type courseGroup struct {
		name    string
		changes []Change
	}
	groups := map[string]*courseGroup{}
	order := []string{}

	for _, ch := range changes {
		g, ok := groups[ch.CourseCode]
		if !ok {
			g = &courseGroup{name: ch.CourseName}
			groups[ch.CourseCode] = g
			order = append(order, ch.CourseCode)
		}
		g.changes = append(g.changes, ch)
	}

	lines := []string{"🔔 *Not Değişikliği!*\n"}

	for _, code := range order {
		g := groups[code]
		lines = append(lines, fmt.Sprintf("📚 *%s*", g.name))

		for _, ch := range g.changes {
			weight := ""
			if ch.Weight != "" {
				weight = " " + scraper.FormatWeight(ch.Weight)
			}

			switch ch.Type {
			case "new_score":
				lines = append(lines, fmt.Sprintf("   • %s%s: *%s*", ch.Component, weight, ch.NewScore))
			case "score_change":
				lines = append(lines, fmt.Sprintf("   • %s%s: %s → *%s*", ch.Component, weight, ch.OldScore, ch.NewScore))
			case "new_course":
				lines = append(lines, "   🆕 Ders eklendi")
			}
		}
		lines = append(lines, "")
	}

	return strings.TrimSpace(strings.Join(lines, "\n"))
}
