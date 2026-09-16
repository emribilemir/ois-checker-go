package dersecme

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

type SelectionChange struct {
	Type   string
	Course SelectedCourse
}

type selectionState struct {
	Courses []SelectedCourse `json:"courses"`
}

// SelectionChanges compares the observed list with the last delivered state.
// On the first observation it stores a baseline and intentionally emits no alert.
func SelectionChanges(current []SelectedCourse, stateFile string) ([]SelectionChange, error) {
	previous, err := loadSelectionState(stateFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil, SaveSelectionState(current, stateFile)
	}
	if err != nil {
		return nil, err
	}

	previousByCode := make(map[string]SelectedCourse, len(previous))
	for _, course := range previous {
		previousByCode[course.Code] = course
	}
	currentByCode := make(map[string]SelectedCourse, len(current))
	for _, course := range current {
		currentByCode[course.Code] = course
	}

	var changes []SelectionChange
	for _, course := range current {
		old, exists := previousByCode[course.Code]
		if !exists || old.Name != course.Name {
			changes = append(changes, SelectionChange{Type: "added", Course: course})
		}
	}
	for _, course := range previous {
		latest, exists := currentByCode[course.Code]
		if !exists || latest.Name != course.Name {
			changes = append(changes, SelectionChange{Type: "removed", Course: course})
		}
	}
	return changes, nil
}

func SaveSelectionState(courses []SelectedCourse, stateFile string) error {
	data, err := json.MarshalIndent(selectionState{Courses: courses}, "", "  ")
	if err != nil {
		return fmt.Errorf("ders seçme state encode: %w", err)
	}
	if err := os.WriteFile(stateFile, data, 0600); err != nil {
		return fmt.Errorf("ders seçme state yazma: %w", err)
	}
	return nil
}

func loadSelectionState(stateFile string) ([]SelectedCourse, error) {
	data, err := os.ReadFile(stateFile)
	if err != nil {
		return nil, err
	}
	var state selectionState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("ders seçme state okuma: %w", err)
	}
	return state.Courses, nil
}
