package schedule

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type State struct {
	Enabled bool            `json:"enabled"`
	Sent    map[string]bool `json:"sent"`
}

func LoadState(path string) (State, error) {
	state := State{Sent: map[string]bool{}}
	if path == "" {
		return state, nil
	}
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(body, &state); err != nil {
		return State{Sent: map[string]bool{}}, err
	}
	if state.Sent == nil {
		state.Sent = map[string]bool{}
	}
	return state, nil
}

func SaveState(path string, state State) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	body, err := json.Marshal(state)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".schedule-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
