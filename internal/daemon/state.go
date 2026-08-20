package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type usageState struct {
	Date  string                      `json:"date"`
	Users map[string]map[string]int64 `json:"users"`
}

func newState(date string) usageState {
	return usageState{Date: date, Users: make(map[string]map[string]int64)}
}

func loadState(path, date string) (usageState, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return newState(date), nil
	}
	if err != nil {
		return usageState{}, err
	}
	var state usageState
	if err := json.Unmarshal(data, &state); err != nil {
		return usageState{}, fmt.Errorf("decode state: %w", err)
	}
	if state.Date != date {
		return newState(date), nil
	}
	if state.Users == nil {
		state.Users = make(map[string]map[string]int64)
	}
	return state, nil
}

func saveState(path string, state usageState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".usage-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
