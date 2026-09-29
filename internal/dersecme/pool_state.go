package dersecme

import (
	"encoding/json"
	"fmt"
	"os"
)

type PoolState map[string][]PoolCourse

func LoadPoolState(path string) (PoolState, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return make(PoolState), nil
	}
	if err != nil {
		return nil, err
	}
	var state PoolState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("seçmeli havuz durumu okuma: %w", err)
	}
	if state == nil {
		state = make(PoolState)
	}
	return state, nil
}

func SavePoolState(path string, state PoolState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("seçmeli havuz durumu yazma: %w", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("seçmeli havuz durumu yazma: %w", err)
	}
	return nil
}
